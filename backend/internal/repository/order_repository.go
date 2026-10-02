package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"campusbite/internal/database"
	"campusbite/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrOrderNotFound         = errors.New("order not found")
	ErrEmptyCart             = errors.New("cannot create order from an empty cart")
	ErrInsufficientInventory = errors.New("insufficient inventory")
	ErrMenuItemUnavailable   = errors.New("menu item is unavailable")
	ErrIdempotencyProcessing = errors.New("a checkout operation with this idempotency key is currently being processed")
	ErrIdempotencyFailed     = errors.New("previous checkout operation with this idempotency key failed")
)

const (
	// ReservationTTL defines the duration an inventory reservation remains ACTIVE before expiry.
	ReservationTTL = 15 * time.Minute
	// IdempotencyTTL defines the retention duration for idempotency records.
	IdempotencyTTL = 24 * time.Hour
)

// OrderRepository handles database operations for orders, order items, inventory reservations, and idempotency.
type OrderRepository struct {
	db *database.DB
}

// NewOrderRepository creates a new OrderRepository.
func NewOrderRepository(db *database.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

// CreateFromCart executes an idempotent, transactional checkout with concurrency-safe inventory reservation.
// It inserts an idempotency record inside the transaction to ensure atomic deduplication.
// If paymentMethod is "COD" or "CASH_ON_DELIVERY", the order is placed directly in CONFIRMED status with CONSUMED reservations,
// creates a pending cash payment record, and persists an order confirmation notification.
// If a repeated request with the same (user_id, idempotencyKey) is detected:
//   - If COMPLETED: returns the previously created order payload without re-running checkout or re-reserving stock.
//   - If PROCESSING: returns ErrIdempotencyProcessing to prevent concurrent duplicate execution.
//   - If FAILED: returns ErrIdempotencyFailed.
func (r *OrderRepository) CreateFromCart(ctx context.Context, userID string, idempotencyKey string, requestHash string, paymentMethodOption ...string) (*models.OrderResponse, error) {
	if idempotencyKey == "" {
		return nil, errors.New("idempotency key is required")
	}

	paymentMethod := "ONLINE"
	if len(paymentMethodOption) > 0 && strings.TrimSpace(paymentMethodOption[0]) != "" {
		m := strings.ToUpper(strings.TrimSpace(paymentMethodOption[0]))
		if m == "COD" || m == "CASH_ON_DELIVERY" || m == "CASH" || m == "CASH ON DELIVERY" {
			paymentMethod = "COD"
		}
	}
	isCOD := paymentMethod == "COD"

	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Attempt to claim the idempotency key for this user atomically within the transaction
	insertIdempotencyQuery := `
		INSERT INTO idempotency_keys (user_id, key, request_hash, status, expires_at, created_at)
		VALUES ($1, $2, $3, $4, NOW() + $5::interval, NOW())
	`
	_, err = tx.Exec(ctx, insertIdempotencyQuery, userID, idempotencyKey, requestHash, models.IdempotencyStatusProcessing, "24 hours")
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// Duplicate idempotency key detected. Roll back our transaction and fetch the existing record.
			_ = tx.Rollback(ctx)
			return r.handleExistingIdempotencyKey(ctx, userID, idempotencyKey)
		}
		return nil, fmt.Errorf("failed to insert idempotency key: %w", err)
	}

	// 2. Fetch current cart items ordered deterministically by menu_item_id to prevent database deadlocks
	cartQuery := `
		SELECT 
			ci.menu_item_id,
			m.name,
			m.price,
			m.is_available,
			ci.quantity
		FROM cart_items ci
		JOIN menu_items m ON ci.menu_item_id = m.id
		JOIN carts c ON ci.cart_id = c.id
		WHERE c.user_id = $1
		ORDER BY ci.menu_item_id ASC
	`

	rows, err := tx.Query(ctx, cartQuery, userID)
	if err != nil {
		_ = tx.Rollback(ctx)
		r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, err.Error())
		return nil, fmt.Errorf("failed to query cart items: %w", err)
	}

	type cartRow struct {
		menuItemID  string
		name        string
		price       float64
		isAvailable bool
		quantity    int
	}

	var itemsToOrder []cartRow
	for rows.Next() {
		var item cartRow
		if err := rows.Scan(&item.menuItemID, &item.name, &item.price, &item.isAvailable, &item.quantity); err != nil {
			rows.Close()
			_ = tx.Rollback(ctx)
			r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, err.Error())
			return nil, fmt.Errorf("failed to scan cart row: %w", err)
		}
		itemsToOrder = append(itemsToOrder, item)
	}
	rows.Close()

	if len(itemsToOrder) == 0 {
		_ = tx.Rollback(ctx)
		r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, "cart is empty")
		return nil, ErrEmptyCart
	}

	// 3. Validate menu item availability & Lock inventory rows with SELECT ... FOR UPDATE
	var totalAmount float64
	orderItemsResp := make([]models.OrderItemResponse, 0, len(itemsToOrder))
	reservationsResp := make([]models.InventoryReservation, 0, len(itemsToOrder))

	lockInventoryQuery := `
		SELECT quantity
		FROM inventory
		WHERE menu_item_id = $1
		FOR UPDATE
	`

	deductInventoryQuery := `
		UPDATE inventory
		SET quantity = quantity - $2,
		    updated_at = NOW()
		WHERE menu_item_id = $1
	`

	for _, item := range itemsToOrder {
		if !item.isAvailable {
			_ = tx.Rollback(ctx)
			failErr := fmt.Errorf("item '%s' is no longer available: %w", item.name, ErrMenuItemUnavailable)
			r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, failErr.Error())
			return nil, failErr
		}

		// Lock inventory row for this menu item
		var currentStock int
		err := tx.QueryRow(ctx, lockInventoryQuery, item.menuItemID).Scan(&currentStock)
		if err != nil {
			_ = tx.Rollback(ctx)
			if errors.Is(err, pgx.ErrNoRows) {
				failErr := fmt.Errorf("inventory record not found for '%s': %w", item.name, ErrInsufficientInventory)
				r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, failErr.Error())
				return nil, failErr
			}
			r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, err.Error())
			return nil, fmt.Errorf("failed to lock inventory for '%s': %w", item.name, err)
		}

		// Verify stock sufficiency
		if currentStock < item.quantity {
			_ = tx.Rollback(ctx)
			failErr := fmt.Errorf("insufficient stock for '%s': requested %d, available %d: %w",
				item.name, item.quantity, currentStock, ErrInsufficientInventory)
			r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, failErr.Error())
			return nil, failErr
		}

		// Deduct available inventory
		_, err = tx.Exec(ctx, deductInventoryQuery, item.menuItemID, item.quantity)
		if err != nil {
			_ = tx.Rollback(ctx)
			r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, err.Error())
			return nil, fmt.Errorf("failed to deduct inventory for '%s': %w", item.name, err)
		}

		subtotal := math.Round(float64(item.quantity)*item.price*100) / 100
		totalAmount += subtotal

		orderItemsResp = append(orderItemsResp, models.OrderItemResponse{
			MenuItemID: item.menuItemID,
			Name:       item.name,
			Quantity:   item.quantity,
			UnitPrice:  item.price,
			Subtotal:   subtotal,
		})
	}

	totalAmount = math.Round(totalAmount*100) / 100

	// 4. Determine initial order status
	initialOrderStatus := models.OrderStatusPending
	reservationStatus := models.ReservationStatusActive
	if isCOD {
		initialOrderStatus = models.OrderStatusConfirmed
		reservationStatus = models.ReservationStatusConsumed
	}

	// Insert order record
	insertOrderQuery := `
		INSERT INTO orders (user_id, status, total_amount, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		RETURNING id, created_at, updated_at
	`
	var order models.OrderResponse
	order.UserID = userID
	order.Status = initialOrderStatus
	order.TotalAmount = totalAmount
	order.PaymentMethod = paymentMethod
	order.PaymentStatus = string(models.PaymentStatusPending)

	err = tx.QueryRow(ctx, insertOrderQuery, userID, initialOrderStatus, totalAmount).
		Scan(&order.ID, &order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		_ = tx.Rollback(ctx)
		r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, err.Error())
		return nil, fmt.Errorf("failed to insert order: %w", err)
	}

	// 5. Insert order items with frozen price snapshot
	insertItemQuery := `
		INSERT INTO order_items (order_id, menu_item_id, quantity, unit_price)
		VALUES ($1, $2, $3, $4)
	`
	for _, item := range itemsToOrder {
		_, err := tx.Exec(ctx, insertItemQuery, order.ID, item.menuItemID, item.quantity, item.price)
		if err != nil {
			_ = tx.Rollback(ctx)
			r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, err.Error())
			return nil, fmt.Errorf("failed to insert order item: %w", err)
		}
	}

	// 6. Create inventory reservation records
	insertReservationQuery := `
		INSERT INTO inventory_reservations (order_id, menu_item_id, quantity, status, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		RETURNING id, created_at
	`
	expiresAt := time.Now().Add(ReservationTTL)
	for _, item := range itemsToOrder {
		var res models.InventoryReservation
		res.OrderID = order.ID
		res.MenuItemID = item.menuItemID
		res.Quantity = item.quantity
		res.Status = reservationStatus
		res.ExpiresAt = expiresAt

		err := tx.QueryRow(ctx, insertReservationQuery, order.ID, item.menuItemID, item.quantity, reservationStatus, expiresAt).
			Scan(&res.ID, &res.CreatedAt)
		if err != nil {
			_ = tx.Rollback(ctx)
			r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, err.Error())
			return nil, fmt.Errorf("failed to insert inventory reservation: %w", err)
		}
		reservationsResp = append(reservationsResp, res)
	}

	// 7. For COD: Insert payment record and confirmation notification
	if isCOD {
		insertPaymentQuery := `
			INSERT INTO payments (order_id, provider_order_id, amount, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, NOW(), NOW())
		`
		_, err = tx.Exec(ctx, insertPaymentQuery, order.ID, "COD", totalAmount, models.PaymentStatusPending)
		if err != nil {
			_ = tx.Rollback(ctx)
			r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, err.Error())
			return nil, fmt.Errorf("failed to insert COD payment record: %w", err)
		}

		// Persist durable order confirmation notification
		shortID := order.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		dataBytes, _ := json.Marshal(map[string]interface{}{
			"order_id":       order.ID,
			"status":         models.OrderStatusConfirmed,
			"payment_method": "COD",
		})
		insertNotifQuery := `
			INSERT INTO notifications (user_id, type, title, message, data, is_read, created_at)
			VALUES ($1, $2, $3, $4, $5, false, NOW())
		`
		_, err = tx.Exec(ctx, insertNotifQuery, userID, models.NotificationTypeOrderStatus, "Order Confirmed", fmt.Sprintf("Your order #%s has been confirmed.", shortID), dataBytes)
		if err != nil {
			_ = tx.Rollback(ctx)
			r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, err.Error())
			return nil, fmt.Errorf("failed to persist COD notification: %w", err)
		}
	}

	// 8. Clear user's cart items
	clearCartQuery := `
		DELETE FROM cart_items ci
		USING carts c
		WHERE ci.cart_id = c.id AND c.user_id = $1
	`
	_, err = tx.Exec(ctx, clearCartQuery, userID)
	if err != nil {
		_ = tx.Rollback(ctx)
		r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, err.Error())
		return nil, fmt.Errorf("failed to clear cart items: %w", err)
	}

	order.Items = orderItemsResp
	order.Reservations = reservationsResp

	// 8. Update idempotency record to COMPLETED with serialized response payload
	orderJSON, err := json.Marshal(order)
	if err != nil {
		_ = tx.Rollback(ctx)
		r.recordIdempotencyFailure(ctx, userID, idempotencyKey, requestHash, err.Error())
		return nil, fmt.Errorf("failed to serialize order response: %w", err)
	}
	orderBodyStr := string(orderJSON)
	responseStatus := 201

	updateIdempotencyQuery := `
		UPDATE idempotency_keys
		SET status = $3,
		    response_status = $4,
		    response_body = $5
		WHERE user_id = $1 AND key = $2
	`
	_, err = tx.Exec(ctx, updateIdempotencyQuery, userID, idempotencyKey, models.IdempotencyStatusCompleted, responseStatus, orderBodyStr)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("failed to update idempotency key status: %w", err)
	}

	// 9. Commit the entire transaction atomically
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit order transaction: %w", err)
	}

	return &order, nil
}

