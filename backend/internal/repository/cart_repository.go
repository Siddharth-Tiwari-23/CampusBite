package repository

import (
	"context"
	"errors"
	"fmt"

	"campusbite/internal/database"
	"campusbite/internal/models"

	"github.com/jackc/pgx/v5"
)

var (
	ErrCartItemNotFound    = errors.New("cart item not found")
	ErrCartNotFound        = errors.New("cart not found")
	ErrCartItemUnavailable = errors.New("menu item is unavailable")
)

// CartRepository handles database operations for student carts.
type CartRepository struct {
	db *database.DB
}

// NewCartRepository creates a new CartRepository.
func NewCartRepository(db *database.DB) *CartRepository {
	return &CartRepository{db: db}
}

// GetOrCreateCartID retrieves existing cart ID or creates a new one for the user.
func (r *CartRepository) GetOrCreateCartID(ctx context.Context, userID string) (string, error) {
	query := `
		INSERT INTO carts (user_id, updated_at)
		VALUES ($1, NOW())
		ON CONFLICT (user_id) DO UPDATE SET updated_at = NOW()
		RETURNING id
	`
	var cartID string
	err := r.db.Pool.QueryRow(ctx, query, userID).Scan(&cartID)
	if err != nil {
		return "", fmt.Errorf("failed to get or create cart: %w", err)
	}
	return cartID, nil
}

// GetCart retrieves the user's cart along with all items and computed totals.
func (r *CartRepository) GetCart(ctx context.Context, userID string) (*models.CartResponse, error) {
	cartID, err := r.GetOrCreateCartID(ctx, userID)
	if err != nil {
		return nil, err
	}

	query := `
		SELECT 
			ci.menu_item_id,
			m.name,
			m.price,
			ci.quantity,
			ROUND((ci.quantity * m.price)::numeric, 2) AS subtotal
		FROM cart_items ci
		JOIN menu_items m ON ci.menu_item_id = m.id
		WHERE ci.cart_id = $1
		ORDER BY m.name ASC
	`

	rows, err := r.db.Pool.Query(ctx, query, cartID)
	if err != nil {
		return nil, fmt.Errorf("failed to query cart items: %w", err)
	}
	defer rows.Close()

	items := make([]models.CartItemResponse, 0)
	var totalAmount float64

	for rows.Next() {
		var item models.CartItemResponse
		if err := rows.Scan(
			&item.MenuItemID,
			&item.Name,
			&item.Price,
			&item.Quantity,
			&item.Subtotal,
		); err != nil {
			return nil, fmt.Errorf("failed to scan cart item: %w", err)
		}
		totalAmount += item.Subtotal
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating cart items: %w", err)
	}

	return &models.CartResponse{
		CartID:      cartID,
		UserID:      userID,
		Items:       items,
		TotalAmount: totalAmount,
	}, nil
}

// AddItem adds an item or increments its quantity in the user's cart.
func (r *CartRepository) AddItem(ctx context.Context, userID string, menuItemID string, quantity int) (*models.CartResponse, error) {
	// Verify menu item exists and is available
	var isAvailable bool
	err := r.db.Pool.QueryRow(ctx, "SELECT is_available FROM menu_items WHERE id = $1", menuItemID).Scan(&isAvailable)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMenuItemNotFound
		}
		return nil, fmt.Errorf("failed to check menu item: %w", err)
	}

	if !isAvailable {
		return nil, ErrCartItemUnavailable
	}

	cartID, err := r.GetOrCreateCartID(ctx, userID)
	if err != nil {
		return nil, err
	}

	upsertQuery := `
		INSERT INTO cart_items (cart_id, menu_item_id, quantity)
		VALUES ($1, $2, $3)
		ON CONFLICT (cart_id, menu_item_id)
		DO UPDATE SET quantity = cart_items.quantity + EXCLUDED.quantity
	`

	_, err = r.db.Pool.Exec(ctx, upsertQuery, cartID, menuItemID, quantity)
	if err != nil {
		return nil, fmt.Errorf("failed to add item to cart: %w", err)
	}

	return r.GetCart(ctx, userID)
}

// UpdateItemQuantity updates the quantity of a specific item in the user's cart.
func (r *CartRepository) UpdateItemQuantity(ctx context.Context, userID string, menuItemID string, quantity int) (*models.CartResponse, error) {
	cartID, err := r.GetOrCreateCartID(ctx, userID)
	if err != nil {
		return nil, err
	}

	query := `
		UPDATE cart_items
		SET quantity = $3
		WHERE cart_id = $1 AND menu_item_id = $2
	`

	cmdTag, err := r.db.Pool.Exec(ctx, query, cartID, menuItemID, quantity)
	if err != nil {
		return nil, fmt.Errorf("failed to update cart item quantity: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return nil, ErrCartItemNotFound
	}

	return r.GetCart(ctx, userID)
}

// DeleteItem removes a single item from the user's cart.
func (r *CartRepository) DeleteItem(ctx context.Context, userID string, menuItemID string) error {
	cartID, err := r.GetOrCreateCartID(ctx, userID)
	if err != nil {
		return err
	}

	query := `
		DELETE FROM cart_items
		WHERE cart_id = $1 AND menu_item_id = $2
	`

	cmdTag, err := r.db.Pool.Exec(ctx, query, cartID, menuItemID)
	if err != nil {
		return fmt.Errorf("failed to delete cart item: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return ErrCartItemNotFound
	}

	return nil
}

// ClearCart removes all items from the user's cart.
func (r *CartRepository) ClearCart(ctx context.Context, userID string) error {
	cartID, err := r.GetOrCreateCartID(ctx, userID)
	if err != nil {
		return err
	}

	query := `
		DELETE FROM cart_items
		WHERE cart_id = $1
	`

	_, err = r.db.Pool.Exec(ctx, query, cartID)
	if err != nil {
		return fmt.Errorf("failed to clear cart: %w", err)
	}

	return nil
}
