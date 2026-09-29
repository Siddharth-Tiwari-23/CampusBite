package models

import "time"

// Centralized order status constants
const (
	OrderStatusPending   = "PENDING"
	OrderStatusConfirmed = "CONFIRMED"
	OrderStatusPreparing = "PREPARING"
	OrderStatusReady     = "READY"
	OrderStatusCompleted = "COMPLETED"
	OrderStatusCancelled = "CANCELLED"
)

// OrderItemResponse represents an item snapshot in an order.
type OrderItemResponse struct {
	MenuItemID string  `json:"menu_item_id"`
	Name       string  `json:"name"`
	Quantity   int     `json:"quantity"`
	UnitPrice  float64 `json:"unit_price"`
	Subtotal   float64 `json:"subtotal"`
}

// OrderResponse represents an order with snapshot items and server total.
type OrderResponse struct {
	ID          string              `json:"id"`
	UserID      string              `json:"user_id"`
	Status      string              `json:"status"`
	TotalAmount float64             `json:"total_amount"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
	Items       []OrderItemResponse `json:"items"`
}