// handleExistingIdempotencyKey inspects an already registered idempotency key and returns the saved result if COMPLETED.
func (r *OrderRepository) handleExistingIdempotencyKey(ctx context.Context, userID, idempotencyKey string) (*models.OrderResponse, error) {
	query := `
		SELECT status, response_status, response_body
		FROM idempotency_keys
		WHERE user_id = $1 AND key = $2
	`
	var status string
	var responseStatus *int
	var responseBody *string

	err := r.db.Pool.QueryRow(ctx, query, userID, idempotencyKey).Scan(&status, &responseStatus, &responseBody)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("idempotency record not found")
		}
		return nil, fmt.Errorf("failed to retrieve idempotency record: %w", err)
	}

	switch status {
	case models.IdempotencyStatusCompleted:
		if responseBody == nil || *responseBody == "" {
			return nil, errors.New("completed idempotency record has empty response body")
		}
		var order models.OrderResponse
		if err := json.Unmarshal([]byte(*responseBody), &order); err != nil {
			return nil, fmt.Errorf("failed to deserialize cached order response: %w", err)
		}
		return &order, nil

	case models.IdempotencyStatusProcessing:
		return nil, ErrIdempotencyProcessing

	case models.IdempotencyStatusFailed:
		if responseBody != nil && *responseBody != "" {
			return nil, fmt.Errorf("%w: %s", ErrIdempotencyFailed, *responseBody)
		}
		return nil, ErrIdempotencyFailed

	default:
		return nil, fmt.Errorf("unknown idempotency status: %s", status)
	}
}

