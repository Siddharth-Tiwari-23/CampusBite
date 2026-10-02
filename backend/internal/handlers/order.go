package handlers

import (
	"errors"
	"net/http"
	"strings"

	"campusbite/internal/middleware"
	"campusbite/internal/models"
	"campusbite/internal/repository"
	"campusbite/internal/ws"

	"github.com/gin-gonic/gin"
)

// OrderHandler handles HTTP requests for order creation, history, and status updates.
type OrderHandler struct {
	orderRepo *repository.OrderRepository
	hub       *ws.Hub
}

// NewOrderHandler creates a new OrderHandler instance.
func NewOrderHandler(orderRepo *repository.OrderRepository, hub ...*ws.Hub) *OrderHandler {
	var wsHub *ws.Hub
	if len(hub) > 0 {
		wsHub = hub[0]
	}
	return &OrderHandler{
		orderRepo: orderRepo,
		hub:       wsHub,
	}
}

// CreateOrder places an order from the user's active cart in PENDING status.
// It requires an Idempotency-Key header to ensure atomic, safe deduplication of checkouts.
func (h *OrderHandler) CreateOrder(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key header is required"})
		return
	}
	if len(idempotencyKey) > 255 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key must not exceed 255 characters"})
		return
	}

	order, err := h.orderRepo.CreateFromCart(c.Request.Context(), userID, idempotencyKey, "")
	if err != nil {
		if errors.Is(err, repository.ErrIdempotencyProcessing) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, repository.ErrIdempotencyFailed) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, repository.ErrEmptyCart) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot create order from an empty cart"})
			return
		}
		if errors.Is(err, repository.ErrInsufficientInventory) || strings.Contains(err.Error(), "insufficient") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, repository.ErrMenuItemUnavailable) || strings.Contains(err.Error(), "available") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create order"})
		return
	}

	// Notify connected admins in real time about the new order after successful commit
	if h.hub != nil {
		h.hub.BroadcastToAdmins(ws.NewOrderCreatedEvent(order.ID, order.UserID, order.TotalAmount))
	}

	c.JSON(http.StatusCreated, order)
}

// GetOrder returns details of a single order by ID.
// Students can only view their own orders; Admins can view any order.
func (h *OrderHandler) GetOrder(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	userRole, _ := middleware.GetUserRole(c)

	orderID := c.Param("id")
	if !isValidUUID(orderID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order ID format"})
		return
	}

	order, err := h.orderRepo.GetByID(c.Request.Context(), orderID)
	if err != nil {
		if errors.Is(err, repository.ErrOrderNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve order"})
		return
	}

	// Authorization check: Students can only view their own orders
	if userRole != models.RoleAdmin && order.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: you do not have permission to view this order"})
		return
	}

	c.JSON(http.StatusOK, order)
}

// ListOrders returns order history.
// Students see only their own orders; Admins see all orders.
func (h *OrderHandler) ListOrders(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	userRole, _ := middleware.GetUserRole(c)

	var orders []models.OrderResponse
	var err error

	if userRole == models.RoleAdmin {
		orders, err = h.orderRepo.ListAll(c.Request.Context())
	} else {
		orders, err = h.orderRepo.ListByUserID(c.Request.Context(), userID)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve orders"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"orders": orders,
	})
}

type updateOrderStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// UpdateOrderStatus updates an order status (ADMIN only) and emits a real-time event to the owner.
// PATCH /api/v1/orders/:id/status
func (h *OrderHandler) UpdateOrderStatus(c *gin.Context) {
	orderID := c.Param("id")
	if !isValidUUID(orderID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order ID format"})
		return
	}

	var req updateOrderStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	req.Status = strings.ToUpper(strings.TrimSpace(req.Status))

	order, err := h.orderRepo.UpdateStatus(c.Request.Context(), orderID, req.Status)
	if err != nil {
		if errors.Is(err, repository.ErrOrderNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Notify the order owner in real time over WebSocket strictly after successful DB commit
	if h.hub != nil {
		h.hub.SendToUser(order.UserID, ws.NewOrderStatusUpdatedEvent(order.ID, req.Status))
	}

	c.JSON(http.StatusOK, order)
}
