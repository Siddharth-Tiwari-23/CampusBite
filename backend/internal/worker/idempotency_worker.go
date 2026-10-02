package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"campusbite/internal/database"
)

// IdempotencyWorker periodically cleans up expired idempotency keys from the database.
type IdempotencyWorker struct {
	db       *database.DB
	interval time.Duration
}

// NewIdempotencyWorker creates a new IdempotencyWorker.
func NewIdempotencyWorker(db *database.DB, interval time.Duration) *IdempotencyWorker {
	if interval <= 0 {
		interval = 3600 * time.Second
	}
	return &IdempotencyWorker{
		db:       db,
		interval: interval,
	}
}

// Start runs the periodic idempotency key cleanup loop until context cancellation.
func (w *IdempotencyWorker) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Printf("[IdempotencyWorker] Started with interval %v", w.interval)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[IdempotencyWorker] Stopping gracefully on context cancellation...")
			return
		case <-ticker.C:
			deleted, err := w.CleanupExpiredKeys(ctx)
			if err != nil {
				log.Printf("[IdempotencyWorker] Error cleaning up expired idempotency keys: %v", err)
			} else if deleted > 0 {
				log.Printf("[IdempotencyWorker] Purged %d expired idempotency key(s)", deleted)
			}
		}
	}
}

// CleanupExpiredKeys deletes idempotency key records whose expires_at is in the past.
// Returns the count of deleted rows.
func (w *IdempotencyWorker) CleanupExpiredKeys(ctx context.Context) (int64, error) {
	query := `
		DELETE FROM idempotency_keys
		WHERE expires_at <= NOW()
	`

	tag, err := w.db.Pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to clean up expired idempotency keys: %w", err)
	}

	return tag.RowsAffected(), nil
}
