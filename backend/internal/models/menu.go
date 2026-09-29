package models

import "time"

// MenuItem represents the database entity for a menu item.
type MenuItem struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Price       float64   `json:"price"`
	IsAvailable bool      `json:"is_available"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateMenuItemRequest represents payload for creating a menu item.
type CreateMenuItemRequest struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Price           *float64 `json:"price"`
	IsAvailable     *bool    `json:"is_available"`
	InitialQuantity *int     `json:"initial_quantity"`
}

// UpdateMenuItemRequest represents payload for updating an existing menu item.
type UpdateMenuItemRequest struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	Price       *float64 `json:"price"`
	IsAvailable *bool    `json:"is_available"`
}
