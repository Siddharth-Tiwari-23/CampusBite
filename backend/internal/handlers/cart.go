package handlers

import (
	"errors"
	"net/http"

	"campusbite/internal/middleware"
	"campusbite/internal/models"
	"campusbite/internal/repository"

	"github.com/gin-gonic/gin"
)

// CartHandler handles HTTP requests for user shopping carts.
type CartHandler struct {
	cartRepo *repository.CartRepository
}

// NewCartHandler creates a new CartHandler instance.
func NewCartHandler(cartRepo *repository.CartRepository) *CartHandler {
	return &CartHandler{cartRepo: cartRepo}
}

// GetCart returns the authenticated user's cart.
func (h *CartHandler) GetCart(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	cart, err := h.cartRepo.GetCart(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve cart"})
		return
	}

	c.JSON(http.StatusOK, cart)
}

// AddToCart adds a menu item to the authenticated user's cart.
func (h *CartHandler) AddToCart(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req models.AddToCartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	menuItemID := req.MenuItemID
	if menuItemID == "" && req.ItemID != "" {
		menuItemID = req.ItemID
	}

	if !isValidUUID(menuItemID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid menu item ID format"})
		return
	}

	if req.Quantity <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity must be greater than 0"})
		return
	}

	cart, err := h.cartRepo.AddItem(c.Request.Context(), userID, menuItemID, req.Quantity)
	if err != nil {
		if errors.Is(err, repository.ErrMenuItemNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "menu item not found"})
			return
		}
		if errors.Is(err, repository.ErrCartItemUnavailable) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "menu item is unavailable"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add item to cart"})
		return
	}

	c.JSON(http.StatusOK, cart)
}

// UpdateCartItem updates the quantity of a specific item in the cart.
func (h *CartHandler) UpdateCartItem(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	menuItemID := c.Param("menuItemId")
	if !isValidUUID(menuItemID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid menu item ID format"})
		return
	}

	var req models.UpdateCartItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	if req.Quantity <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity must be greater than 0"})
		return
	}

	cart, err := h.cartRepo.UpdateItemQuantity(c.Request.Context(), userID, menuItemID, req.Quantity)
	if err != nil {
		if errors.Is(err, repository.ErrCartItemNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "item not found in cart"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update cart item"})
		return
	}

	c.JSON(http.StatusOK, cart)
}

// DeleteCartItem removes a specific item from the cart.
func (h *CartHandler) DeleteCartItem(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	menuItemID := c.Param("menuItemId")
	if !isValidUUID(menuItemID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid menu item ID format"})
		return
	}

	err := h.cartRepo.DeleteItem(c.Request.Context(), userID, menuItemID)
	if err != nil {
		if errors.Is(err, repository.ErrCartItemNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "item not found in cart"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete cart item"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "item removed from cart successfully",
	})
}

// ClearCart removes all items from the user's cart.
func (h *CartHandler) ClearCart(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	err := h.cartRepo.ClearCart(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear cart"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "cart cleared successfully",
	})
}
