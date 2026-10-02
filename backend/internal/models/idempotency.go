package models

import "time"

// Idempotency status constants
const (
	IdempotencyStatusProcessing = "PROCESSING"
	IdempotencyStatusCompleted  = "COMPLETED"
	IdempotencyStatusFailed     = "FAILED"
)

// IdempotencyRecord represents a stored idempotency key and its execution result.
type IdempotencyRecord struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	Key            string    `json:"key"`
	RequestHash    string    `json:"request_hash"`
	Status         string    `json:"status"`
	ResponseStatus *int      `json:"response_status,omitempty"`
	ResponseBody   *string   `json:"response_body,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}
