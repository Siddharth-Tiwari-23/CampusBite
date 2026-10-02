package models

// CartItemResponse represents an item inside a cart with current catalog details.
type CartItemResponse struct {
	MenuItemID string  `json:"menu_item_id"`
	Name       string  `json:"name"`
	Price      float64 `json:"price"`
	ImageURL   string  `json:"image_url,omitempty"`
	Quantity   int     `json:"quantity"`
	Subtotal   float64 `json:"subtotal"`
}

// CartResponse represents the complete cart with items and server-calculated total.
type CartResponse struct {
	CartID      string             `json:"cart_id"`
	UserID      string             `json:"user_id"`
	Items       []CartItemResponse `json:"items"`
	TotalAmount float64            `json:"total_amount"`
}

// AddToCartRequest represents the payload for adding an item to the cart.
type AddToCartRequest struct {
	MenuItemID string `json:"menu_item_id"`
	ItemID     string `json:"item_id,omitempty"`
	Quantity   int    `json:"quantity"`
}

// UpdateCartItemRequest represents the payload for updating an item's quantity in the cart.
type UpdateCartItemRequest struct {
	Quantity int `json:"quantity"`
}
