package repository

import (
	"context"
	"errors"
	"fmt"
	"math"

	"campusbite/internal/database"
	"campusbite/internal/models"

	"github.com/jackc/pgx/v5"
)

var (
	ErrOrderNotFound = errors.New("order not found")
	ErrEmptyCart     = errors.New("cannot create order from an empty cart")
)

// OrderRepository handles database operations for orders and order items.
type OrderRepository struct {
	db *database.DB
}

// NewOrderRepository creates a new OrderRepository.
func NewOrderRepository(db *database.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

// CreateFromCart creates an order in PENDING status from the user's active cart in a single transaction.
func (r *OrderRepository) CreateFromCart(ctx context.Context, userID string) (*models.OrderResponse, error) {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Fetch current cart items with catalog price and availability
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
		ORDER BY m.name ASC
	`

	rows, err := tx.Query(ctx, cartQuery, userID)
	if err != nil {
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
			return nil, fmt.Errorf("failed to scan cart row: %w", err)
		}
		itemsToOrder = append(itemsToOrder, item)
	}
	rows.Close()

	if len(itemsToOrder) == 0 {
		return nil, ErrEmptyCart
	}

	// 2. Validate availability and compute total
	var totalAmount float64
	orderItemsResp := make([]models.OrderItemResponse, 0, len(itemsToOrder))

	for _, item := range itemsToOrder {
		if !item.isAvailable {
			return nil, fmt.Errorf("item '%s' is no longer available", item.name)
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

	// 3. Insert order record
	insertOrderQuery := `
		INSERT INTO orders (user_id, status, total_amount, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		RETURNING id, created_at, updated_at
	`
	var order models.OrderResponse
	order.UserID = userID
	order.Status = models.OrderStatusPending
	order.TotalAmount = totalAmount

	err = tx.QueryRow(ctx, insertOrderQuery, userID, models.OrderStatusPending, totalAmount).
		Scan(&order.ID, &order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to insert order: %w", err)
	}

	// 4. Insert order items with snapshot unit price
	insertItemQuery := `
		INSERT INTO order_items (order_id, menu_item_id, quantity, unit_price)
		VALUES ($1, $2, $3, $4)
	`
	for _, item := range itemsToOrder {
		_, err := tx.Exec(ctx, insertItemQuery, order.ID, item.menuItemID, item.quantity, item.price)
		if err != nil {
			return nil, fmt.Errorf("failed to insert order item: %w", err)
		}
	}

	// 5. Clear cart items
	clearCartQuery := `
		DELETE FROM cart_items ci
		USING carts c
		WHERE ci.cart_id = c.id AND c.user_id = $1
	`
	_, err = tx.Exec(ctx, clearCartQuery, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to clear cart items: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit order transaction: %w", err)
	}

	order.Items = orderItemsResp
	return &order, nil
}

// GetByID retrieves a single order and its items by order ID.
func (r *OrderRepository) GetByID(ctx context.Context, orderID string) (*models.OrderResponse, error) {
	orderQuery := `
		SELECT id, user_id, status, total_amount, created_at, updated_at
		FROM orders
		WHERE id = $1
	`

	var order models.OrderResponse
	err := r.db.Pool.QueryRow(ctx, orderQuery, orderID).Scan(
		&order.ID,
		&order.UserID,
		&order.Status,
		&order.TotalAmount,
		&order.CreatedAt,
		&order.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderNotFound
		}
		return nil, fmt.Errorf("failed to get order: %w", err)
	}

	items, err := r.getOrderItems(ctx, order.ID)
	if err != nil {
		return nil, err
	}
	order.Items = items

	return &order, nil
}

// ListByUserID retrieves all orders placed by a specific user.
func (r *OrderRepository) ListByUserID(ctx context.Context, userID string) ([]models.OrderResponse, error) {
	query := `
		SELECT id, user_id, status, total_amount, created_at, updated_at
		FROM orders
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	return r.queryOrders(ctx, query, userID)
}

// ListAll retrieves all orders in the system (for ADMIN use).
func (r *OrderRepository) ListAll(ctx context.Context) ([]models.OrderResponse, error) {
	query := `
		SELECT id, user_id, status, total_amount, created_at, updated_at
		FROM orders
		ORDER BY created_at DESC
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
		if err := rows.Scan(
			&o.ID,
			&o.UserID,
			&o.Status,
			&o.TotalAmount,
			&o.CreatedAt,
			&o.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}
		orders = append(orders, o)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating orders: %w", err)
	}

	// Populate items for each order
	for i := range orders {
		items, err := r.getOrderItems(ctx, orders[i].ID)
		if err != nil {
			return nil, err
		}
		orders[i].Items = items
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
