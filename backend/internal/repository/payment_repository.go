package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"campusbite/internal/database"
	"campusbite/internal/models"

	"github.com/jackc/pgx/v5"
)

var (
	ErrPaymentNotFound = errors.New("payment not found")
	ErrPaymentConflict = errors.New("payment is in an invalid state for this operation")
)

// PaymentRepository handles payment persistence and payment-triggered state transitions.
type PaymentRepository struct {
	db        *database.DB
	orderRepo *OrderRepository
}

// NewPaymentRepository creates a new PaymentRepository.
func NewPaymentRepository(db *database.DB, orderRepo *OrderRepository) *PaymentRepository {
	return &PaymentRepository{
		db:        db,
		orderRepo: orderRepo,
	}
}

// CreateOrGetPayment inserts a pending payment record or retrieves/updates an existing pending one.
func (r *PaymentRepository) CreateOrGetPayment(ctx context.Context, orderID string, providerOrderID string, amount float64) (*models.Payment, error) {
	// First check if payment already exists for this order
	query := `
		SELECT id, order_id, provider_order_id, provider_payment_id, amount, status, created_at, updated_at
		FROM payments
		WHERE order_id = $1
	`
	var payment models.Payment
	err := r.db.Pool.QueryRow(ctx, query, orderID).Scan(
		&payment.ID,
		&payment.OrderID,
		&payment.ProviderOrderID,
		&payment.ProviderPaymentID,
		&payment.Amount,
		&payment.Status,
		&payment.CreatedAt,
		&payment.UpdatedAt,
	)
	if err == nil {
		// Existing record found
		if payment.Status == models.PaymentStatusPending {
			// Update provider_order_id if new order was generated
			updateQuery := `
				UPDATE payments
				SET provider_order_id = $1, amount = $2, updated_at = NOW()
				WHERE id = $3
				RETURNING updated_at
			`
			err = r.db.Pool.QueryRow(ctx, updateQuery, providerOrderID, amount, payment.ID).Scan(&payment.UpdatedAt)
			if err != nil {
				return nil, fmt.Errorf("failed to update payment record: %w", err)
			}
			payment.ProviderOrderID = &providerOrderID
			payment.Amount = amount
		}
		return &payment, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("failed to query payment by order id: %w", err)
	}

	// Insert new payment row
	insertQuery := `
		INSERT INTO payments (order_id, provider_order_id, amount, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		RETURNING id, order_id, provider_order_id, provider_payment_id, amount, status, created_at, updated_at
	`
	err = r.db.Pool.QueryRow(ctx, insertQuery, orderID, providerOrderID, amount, models.PaymentStatusPending).Scan(
		&payment.ID,
		&payment.OrderID,
		&payment.ProviderOrderID,
		&payment.ProviderPaymentID,
		&payment.Amount,
		&payment.Status,
		&payment.CreatedAt,
		&payment.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert payment record: %w", err)
	}

	return &payment, nil
}

// GetPaymentByOrderID retrieves a payment by order UUID.
func (r *PaymentRepository) GetPaymentByOrderID(ctx context.Context, orderID string) (*models.Payment, error) {
	query := `
		SELECT id, order_id, provider_order_id, provider_payment_id, amount, status, created_at, updated_at
		FROM payments
		WHERE order_id = $1
	`
	var payment models.Payment
	err := r.db.Pool.QueryRow(ctx, query, orderID).Scan(
		&payment.ID,
		&payment.OrderID,
		&payment.ProviderOrderID,
		&payment.ProviderPaymentID,
		&payment.Amount,
		&payment.Status,
		&payment.CreatedAt,
		&payment.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}
		return nil, fmt.Errorf("failed to get payment: %w", err)
	}
	return &payment, nil
}

// GetPaymentByProviderOrderID retrieves a payment by Razorpay provider order ID.
func (r *PaymentRepository) GetPaymentByProviderOrderID(ctx context.Context, providerOrderID string) (*models.Payment, error) {
	query := `
		SELECT id, order_id, provider_order_id, provider_payment_id, amount, status, created_at, updated_at
		FROM payments
		WHERE provider_order_id = $1
	`
	var payment models.Payment
	err := r.db.Pool.QueryRow(ctx, query, providerOrderID).Scan(
		&payment.ID,
		&payment.OrderID,
		&payment.ProviderOrderID,
		&payment.ProviderPaymentID,
		&payment.Amount,
		&payment.Status,
		&payment.CreatedAt,
		&payment.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}
		return nil, fmt.Errorf("failed to get payment by provider order id: %w", err)
	}
	return &payment, nil
}