// recordIdempotencyFailure persists a FAILED idempotency state in a standalone statement.
func (r *OrderRepository) recordIdempotencyFailure(ctx context.Context, userID, idempotencyKey, requestHash, reason string) {
	query := `
		INSERT INTO idempotency_keys (user_id, key, request_hash, status, response_status, response_body, expires_at, created_at)
		VALUES ($1, $2, $3, $4, 400, $5, NOW() + INTERVAL '24 hours', NOW())
		ON CONFLICT (user_id, key) DO UPDATE
		SET status = $4, response_status = 400, response_body = $5
	`
	_, _ = r.db.Pool.Exec(ctx, query, userID, idempotencyKey, requestHash, models.IdempotencyStatusFailed, reason)
}

// GetByID retrieves a single order and its items by order ID.
func (r *OrderRepository) GetByID(ctx context.Context, orderID string) (*models.OrderResponse, error) {
	orderQuery := `
		SELECT 
			o.id, 
			o.user_id, 
			o.status, 
			o.total_amount, 
			o.created_at, 
			o.updated_at,
			COALESCE(p.provider_order_id, ''),
			COALESCE(p.status, '')
		FROM orders o
		LEFT JOIN payments p ON p.order_id = o.id
		WHERE o.id = $1
	`

	var order models.OrderResponse
	var providerOrderID, paymentStatus string
	err := r.db.Pool.QueryRow(ctx, orderQuery, orderID).Scan(
		&order.ID,
		&order.UserID,
		&order.Status,
		&order.TotalAmount,
		&order.CreatedAt,
		&order.UpdatedAt,
		&providerOrderID,
		&paymentStatus,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderNotFound
		}
		return nil, fmt.Errorf("failed to get order: %w", err)
	}

	if providerOrderID == "COD" {
		order.PaymentMethod = "COD"
	} else if providerOrderID != "" {
		order.PaymentMethod = "ONLINE"
	} else {
		order.PaymentMethod = "ONLINE"
	}
	order.PaymentStatus = paymentStatus

	items, err := r.getOrderItems(ctx, order.ID)
	if err != nil {
		return nil, err
	}
	order.Items = items

	reservations, err := r.GetReservationsByOrderID(ctx, order.ID)
	if err != nil {
		return nil, err
	}
	order.Reservations = reservations

	return &order, nil
}

