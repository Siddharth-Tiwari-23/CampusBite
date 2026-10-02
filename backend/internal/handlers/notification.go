package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"campusbite/internal/middleware"
	"campusbite/internal/repository"
	"campusbite/internal/ws"

	"github.com/gin-gonic/gin"
)

// NotificationHandler handles HTTP requests for user notifications.
type NotificationHandler struct {
	notifRepo *repository.NotificationRepository
	hub       *ws.Hub
}

// NewNotificationHandler creates a new NotificationHandler.
func NewNotificationHandler(notifRepo *repository.NotificationRepository, hub ...*ws.Hub) *NotificationHandler {
	var wsHub *ws.Hub
	if len(hub) > 0 {
		wsHub = hub[0]
	}
	return &NotificationHandler{
		notifRepo: notifRepo,
		hub:       wsHub,
	}
}

// ListNotifications returns the paginated notifications for the authenticated user.
// GET /api/v1/notifications
func (h *NotificationHandler) ListNotifications(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	limit := 20
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	offset := 0
	if o := c.Query("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	resp, err := h.notifRepo.ListByUser(c.Request.Context(), userID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list notifications"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// MarkRead marks a single notification as read for the authenticated user.
// PATCH /api/v1/notifications/:id/read
func (h *NotificationHandler) MarkRead(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	notificationID := c.Param("id")
	if !isValidUUID(notificationID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid notification ID format"})
		return
	}

	notif, err := h.notifRepo.MarkAsRead(c.Request.Context(), userID, notificationID)
	if err != nil {
		if errors.Is(err, repository.ErrNotificationNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark notification as read"})
		return
	}

	c.JSON(http.StatusOK, notif)
}

// MarkAllRead marks all unread notifications as read for the authenticated user.
// POST /api/v1/notifications/read-all
func (h *NotificationHandler) MarkAllRead(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	count, err := h.notifRepo.MarkAllAsRead(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark all notifications as read"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      "all notifications marked as read",
		"marked_count": count,
	})
}