// ConfirmPaymentSuccess handles transactional state transition for a successful payment.
// It locks the payment row, sets payment -> SUCCESS, order -> CONFIRMED, and active reservations -> CONSUMED.
// It is fully idempotent against repeat invocations.
func (r *PaymentRepository) ConfirmPaymentSuccess(ctx context.Context, providerOrderID string, providerPaymentID string) (*models.Payment, *models.OrderResponse, error) {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Lock the payment row
	lockQuery := `
		SELECT id, order_id, provider_order_id, provider_payment_id, amount, status, created_at, updated_at
		FROM payments
		WHERE provider_order_id = $1
		FOR UPDATE
	`
	var payment models.Payment
	err = tx.QueryRow(ctx, lockQuery, providerOrderID).Scan(
		&payment.ID,
		&payment.OrderID,
		&payment.ProviderOrderID,
		&payment.ProviderPaymentID,
		&payment.Amount,
		&payment.Status,
		&payment.CreatedAt,
		&payment.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrPaymentNotFound
		}
		return nil, nil, fmt.Errorf("failed to lock payment row: %w", err)
	}

	// Idempotency check: If already SUCCESS, return existing state without duplicate updates
	if payment.Status == models.PaymentStatusSuccess {
		_ = tx.Commit(ctx)
		order, err := r.orderRepo.GetByID(ctx, payment.OrderID)
		if err != nil {
			return nil, nil, err
		}
		return &payment, order, nil
	}

	if payment.Status == models.PaymentStatusFailed {
		return nil, nil, fmt.Errorf("cannot confirm a failed payment: %w", ErrPaymentConflict)
	}

	// 1. Update payment to SUCCESS
	updatePaymentQuery := `
		UPDATE payments
		SET status = $1,
		    provider_payment_id = $2,
		    updated_at = NOW()
		WHERE id = $3
		RETURNING updated_at
	`
	err = tx.QueryRow(ctx, updatePaymentQuery, models.PaymentStatusSuccess, providerPaymentID, payment.ID).
		Scan(&payment.UpdatedAt)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to update payment to success: %w", err)
	}
	payment.Status = models.PaymentStatusSuccess
	payment.ProviderPaymentID = &providerPaymentID

	// 2. Update order to CONFIRMED
	updateOrderQuery := `
		UPDATE orders
		SET status = $1,
		    updated_at = NOW()
		WHERE id = $2
	`
	_, err = tx.Exec(ctx, updateOrderQuery, models.OrderStatusConfirmed, payment.OrderID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to update order status to confirmed: %w", err)
	}

	// 3. Mark active inventory reservations as CONSUMED
	updateReservationsQuery := `
		UPDATE inventory_reservations
		SET status = $1
		WHERE order_id = $2 AND status = $3
	`
	_, err = tx.Exec(ctx, updateReservationsQuery, models.ReservationStatusConsumed, payment.OrderID, models.ReservationStatusActive)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to mark reservations as consumed: %w", err)
	}

	// 4. Persist durable notification for the order owner
	var userID string
	err = tx.QueryRow(ctx, "SELECT user_id FROM orders WHERE id = $1", payment.OrderID).Scan(&userID)
	if err == nil && userID != "" {
		shortID := payment.OrderID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		dataBytes, _ := json.Marshal(map[string]interface{}{
			"order_id":   payment.OrderID,
			"payment_id": payment.ID,
			"status":     models.OrderStatusConfirmed,
		})
		insertNotifQuery := `
			INSERT INTO notifications (user_id, type, title, message, data, is_read, created_at)
			VALUES ($1, $2, $3, $4, $5, false, NOW())
		`
		_, err = tx.Exec(ctx, insertNotifQuery, userID, models.NotificationTypePayment, "Order Confirmed", fmt.Sprintf("Payment successful! Your order #%s has been confirmed.", shortID), dataBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to persist payment confirmation notification: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to commit payment confirmation transaction: %w", err)
	}

	order, err := r.orderRepo.GetByID(ctx, payment.OrderID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch updated order: %w", err)
	}

	return &payment, order, nil
}

