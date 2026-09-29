package handlers

import (
	"errors"
	"net/http"
	"strings"

	"campusbite/internal/middleware"
	"campusbite/internal/models"
	"campusbite/internal/repository"

	"github.com/gin-gonic/gin"
)

// OrderHandler handles HTTP requests for order creation and history.
type OrderHandler struct {
	orderRepo *repository.OrderRepository
}

// NewOrderHandler creates a new OrderHandler instance.
func NewOrderHandler(orderRepo *repository.OrderRepository) *OrderHandler {
	return &OrderHandler{orderRepo: orderRepo}
}

// CreateOrder places an order from the user's active cart in PENDING status.
func (h *OrderHandler) CreateOrder(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	order, err := h.orderRepo.CreateFromCart(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, repository.ErrEmptyCart) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot create order from an empty cart"})
			return
		}
		if strings.Contains(err.Error(), "available") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create order"})
		return
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
