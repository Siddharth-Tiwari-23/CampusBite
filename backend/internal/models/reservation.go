package models

import "time"

// Centralized reservation status constants matching the PostgreSQL schema constraints.
const (
	ReservationStatusActive   = "ACTIVE"
	ReservationStatusReleased = "RELEASED"
	ReservationStatusConsumed = "CONSUMED"
	ReservationStatusExpired  = "EXPIRED"
)

// InventoryReservation represents a business-level temporary inventory reservation.
type InventoryReservation struct {
	ID         string    `json:"id"`
	OrderID    string    `json:"order_id"`
	MenuItemID string    `json:"menu_item_id"`
	Quantity   int       `json:"quantity"`
	Status     string    `json:"status"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
}
