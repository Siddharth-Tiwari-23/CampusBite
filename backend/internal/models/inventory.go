package models

import "time"

// InventoryItem represents the database entity for inventory tracking.
type InventoryItem struct {
	MenuItemID string    `json:"menu_item_id"`
	Quantity   int       `json:"quantity"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// UpdateInventoryRequest represents payload for updating inventory quantity.
type UpdateInventoryRequest struct {
	Quantity *int `json:"quantity"`
}
