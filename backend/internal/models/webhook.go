package models

import "time"

// WebhookEvent represents an incoming webhook event record in PostgreSQL.
type WebhookEvent struct {
	ID              string                 `json:"id"`
	ProviderEventID string                 `json:"provider_event_id"`
	EventType       string                 `json:"event_type"`
	Payload         map[string]interface{} `json:"payload"`
	Processed       bool                   `json:"processed"`
	CreatedAt       time.Time              `json:"created_at"`
}

// RazorpayWebhookPayload matches the standard Razorpay webhook JSON payload structure.
type RazorpayWebhookPayload struct {
	Entity    string              `json:"entity"`
	AccountID string              `json:"account_id"`
	Event     string              `json:"event"`
	Contains  []string            `json:"contains"`
	Payload   RazorpayWebhookData `json:"payload"`
	CreatedAt int64               `json:"created_at"`
}

type RazorpayWebhookData struct {
	Payment *RazorpayPaymentEntityWrapper `json:"payment,omitempty"`
	Order   *RazorpayOrderEntityWrapper   `json:"order,omitempty"`
}

type RazorpayPaymentEntityWrapper struct {
	Entity RazorpayPaymentEntity `json:"entity"`
}

type RazorpayPaymentEntity struct {
	ID          string                 `json:"id"`
	OrderID     string                 `json:"order_id"`
	Amount      int64                  `json:"amount"`
	Currency    string                 `json:"currency"`
	Status      string                 `json:"status"`
	Method      string                 `json:"method"`
	Description string                 `json:"description"`
	ErrorReason string                 `json:"error_reason"`
	ErrorCode   string                 `json:"error_code"`
	Notes       map[string]interface{} `json:"notes"`
}

type RazorpayOrderEntityWrapper struct {
	Entity RazorpayOrderEntity `json:"entity"`
}

type RazorpayOrderEntity struct {
	ID       string                 `json:"id"`
	Amount   int64                  `json:"amount"`
	Currency string                 `json:"currency"`
	Receipt  string                 `json:"receipt"`
	Status   string                 `json:"status"`
	Notes    map[string]interface{} `json:"notes"`
}
