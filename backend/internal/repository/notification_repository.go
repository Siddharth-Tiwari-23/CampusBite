package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"campusbite/internal/database"
	"campusbite/internal/models"

	"github.com/jackc/pgx/v5"
)

var (
	ErrNotificationNotFound = errors.New("notification not found")
)

// NotificationRepository handles database operations for notifications.
type NotificationRepository struct {
	db *database.DB
}

// NewNotificationRepository creates a new NotificationRepository instance.
func NewNotificationRepository(db *database.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

// Create persists a new notification for a user.
func (r *NotificationRepository) Create(
	ctx context.Context,
	userID string,
	notifType string,
	title string,
	message string,
	data map[string]interface{},
) (*models.Notification, error) {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		dataBytes = []byte("{}")
	}

	query := `
		INSERT INTO notifications (user_id, type, title, message, data, is_read, created_at)
		VALUES ($1, $2, $3, $4, $5, false, NOW())
		RETURNING id, user_id, type, title, message, data, is_read, created_at
	`

	var notif models.Notification
	var rawData []byte
	err = r.db.Pool.QueryRow(ctx, query, userID, notifType, title, message, dataBytes).Scan(
		&notif.ID,
		&notif.UserID,
		&notif.Type,
		&notif.Title,
		&notif.Message,
		&rawData,
		&notif.IsRead,
		&notif.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create notification: %w", err)
	}

	if len(rawData) > 0 {
		_ = json.Unmarshal(rawData, &notif.Data)
	}
	if notif.Data == nil {
		notif.Data = make(map[string]interface{})
	}

	return &notif, nil
}

// CreateTx persists a new notification within an ongoing database transaction.
func (r *NotificationRepository) CreateTx(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	notifType string,
	title string,
	message string,
	data map[string]interface{},
) (*models.Notification, error) {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		dataBytes = []byte("{}")
	}

	query := `
		INSERT INTO notifications (user_id, type, title, message, data, is_read, created_at)
		VALUES ($1, $2, $3, $4, $5, false, NOW())
		RETURNING id, user_id, type, title, message, data, is_read, created_at
	`

	var notif models.Notification
	var rawData []byte
	err = tx.QueryRow(ctx, query, userID, notifType, title, message, dataBytes).Scan(
		&notif.ID,
		&notif.UserID,
		&notif.Type,
		&notif.Title,
		&notif.Message,
		&rawData,
		&notif.IsRead,
		&notif.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create notification in tx: %w", err)
	}

	if len(rawData) > 0 {
		_ = json.Unmarshal(rawData, &notif.Data)
	}
	if notif.Data == nil {
		notif.Data = make(map[string]interface{})
	}

	return &notif, nil
}

// ListByUser retrieves a paginated list of notifications for a user, along with unread and total counts.
func (r *NotificationRepository) ListByUser(ctx context.Context, userID string, limit int, offset int) (*models.NotificationListResponse, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	// 1. Get total and unread counts
	countsQuery := `
		SELECT 
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE is_read = false) AS unread
		FROM notifications
		WHERE user_id = $1
	`
	var total, unread int
	err := r.db.Pool.QueryRow(ctx, countsQuery, userID).Scan(&total, &unread)
	if err != nil {
		return nil, fmt.Errorf("failed to query notification counts: %w", err)
	}

	// 2. Query paginated notifications
	query := `
		SELECT id, user_id, type, title, message, data, is_read, created_at
		FROM notifications
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query notifications: %w", err)
	}
	defer rows.Close()

	notifications := make([]models.Notification, 0)
	for rows.Next() {
		var notif models.Notification
		var rawData []byte
		if err := rows.Scan(
			&notif.ID,
			&notif.UserID,
			&notif.Type,
			&notif.Title,
			&notif.Message,
			&rawData,
			&notif.IsRead,
			&notif.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan notification: %w", err)
		}

		if len(rawData) > 0 {
			_ = json.Unmarshal(rawData, &notif.Data)
		}
		if notif.Data == nil {
			notif.Data = make(map[string]interface{})
		}

		notifications = append(notifications, notif)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating notifications: %w", err)
	}

	return &models.NotificationListResponse{
		Notifications: notifications,
		UnreadCount:   unread,
		Total:         total,
		Limit:         limit,
		Offset:        offset,
	}, nil
}

// MarkAsRead marks a specific notification as read for a given user.
func (r *NotificationRepository) MarkAsRead(ctx context.Context, userID string, notificationID string) (*models.Notification, error) {
	query := `
		UPDATE notifications
		SET is_read = true
		WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, type, title, message, data, is_read, created_at
	`

	var notif models.Notification
	var rawData []byte
	err := r.db.Pool.QueryRow(ctx, query, notificationID, userID).Scan(
		&notif.ID,
		&notif.UserID,
		&notif.Type,
		&notif.Title,
		&notif.Message,
		&rawData,
		&notif.IsRead,
		&notif.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotificationNotFound
		}
		return nil, fmt.Errorf("failed to mark notification as read: %w", err)
	}

	if len(rawData) > 0 {
		_ = json.Unmarshal(rawData, &notif.Data)
	}
	if notif.Data == nil {
		notif.Data = make(map[string]interface{})
	}

	return &notif, nil
}

// MarkAllAsRead marks all notifications as read for a given user.
func (r *NotificationRepository) MarkAllAsRead(ctx context.Context, userID string) (int64, error) {
	query := `
		UPDATE notifications
		SET is_read = true
		WHERE user_id = $1 AND is_read = false
	`

	tag, err := r.db.Pool.Exec(ctx, query, userID)
	if err != nil {
		return 0, fmt.Errorf("failed to mark all notifications as read: %w", err)
	}

	return tag.RowsAffected(), nil
}
