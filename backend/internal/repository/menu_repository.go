package repository

import (
	"context"
	"errors"
	"fmt"

	"campusbite/internal/database"
	"campusbite/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrMenuItemNotFound = errors.New("menu item not found")
)

// MenuRepository handles menu item data persistence operations.
type MenuRepository struct {
	db *database.DB
}

// NewMenuRepository creates a new MenuRepository instance.
func NewMenuRepository(db *database.DB) *MenuRepository {
	return &MenuRepository{db: db}
}

// GetAll retrieves all menu items ordered by creation date.
func (r *MenuRepository) GetAll(ctx context.Context) ([]models.MenuItem, error) {
	query := `
		SELECT id, name, COALESCE(description, ''), price, is_available, created_at, updated_at
		FROM menu_items
		ORDER BY created_at ASC
	`

	rows, err := r.db.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query menu items: %w", err)
	}
	defer rows.Close()

	items := make([]models.MenuItem, 0)
	for rows.Next() {
		var item models.MenuItem
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.Description,
			&item.Price,
			&item.IsAvailable,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan menu item: %w", err)
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

// GetByID retrieves a single menu item by its UUID.
func (r *MenuRepository) GetByID(ctx context.Context, id string) (*models.MenuItem, error) {
	query := `
		SELECT id, name, COALESCE(description, ''), price, is_available, created_at, updated_at
		FROM menu_items
		WHERE id = $1
	`

	var item models.MenuItem
	err := r.db.Pool.QueryRow(ctx, query, id).Scan(
		&item.ID,
		&item.Name,
		&item.Description,
		&item.Price,
		&item.IsAvailable,
		&item.CreatedAt,
		&item.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMenuItemNotFound
		}
		return nil, fmt.Errorf("failed to query menu item by id: %w", err)
	}

	return &item, nil
}

// Create inserts a new menu item and its initial inventory record in a single transaction.
func (r *MenuRepository) Create(ctx context.Context, name, description string, price float64, isAvailable bool, initialQuantity int) (*models.MenuItem, error) {
	tx, err := r.db.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	menuQuery := `
		INSERT INTO menu_items (name, description, price, is_available)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, COALESCE(description, ''), price, is_available, created_at, updated_at
	`

	var item models.MenuItem
	err = tx.QueryRow(ctx, menuQuery, name, description, price, isAvailable).Scan(
		&item.ID,
		&item.Name,
		&item.Description,
		&item.Price,
		&item.IsAvailable,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert menu item: %w", err)
	}

	inventoryQuery := `
		INSERT INTO inventory (menu_item_id, quantity)
		VALUES ($1, $2)
		ON CONFLICT (menu_item_id) DO UPDATE SET quantity = $2, updated_at = NOW()
	`
	if _, err := tx.Exec(ctx, inventoryQuery, item.ID, initialQuantity); err != nil {
		return nil, fmt.Errorf("failed to initialize inventory for menu item: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &item, nil
}

// Update updates selected fields of an existing menu item.
func (r *MenuRepository) Update(ctx context.Context, id string, name *string, description *string, price *float64, isAvailable *bool) (*models.MenuItem, error) {
	current, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	newName := current.Name
	if name != nil {
		newName = *name
	}

	newDesc := current.Description
	if description != nil {
		newDesc = *description
	}

	newPrice := current.Price
	if price != nil {
		newPrice = *price
	}

	newAvailable := current.IsAvailable
	if isAvailable != nil {
		newAvailable = *isAvailable
	}

	updateQuery := `
		UPDATE menu_items
		SET name = $1, description = $2, price = $3, is_available = $4, updated_at = NOW()
		WHERE id = $5
		RETURNING id, name, COALESCE(description, ''), price, is_available, created_at, updated_at
	`

	var item models.MenuItem
	err = r.db.Pool.QueryRow(ctx, updateQuery, newName, newDesc, newPrice, newAvailable, id).Scan(
		&item.ID,
		&item.Name,
		&item.Description,
		&item.Price,
		&item.IsAvailable,
		&item.CreatedAt,
		&item.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMenuItemNotFound
		}
		return nil, fmt.Errorf("failed to update menu item: %w", err)
	}

	return &item, nil
}

// Delete removes the menu item or soft-disables it if referenced in historical orders.
func (r *MenuRepository) Delete(ctx context.Context, id string) error {
	// First check if the item exists
	if _, err := r.GetByID(ctx, id); err != nil {
		return err
	}

	deleteQuery := `DELETE FROM menu_items WHERE id = $1`
	_, err := r.db.Pool.Exec(ctx, deleteQuery, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			// Foreign key constraint violation (e.g. historical orders reference this item)
			// Gracefully soft-disable to preserve order history integrity
			softDeleteQuery := `UPDATE menu_items SET is_available = false, updated_at = NOW() WHERE id = $1`
			if _, sErr := r.db.Pool.Exec(ctx, softDeleteQuery, id); sErr != nil {
				return fmt.Errorf("failed to soft-disable menu item: %w", sErr)
			}
			return nil
		}
		return fmt.Errorf("failed to delete menu item: %w", err)
	}

	return nil
}
