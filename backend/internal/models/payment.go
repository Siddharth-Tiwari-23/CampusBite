package models

import "time"

// Centralized payment status constants
const (
	PaymentStatusPending = "PENDING"
	PaymentStatusSuccess = "SUCCESS"
	PaymentStatusFailed  = "FAILED"
)

// Payment represents a payment record in the database.
type Payment struct {
	ID                string    `json:"id"`
	OrderID           string    `json:"order_id"`
	ProviderOrderID   *string   `json:"provider_order_id,omitempty"`
	ProviderPaymentID *string   `json:"provider_payment_id,omitempty"`
	Amount            float64   `json:"amount"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// CreatePaymentOrderResponse is returned when a Razorpay order is initiated.
type CreatePaymentOrderResponse struct {
	PaymentID       string `json:"payment_id"`
	OrderID         string `json:"order_id"`
	RazorpayOrderID string `json:"razorpay_order_id"`
	Amount          int64  `json:"amount"` // In paise (e.g. 50000 = ₹500.00)
	Currency        string `json:"currency"`
	KeyID           string `json:"key_id"`
}

// VerifyPaymentRequest is the payload submitted by the client after payment checkout.
type VerifyPaymentRequest struct {
	RazorpayOrderID   string `json:"razorpay_order_id" binding:"required"`
	RazorpayPaymentID string `json:"razorpay_payment_id" binding:"required"`
	RazorpaySignature string `json:"razorpay_signature" binding:"required"`
}

// VerifyPaymentResponse is the response returned after successful payment verification.
type VerifyPaymentResponse struct {
	Message   string `json:"message"`
	PaymentID string `json:"payment_id"`
	OrderID   string `json:"order_id"`
	Status    string `json:"status"`
}
