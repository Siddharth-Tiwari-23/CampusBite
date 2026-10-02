package ws

import "time"

// WebSocket event type constants
const (
	EventOrderStatusUpdated = "ORDER_STATUS_UPDATED"
	EventNewOrder           = "NEW_ORDER"
	EventNotification       = "NOTIFICATION"
)

// Event represents a structured, lightweight WebSocket event payload.
type Event struct {
	Type      string      `json:"type"`
	OrderID   string      `json:"order_id,omitempty"`
	Status    string      `json:"status,omitempty"`
	Payload   interface{} `json:"payload,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// NewOrderStatusUpdatedEvent creates an event for an order status transition.
func NewOrderStatusUpdatedEvent(orderID, status string) Event {
	return Event{
		Type:      EventOrderStatusUpdated,
		OrderID:   orderID,
		Status:    status,
		Timestamp: time.Now().UTC(),
	}
}

// NewOrderCreatedEvent creates an event for a newly placed order (for admin notifications).
func NewOrderCreatedEvent(orderID, userID string, totalAmount float64) Event {
	return Event{
		Type:    EventNewOrder,
		OrderID: orderID,
		Payload: map[string]interface{}{
			"user_id":      userID,
			"total_amount": totalAmount,
		},
		Timestamp: time.Now().UTC(),
	}
}

// NewNotificationEvent creates a generic notification event.
func NewNotificationEvent(message string, data interface{}) Event {
	return Event{
		Type: EventNotification,
		Payload: map[string]interface{}{
			"message": message,
			"data":    data,
		},
		Timestamp: time.Now().UTC(),
	}
}
