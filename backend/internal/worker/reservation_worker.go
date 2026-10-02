package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"campusbite/internal/database"
	"campusbite/internal/models"

	"github.com/jackc/pgx/v5"
)

// ReservationWorker periodically scans for and releases expired inventory reservations.
type ReservationWorker struct {
	db       *database.DB
	interval time.Duration
}

// NewReservationWorker creates a new ReservationWorker.
func NewReservationWorker(db *database.DB, interval time.Duration) *ReservationWorker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &ReservationWorker{
		db:       db,
		interval: interval,
	}
}

// Start runs the periodic reservation worker loop until context cancellation.
func (w *ReservationWorker) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Printf("[ReservationWorker] Started with interval %v", w.interval)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[ReservationWorker] Stopping gracefully on context cancellation...")
			return
		case <-ticker.C:
			processed, err := w.ProcessExpiredReservations(ctx)
			if err != nil {
				log.Printf("[ReservationWorker] Error processing expired reservations: %v", err)
			} else if processed > 0 {
				log.Printf("[ReservationWorker] Successfully released %d expired reservation(s)", processed)
			}
		}
	}
}

// ProcessExpiredReservations finds and releases all currently expired active reservations.
// Returns the count of processed reservations.
func (w *ReservationWorker) ProcessExpiredReservations(ctx context.Context) (int, error) {
	query := `
		SELECT id
		FROM inventory_reservations
		WHERE status = $1 AND expires_at <= NOW()
		ORDER BY created_at ASC
		LIMIT 100
	`

	rows, err := w.db.Pool.Query(ctx, query, models.ReservationStatusActive)
	if err != nil {
		return 0, fmt.Errorf("failed to query expired reservations: %w", err)
	}
	defer rows.Close()

	var expiredIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("failed to scan expired reservation id: %w", err)
		}
		expiredIDs = append(expiredIDs, id)
	}
	rows.Close()

	if len(expiredIDs) == 0 {
		return 0, nil
	}

	processedCount := 0
	for _, id := range expiredIDs {
		// Stop if context cancelled during processing batch
		if ctx.Err() != nil {
			break
		}

		handled, err := w.ExpireSingleReservation(ctx, id)
		if err != nil {
			log.Printf("[ReservationWorker] Error expiring reservation %s: %v", id, err)
			continue
		}
		if handled {
			processedCount++
		}
	}

	return processedCount, nil
}

// ExpireSingleReservation executes transactional, row-locked expiration of a single reservation.
// It acquires row locks on the reservation and inventory rows to prevent race conditions.
// Returns true if the reservation was expired and inventory restored, or false if it was skipped (e.g. already consumed/expired).
func (w *ReservationWorker) ExpireSingleReservation(ctx context.Context, reservationID string) (bool, error) {
	tx, err := w.db.Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Lock the reservation row
	lockResQuery := `
		SELECT id, order_id, menu_item_id, quantity, status, expires_at
		FROM inventory_reservations
		WHERE id = $1
		FOR UPDATE
	`

	var res models.InventoryReservation
	err = tx.QueryRow(ctx, lockResQuery, reservationID).Scan(
		&res.ID,
		&res.OrderID,
		&res.MenuItemID,
		&res.Quantity,
		&res.Status,
		&res.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("failed to lock reservation row: %w", err)
	}

	// 2. Re-check if reservation is still ACTIVE and actually expired
	now := time.Now()
	if res.Status != models.ReservationStatusActive || res.ExpiresAt.After(now) {
		// Already consumed, released, or not yet expired — skip safely
		return false, nil
	}

	// 3. Lock the inventory row
	lockInvQuery := `
		SELECT quantity
		FROM inventory
		WHERE menu_item_id = $1
		FOR UPDATE
	`
	var currentQty int
	err = tx.QueryRow(ctx, lockInvQuery, res.MenuItemID).Scan(&currentQty)
	if err != nil {
		return false, fmt.Errorf("failed to lock inventory row for menu item %s: %w", res.MenuItemID, err)
	}

	// 4. Restore reserved quantity to inventory
	restoreQuery := `
		UPDATE inventory
		SET quantity = quantity + $1,
		    updated_at = NOW()
		WHERE menu_item_id = $2
	`
	_, err = tx.Exec(ctx, restoreQuery, res.Quantity, res.MenuItemID)
	if err != nil {
		return false, fmt.Errorf("failed to restore inventory quantity: %w", err)
	}

	// 5. Mark reservation as EXPIRED
	updateResQuery := `
		UPDATE inventory_reservations
		SET status = $1
		WHERE id = $2
	`
	_, err = tx.Exec(ctx, updateResQuery, models.ReservationStatusExpired, res.ID)
	if err != nil {
		return false, fmt.Errorf("failed to update reservation status to EXPIRED: %w", err)
	}

	// 6. If no active reservations remain for the associated order, transition PENDING order to CANCELLED
	var activeCount int
	countQuery := `
		SELECT COUNT(*)
		FROM inventory_reservations
		WHERE order_id = $1 AND status = $2
	`
	err = tx.QueryRow(ctx, countQuery, res.OrderID, models.ReservationStatusActive).Scan(&activeCount)
	if err == nil && activeCount == 0 {
		cancelOrderQuery := `
			UPDATE orders
			SET status = $1,
			    updated_at = NOW()
			WHERE id = $2 AND status = $3
		`
		_, _ = tx.Exec(ctx, cancelOrderQuery, models.OrderStatusCancelled, res.OrderID, models.OrderStatusPending)
	}

	// 7. Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit reservation expiration transaction: %w", err)
	}

	return true, nil
}
