package repository

import (
	"context"
	"errors"
	"fmt"

	"campusbite/internal/database"

	"github.com/jackc/pgx/v5"
)

// WebhookRepository handles recording and deduplicating incoming webhook events.
type WebhookRepository struct {
	db *database.DB
}

// NewWebhookRepository creates a new WebhookRepository.
func NewWebhookRepository(db *database.DB) *WebhookRepository {
	return &WebhookRepository{db: db}
}

// RecordAndCheckWebhookEvent inserts the incoming webhook event record.
// Returns isDuplicate = true if the event has already been received.
func (r *WebhookRepository) RecordAndCheckWebhookEvent(ctx context.Context, providerEventID, eventType string, payload []byte) (bool, error) {
	if providerEventID == "" {
		return false, errors.New("provider event id is required")
	}

	query := `
		INSERT INTO webhook_events (provider_event_id, event_type, payload, processed, created_at)
		VALUES ($1, $2, $3, false, NOW())
		ON CONFLICT (provider_event_id) DO NOTHING
		RETURNING id
	`

	var id string
	err := r.db.Pool.QueryRow(ctx, query, providerEventID, eventType, payload).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Duplicate event detected via UNIQUE constraint ON CONFLICT DO NOTHING
			return true, nil
		}
		return false, fmt.Errorf("failed to record webhook event: %w", err)
	}

	return false, nil
}

// MarkWebhookProcessed updates the processed flag for a recorded webhook event.
func (r *WebhookRepository) MarkWebhookProcessed(ctx context.Context, providerEventID string) error {
	query := `
		UPDATE webhook_events
		SET processed = true
		WHERE provider_event_id = $1
	`
	_, err := r.db.Pool.Exec(ctx, query, providerEventID)
	if err != nil {
		return fmt.Errorf("failed to mark webhook event as processed: %w", err)
	}
	return nil
}
