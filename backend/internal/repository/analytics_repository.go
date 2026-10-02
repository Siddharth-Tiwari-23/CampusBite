package repository

import (
	"context"
	"fmt"
	"time"

	"campusbite/internal/database"
	"campusbite/internal/models"
)

// AnalyticsRepository executes predefined, parameterized SQL queries for business analytics.
type AnalyticsRepository struct {
	db *database.DB
}

// NewAnalyticsRepository creates a new AnalyticsRepository.
func NewAnalyticsRepository(db *database.DB) *AnalyticsRepository {
	return &AnalyticsRepository{db: db}
}

// GetTopSellingItems returns ranking of items by units sold within the specified time range.
func (r *AnalyticsRepository) GetTopSellingItems(ctx context.Context, start, end time.Time, limit int) ([]models.TopSellingItemResult, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	query := `
		SELECT 
			m.name AS item_name,
			COALESCE(SUM(oi.quantity), 0)::BIGINT AS units_sold,
			ROUND(COALESCE(SUM(oi.quantity * oi.unit_price), 0)::NUMERIC, 2) AS total_revenue
		FROM order_items oi
		JOIN orders o ON oi.order_id = o.id
		JOIN menu_items m ON oi.menu_item_id = m.id
		WHERE o.status IN ('CONFIRMED', 'PREPARING', 'READY', 'COMPLETED')
		  AND o.created_at >= $1
		  AND o.created_at < $2
		GROUP BY m.id, m.name
		ORDER BY units_sold DESC, total_revenue DESC
		LIMIT $3
	`

	rows, err := r.db.Pool.Query(ctx, query, start, end, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query top selling items: %w", err)
	}
	defer rows.Close()

	results := make([]models.TopSellingItemResult, 0)
	for rows.Next() {
		var item models.TopSellingItemResult
		if err := rows.Scan(&item.ItemName, &item.UnitsSold, &item.TotalRevenue); err != nil {
			return nil, fmt.Errorf("failed to scan top selling item row: %w", err)
		}
		results = append(results, item)
	}

	return results, rows.Err()
}

// GetRevenueSummary returns overall revenue metrics within the specified time range.
func (r *AnalyticsRepository) GetRevenueSummary(ctx context.Context, start, end time.Time) (*models.RevenueSummaryResult, error) {
	query := `
		SELECT 
			COALESCE(COUNT(id), 0)::BIGINT AS total_orders,
			ROUND(COALESCE(SUM(total_amount), 0)::NUMERIC, 2) AS total_revenue,
			ROUND(COALESCE(AVG(total_amount), 0)::NUMERIC, 2) AS average_order_value
		FROM orders
		WHERE status IN ('CONFIRMED', 'PREPARING', 'READY', 'COMPLETED')
		  AND created_at >= $1
		  AND created_at < $2
	`

	var res models.RevenueSummaryResult
	err := r.db.Pool.QueryRow(ctx, query, start, end).Scan(
		&res.TotalOrders,
		&res.TotalRevenue,
		&res.AverageOrderValue,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query revenue summary: %w", err)
	}

	return &res, nil
}

// GetOrderCount returns breakdown of orders across lifecycle statuses.
func (r *AnalyticsRepository) GetOrderCount(ctx context.Context, start, end time.Time) (*models.OrderCountResult, error) {
	query := `
		SELECT 
			COALESCE(COUNT(id), 0)::BIGINT AS total_orders,
			COALESCE(COUNT(id) FILTER (WHERE status = 'COMPLETED'), 0)::BIGINT AS completed_orders,
			COALESCE(COUNT(id) FILTER (WHERE status IN ('CONFIRMED', 'PREPARING', 'READY')), 0)::BIGINT AS in_progress_orders,
			COALESCE(COUNT(id) FILTER (WHERE status = 'CANCELLED'), 0)::BIGINT AS cancelled_orders
		FROM orders
		WHERE created_at >= $1
		  AND created_at < $2
	`

	var res models.OrderCountResult
	err := r.db.Pool.QueryRow(ctx, query, start, end).Scan(
		&res.TotalOrders,
		&res.CompletedOrders,
		&res.InProgressOrders,
		&res.CancelledOrders,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query order count: %w", err)
	}

	return &res, nil
}

// GetAverageOrderValue returns the AOV along with minimum and maximum order totals.
func (r *AnalyticsRepository) GetAverageOrderValue(ctx context.Context, start, end time.Time) (*models.AverageOrderValueResult, error) {
	query := `
		SELECT 
			ROUND(COALESCE(AVG(total_amount), 0)::NUMERIC, 2) AS average_order_value,
			ROUND(COALESCE(MIN(total_amount), 0)::NUMERIC, 2) AS min_order_value,
			ROUND(COALESCE(MAX(total_amount), 0)::NUMERIC, 2) AS max_order_value,
			COALESCE(COUNT(id), 0)::BIGINT AS total_qualifying_orders
		FROM orders
		WHERE status IN ('CONFIRMED', 'PREPARING', 'READY', 'COMPLETED')
		  AND created_at >= $1
		  AND created_at < $2
	`

	var res models.AverageOrderValueResult
	err := r.db.Pool.QueryRow(ctx, query, start, end).Scan(
		&res.AverageOrderValue,
		&res.MinOrderValue,
		&res.MaxOrderValue,
		&res.TotalQualifyingOrders,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query average order value: %w", err)
	}

	return &res, nil
}