// GetReservationsByOrderID retrieves all inventory reservations associated with an order.
func (r *OrderRepository) GetReservationsByOrderID(ctx context.Context, orderID string) ([]models.InventoryReservation, error) {
	query := `
		SELECT id, order_id, menu_item_id, quantity, status, expires_at, created_at
		FROM inventory_reservations
		WHERE order_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.Pool.Query(ctx, query, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to query inventory reservations: %w", err)
	}
	defer rows.Close()

	reservations := make([]models.InventoryReservation, 0)
	for rows.Next() {
		var res models.InventoryReservation
		if err := rows.Scan(
			&res.ID,
			&res.OrderID,
			&res.MenuItemID,
			&res.Quantity,
			&res.Status,
			&res.ExpiresAt,
			&res.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan inventory reservation: %w", err)
		}
		reservations = append(reservations, res)
	}

	return reservations, rows.Err()
}

// ListByUserID retrieves all orders placed by a specific user.
func (r *OrderRepository) ListByUserID(ctx context.Context, userID string) ([]models.OrderResponse, error) {
	query := `
		SELECT 
			o.id, 
			o.user_id, 
			o.status, 
			o.total_amount, 
			o.created_at, 
			o.updated_at,
			COALESCE(p.provider_order_id, ''),
			COALESCE(p.status, '')
		FROM orders o
		LEFT JOIN payments p ON p.order_id = o.id
		WHERE o.user_id = $1
		ORDER BY o.created_at DESC
	`

	return r.queryOrders(ctx, query, userID)
}

// ListAll retrieves all orders in the system (for ADMIN use).
func (r *OrderRepository) ListAll(ctx context.Context) ([]models.OrderResponse, error) {
	query := `
		SELECT 
			o.id, 
			o.user_id, 
			o.status, 
			o.total_amount, 
			o.created_at, 
			o.updated_at,
			COALESCE(p.provider_order_id, ''),
			COALESCE(p.status, '')
		FROM orders o
		LEFT JOIN payments p ON p.order_id = o.id
		ORDER BY o.created_at DESC
	`

	return r.queryOrders(ctx, query)
}

func (r *OrderRepository) queryOrders(ctx context.Context, query string, args ...any) ([]models.OrderResponse, error) {
	rows, err := r.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query orders: %w", err)
	}
	defer rows.Close()

	orders := make([]models.OrderResponse, 0)
	for rows.Next() {
		var o models.OrderResponse
		var providerOrderID, paymentStatus string
		if err := rows.Scan(
			&o.ID,
			&o.UserID,
			&o.Status,
			&o.TotalAmount,
			&o.CreatedAt,
			&o.UpdatedAt,
			&providerOrderID,
			&paymentStatus,
		); err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}
		if providerOrderID == "COD" {
			o.PaymentMethod = "COD"
		} else if providerOrderID != "" {
			o.PaymentMethod = "ONLINE"
		} else {
			o.PaymentMethod = "ONLINE"
		}
		o.PaymentStatus = paymentStatus
		orders = append(orders, o)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating orders: %w", err)
	}

	// Populate items and reservations for each order
	for i := range orders {
		items, err := r.getOrderItems(ctx, orders[i].ID)
		if err != nil {
			return nil, err
		}
		orders[i].Items = items

		reservations, err := r.GetReservationsByOrderID(ctx, orders[i].ID)
		if err != nil {
			return nil, err
		}
		orders[i].Reservations = reservations
	}

	return orders, nil
}

func (r *OrderRepository) getOrderItems(ctx context.Context, orderID string) ([]models.OrderItemResponse, error) {
	query := `
		SELECT 
			oi.menu_item_id,
			COALESCE(m.name, 'Deleted Item'),
			oi.quantity,
			oi.unit_price,
			ROUND((oi.quantity * oi.unit_price)::numeric, 2) AS subtotal
		FROM order_items oi
		LEFT JOIN menu_items m ON oi.menu_item_id = m.id
		WHERE oi.order_id = $1
		ORDER BY oi.menu_item_id ASC
	`

	rows, err := r.db.Pool.Query(ctx, query, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to query order items: %w", err)
	}
	defer rows.Close()

	items := make([]models.OrderItemResponse, 0)
	for rows.Next() {
		var item models.OrderItemResponse
		if err := rows.Scan(
			&item.MenuItemID,
			&item.Name,
			&item.Quantity,
			&item.UnitPrice,
			&item.Subtotal,
		); err != nil {
			return nil, fmt.Errorf("failed to scan order item: %w", err)
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating order items: %w", err)
	}

	return items, nil
}

// UpdateStatus transitions an order to a new status and persists a notification within the same transaction.
func (r *OrderRepository) UpdateStatus(ctx context.Context, orderID string, newStatus string) (*models.OrderResponse, error) {
	switch newStatus {
	case models.OrderStatusPending,
		models.OrderStatusConfirmed,
		models.OrderStatusPreparing,
		models.OrderStatusReady,
		models.OrderStatusCompleted,
		models.OrderStatusCancelled:
	default:
		return nil, fmt.Errorf("invalid order status: %s", newStatus)
	}

	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var userID string
	var currentStatus string
	checkQuery := `
		SELECT user_id, status
		FROM orders
		WHERE id = $1
		FOR UPDATE
	`
	err = tx.QueryRow(ctx, checkQuery, orderID).Scan(&userID, &currentStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderNotFound
		}
		return nil, fmt.Errorf("failed to check order: %w", err)
	}

	updateQuery := `
		UPDATE orders
		SET status = $1,
		    updated_at = NOW()
		WHERE id = $2
	`
	_, err = tx.Exec(ctx, updateQuery, newStatus, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to update order status: %w", err)
	}

	// Persist notification within the same transaction
	title, message := getOrderStatusNotificationContent(orderID, newStatus)
	dataMap := map[string]interface{}{
		"order_id": orderID,
		"status":   newStatus,
	}
	dataBytes, _ := json.Marshal(dataMap)

	insertNotifQuery := `
		INSERT INTO notifications (user_id, type, title, message, data, is_read, created_at)
		VALUES ($1, $2, $3, $4, $5, false, NOW())
	`
	_, err = tx.Exec(ctx, insertNotifQuery, userID, models.NotificationTypeOrderStatus, title, message, dataBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create notification: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit order update transaction: %w", err)
	}

	return r.GetByID(ctx, orderID)
}

func getOrderStatusNotificationContent(orderID, status string) (string, string) {
	shortID := orderID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	switch status {
	case models.OrderStatusConfirmed:
		return "Order Confirmed", fmt.Sprintf("Your order #%s has been confirmed.", shortID)
	case models.OrderStatusPreparing:
		return "Order Preparing", fmt.Sprintf("The kitchen is preparing your order #%s.", shortID)
	case models.OrderStatusReady:
		return "Order Ready", fmt.Sprintf("Your order #%s is ready for pickup!", shortID)
	case models.OrderStatusCompleted:
		return "Order Completed", fmt.Sprintf("Your order #%s has been completed. Enjoy!", shortID)
	case models.OrderStatusCancelled:
		return "Order Cancelled", fmt.Sprintf("Your order #%s was cancelled.", shortID)
	default:
		return "Order Updated", fmt.Sprintf("Your order #%s status changed to %s.", shortID, status)
	}
}
