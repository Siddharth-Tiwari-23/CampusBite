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
	ErrInventoryNotFound = errors.New("inventory item not found")
)

// InventoryRepository handles inventory data persistence operations.
type InventoryRepository struct {
	db *database.DB
}

// NewInventoryRepository creates a new InventoryRepository instance.
func NewInventoryRepository(db *database.DB) *InventoryRepository {
	return &InventoryRepository{db: db}
}

// GetAll retrieves all inventory items.
func (r *InventoryRepository) GetAll(ctx context.Context) ([]models.InventoryItem, error) {
	query := `
		SELECT menu_item_id, quantity, updated_at
		FROM inventory
		ORDER BY updated_at DESC
	`

	rows, err := r.db.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query inventory items: %w", err)
	}
	defer rows.Close()

	items := make([]models.InventoryItem, 0)
	for rows.Next() {
		var item models.InventoryItem
		if err := rows.Scan(
			&item.MenuItemID,
			&item.Quantity,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan inventory item: %w", err)
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

// GetByMenuItemID retrieves the inventory record for a specific menu item.
func (r *InventoryRepository) GetByMenuItemID(ctx context.Context, menuItemId string) (*models.InventoryItem, error) {
	query := `
		SELECT menu_item_id, quantity, updated_at
		FROM inventory
		WHERE menu_item_id = $1
	`

	var item models.InventoryItem
	err := r.db.Pool.QueryRow(ctx, query, menuItemId).Scan(
		&item.MenuItemID,
		&item.Quantity,
		&item.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInventoryNotFound
		}
		return nil, fmt.Errorf("failed to query inventory item: %w", err)
	}

	return &item, nil
}

// Update updates the inventory quantity for a menu item.
func (r *InventoryRepository) Update(ctx context.Context, menuItemId string, quantity int) (*models.InventoryItem, error) {
	// First verify that the referenced menu item exists in menu_items
	checkMenuQuery := `SELECT id FROM menu_items WHERE id = $1`
	var menuID string
	err := r.db.Pool.QueryRow(ctx, checkMenuQuery, menuItemId).Scan(&menuID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMenuItemNotFound
		}
		return nil, fmt.Errorf("failed to verify menu item existence: %w", err)
	}

	// Insert or update inventory for the menu item
	upsertQuery := `
		INSERT INTO inventory (menu_item_id, quantity, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (menu_item_id)
		DO UPDATE SET quantity = $2, updated_at = NOW()
		RETURNING menu_item_id, quantity, updated_at
	`

	var item models.InventoryItem
	err = r.db.Pool.QueryRow(ctx, upsertQuery, menuItemId, quantity).Scan(
		&item.MenuItemID,
		&item.Quantity,
		&item.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to update inventory: %w", err)
	}

	return &item, nil
}

// GetByMenuItemIDForUpdate locks the inventory row for update within an active transaction.
// Designed for the future atomic checkout reservation flow.
func (r *InventoryRepository) GetByMenuItemIDForUpdate(ctx context.Context, tx pgx.Tx, menuItemId string) (*models.InventoryItem, error) {
	query := `
		SELECT menu_item_id, quantity, updated_at
		FROM inventory
		WHERE menu_item_id = $1
		FOR UPDATE
	`

	var item models.InventoryItem
	err := tx.QueryRow(ctx, query, menuItemId).Scan(
		&item.MenuItemID,
		&item.Quantity,
		&item.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInventoryNotFound
		}
		return nil, fmt.Errorf("failed to lock inventory row: %w", err)
	}

	return &item, nil
}