// GetBestSellingCategories returns ranking of menu item catalog segments by units sold.
func (r *AnalyticsRepository) GetBestSellingCategories(ctx context.Context, start, end time.Time, limit int) ([]models.BestSellingCategoryResult, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	query := `
		SELECT 
			m.name AS category_or_item,
			COALESCE(SUM(oi.quantity), 0)::BIGINT AS units_sold,
			ROUND(COALESCE(SUM(oi.quantity * oi.unit_price), 0)::NUMERIC, 2) AS total_revenue
		FROM order_items oi
		JOIN orders o ON oi.order_id = o.id
		JOIN menu_items m ON oi.menu_item_id = m.id
		WHERE o.status IN ('CONFIRMED', 'PREPARING', 'READY', 'COMPLETED')
		  AND o.created_at >= $1
		  AND o.created_at < $2
		GROUP BY m.id, m.name
		ORDER BY units_sold DESC
		LIMIT $3
	`

	rows, err := r.db.Pool.Query(ctx, query, start, end, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query best selling categories: %w", err)
	}
	defer rows.Close()

	results := make([]models.BestSellingCategoryResult, 0)
	for rows.Next() {
		var item models.BestSellingCategoryResult
		if err := rows.Scan(&item.CategoryOrItem, &item.UnitsSold, &item.TotalRevenue); err != nil {
			return nil, fmt.Errorf("failed to scan best selling category row: %w", err)
		}
		results = append(results, item)
	}

	return results, rows.Err()
}

// GetSalesByDay returns aggregate daily revenue and orders count.
func (r *AnalyticsRepository) GetSalesByDay(ctx context.Context, start, end time.Time) ([]models.SalesByDayResult, error) {
	query := `
		SELECT 
			TO_CHAR(DATE_TRUNC('day', created_at), 'YYYY-MM-DD') AS day,
			COALESCE(COUNT(id), 0)::BIGINT AS orders_count,
			ROUND(COALESCE(SUM(total_amount), 0)::NUMERIC, 2) AS daily_revenue
		FROM orders
		WHERE status IN ('CONFIRMED', 'PREPARING', 'READY', 'COMPLETED')
		  AND created_at >= $1
		  AND created_at < $2
		GROUP BY DATE_TRUNC('day', created_at)
		ORDER BY DATE_TRUNC('day', created_at) ASC
	`

	rows, err := r.db.Pool.Query(ctx, query, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to query sales by day: %w", err)
	}
	defer rows.Close()

	results := make([]models.SalesByDayResult, 0)
	for rows.Next() {
		var item models.SalesByDayResult
		if err := rows.Scan(&item.Day, &item.OrdersCount, &item.DailyRevenue); err != nil {
			return nil, fmt.Errorf("failed to scan sales by day row: %w", err)
		}
		results = append(results, item)
	}

	return results, rows.Err()
}

// GetItemTrends executes a parameterized SQL aggregation calculating item-by-item sales comparisons between current and previous periods.
func (r *AnalyticsRepository) GetItemTrends(ctx context.Context, currentStart, currentEnd, prevStart, prevEnd time.Time) ([]models.ItemTrendRawDBResult, error) {
	query := `
		SELECT 
			m.id::text AS item_id,
			m.name AS item_name,
			COALESCE(SUM(oi.quantity) FILTER (WHERE o.created_at >= $1 AND o.created_at < $2), 0)::BIGINT AS current_units,
			COALESCE(SUM(oi.quantity) FILTER (WHERE o.created_at >= $3 AND o.created_at < $4), 0)::BIGINT AS previous_units,
			ROUND(COALESCE(SUM(oi.quantity * oi.unit_price) FILTER (WHERE o.created_at >= $1 AND o.created_at < $2), 0)::NUMERIC, 2) AS current_revenue,
			ROUND(COALESCE(SUM(oi.quantity * oi.unit_price) FILTER (WHERE o.created_at >= $3 AND o.created_at < $4), 0)::NUMERIC, 2) AS previous_revenue
		FROM menu_items m
		LEFT JOIN order_items oi ON m.id = oi.menu_item_id
		LEFT JOIN orders o ON oi.order_id = o.id 
		                   AND o.status IN ('CONFIRMED', 'PREPARING', 'READY', 'COMPLETED')
		                   AND ((o.created_at >= $1 AND o.created_at < $2) OR (o.created_at >= $3 AND o.created_at < $4))
		GROUP BY m.id, m.name
		HAVING COALESCE(SUM(oi.quantity) FILTER (WHERE o.created_at >= $1 AND o.created_at < $2), 0) > 0 
		    OR COALESCE(SUM(oi.quantity) FILTER (WHERE o.created_at >= $3 AND o.created_at < $4), 0) > 0
		ORDER BY current_units DESC, previous_units DESC, m.name ASC
	`

	rows, err := r.db.Pool.Query(ctx, query, currentStart, currentEnd, prevStart, prevEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to query item trends: %w", err)
	}
	defer rows.Close()

	results := make([]models.ItemTrendRawDBResult, 0)
	for rows.Next() {
		var item models.ItemTrendRawDBResult
		if err := rows.Scan(
			&item.ItemID,
			&item.ItemName,
			&item.CurrentUnits,
			&item.PreviousUnits,
			&item.CurrentRevenue,
			&item.PreviousRevenue,
		); err != nil {
			return nil, fmt.Errorf("failed to scan item trend row: %w", err)
		}
		results = append(results, item)
	}

	return results, rows.Err()
}