// HandlePaymentFailure handles transactional state transition for a failed payment.
// It locks the payment row, sets payment -> FAILED, order -> CANCELLED, restores inventory quantity,
// and marks active reservations -> RELEASED. It is idempotent against duplicate failure calls.
func (r *PaymentRepository) HandlePaymentFailure(ctx context.Context, providerOrderID string, providerPaymentID string, reason string) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Lock the payment row
	lockQuery := `
		SELECT id, order_id, provider_order_id, provider_payment_id, amount, status
		FROM payments
		WHERE provider_order_id = $1
		FOR UPDATE
	`
	var payment models.Payment
	err = tx.QueryRow(ctx, lockQuery, providerOrderID).Scan(
		&payment.ID,
		&payment.OrderID,
		&payment.ProviderOrderID,
		&payment.ProviderPaymentID,
		&payment.Amount,
		&payment.Status,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPaymentNotFound
		}
		return fmt.Errorf("failed to lock payment row: %w", err)
	}

	// Idempotency check: If already FAILED, stock is already restored, nothing more to do
	if payment.Status == models.PaymentStatusFailed {
		_ = tx.Commit(ctx)
		return nil
	}

	if payment.Status == models.PaymentStatusSuccess {
		return fmt.Errorf("cannot fail an already successful payment: %w", ErrPaymentConflict)
	}

	// 1. Update payment to FAILED
	updatePaymentQuery := `
		UPDATE payments
		SET status = $1,
		    provider_payment_id = COALESCE($2, provider_payment_id),
		    updated_at = NOW()
		WHERE id = $3
	`
	_, err = tx.Exec(ctx, updatePaymentQuery, models.PaymentStatusFailed, providerPaymentID, payment.ID)
	if err != nil {
		return fmt.Errorf("failed to update payment to failed: %w", err)
	}

	// 2. Update order to CANCELLED
	updateOrderQuery := `
		UPDATE orders
		SET status = $1,
		    updated_at = NOW()
		WHERE id = $2
	`
	_, err = tx.Exec(ctx, updateOrderQuery, models.OrderStatusCancelled, payment.OrderID)
	if err != nil {
		return fmt.Errorf("failed to update order to cancelled: %w", err)
	}

	// 3. Lock active reservations and restore inventory
	selectReservationsQuery := `
		SELECT menu_item_id, quantity
		FROM inventory_reservations
		WHERE order_id = $1 AND status = $2
		FOR UPDATE
	`
	rows, err := tx.Query(ctx, selectReservationsQuery, payment.OrderID, models.ReservationStatusActive)
	if err != nil {
		return fmt.Errorf("failed to lock active reservations: %w", err)
	}

	type resItem struct {
		menuItemID string
		quantity   int
	}
	var activeItems []resItem
	for rows.Next() {
		var item resItem
		if err := rows.Scan(&item.menuItemID, &item.quantity); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan active reservation: %w", err)
		}
		activeItems = append(activeItems, item)
	}
	rows.Close()

	// Restore stock back to inventory
	restoreStockQuery := `
		UPDATE inventory
		SET quantity = quantity + $1,
		    updated_at = NOW()
		WHERE menu_item_id = $2
	`
	for _, item := range activeItems {
		_, err = tx.Exec(ctx, restoreStockQuery, item.quantity, item.menuItemID)
		if err != nil {
			return fmt.Errorf("failed to restore inventory stock for item %s: %w", item.menuItemID, err)
		}
	}

	// 4. Mark active reservations as RELEASED
	updateResStatusQuery := `
		UPDATE inventory_reservations
		SET status = $1
		WHERE order_id = $2 AND status = $3
	`
	_, err = tx.Exec(ctx, updateResStatusQuery, models.ReservationStatusReleased, payment.OrderID, models.ReservationStatusActive)
	if err != nil {
		return fmt.Errorf("failed to mark reservations as released: %w", err)
	}

	// 5. Persist durable notification for the order owner
	var userID string
	err = tx.QueryRow(ctx, "SELECT user_id FROM orders WHERE id = $1", payment.OrderID).Scan(&userID)
	if err == nil && userID != "" {
		shortID := payment.OrderID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		dataBytes, _ := json.Marshal(map[string]interface{}{
			"order_id":          payment.OrderID,
			"provider_order_id": providerOrderID,
			"reason":            reason,
			"status":            models.OrderStatusCancelled,
		})
		failMsg := fmt.Sprintf("Payment for order #%s failed. Your order has been cancelled and stock released.", shortID)
		if reason != "" {
			failMsg = fmt.Sprintf("Payment for order #%s failed (%s). Your order has been cancelled and stock released.", shortID, reason)
		}
		insertNotifQuery := `
			INSERT INTO notifications (user_id, type, title, message, data, is_read, created_at)
			VALUES ($1, $2, $3, $4, $5, false, NOW())
		`
		_, err = tx.Exec(ctx, insertNotifQuery, userID, models.NotificationTypePayment, "Payment Failed", failMsg, dataBytes)
		if err != nil {
			return fmt.Errorf("failed to persist payment failure notification: %w", err)
		}
	}

	return tx.Commit(ctx)
}
