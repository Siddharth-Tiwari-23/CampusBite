package models

import (
	"encoding/json"
	"time"
)

// Notification type constants
const (
	NotificationTypeOrderStatus = "ORDER_STATUS"
	NotificationTypePayment     = "PAYMENT"
	NotificationTypeSystem      = "SYSTEM"
)

// Notification represents a persistent user notification.
type Notification struct {
	ID        string                 `json:"id"`
	UserID    string                 `json:"user_id"`
	Type      string                 `json:"type"`
	Title     string                 `json:"title"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data"`
	IsRead    bool                   `json:"is_read"`
	CreatedAt time.Time              `json:"created_at"`
}

// NotificationListResponse represents the response when querying user notifications.
type NotificationListResponse struct {
	Notifications []Notification `json:"notifications"`
	UnreadCount   int            `json:"unread_count"`
	Total         int            `json:"total"`
	Limit         int            `json:"limit"`
	Offset        int            `json:"offset"`
}

// CreateNotificationDTO represents data required to insert a notification.
type CreateNotificationDTO struct {
	UserID  string
	Type    string
	Title   string
	Message string
	Data    map[string]interface{}
}

// MarshalData converts data map to json bytes.
func (d *CreateNotificationDTO) MarshalData() ([]byte, error) {
	if d.Data == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(d.Data)
}
