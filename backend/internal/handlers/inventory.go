package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"

	"campusbite/internal/cache"
	"campusbite/internal/models"
	"campusbite/internal/repository"

	"github.com/gin-gonic/gin"
)

// InventoryHandler handles HTTP requests for inventory management.
type InventoryHandler struct {
	inventoryRepo *repository.InventoryRepository
	cacheService  cache.CacheService
}

// NewInventoryHandler creates a new InventoryHandler instance.
func NewInventoryHandler(inventoryRepo *repository.InventoryRepository, cacheService ...cache.CacheService) *InventoryHandler {
	var cs cache.CacheService
	if len(cacheService) > 0 {
		cs = cacheService[0]
	}
	return &InventoryHandler{
		inventoryRepo: inventoryRepo,
		cacheService:  cs,
	}
}

// GetInventory returns the current inventory quantities for all items.
func (h *InventoryHandler) GetInventory(c *gin.Context) {
	items, err := h.inventoryRepo.GetAll(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve inventory"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"inventory": items,
	})
}

// GetInventoryItem returns the inventory stock for a specific menu item.
func (h *InventoryHandler) GetInventoryItem(c *gin.Context) {
	menuItemID := c.Param("menuItemId")
	if !isValidUUID(menuItemID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid menu item ID format"})
		return
	}

	item, err := h.inventoryRepo.GetByMenuItemID(c.Request.Context(), menuItemID)
	if err != nil {
		if errors.Is(err, repository.ErrInventoryNotFound) || errors.Is(err, repository.ErrMenuItemNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "inventory not found for menu item"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve inventory item"})
		return
	}

	c.JSON(http.StatusOK, item)
}

// UpdateInventory updates the stock quantity for a menu item (ADMIN only).
func (h *InventoryHandler) UpdateInventory(c *gin.Context) {
	menuItemID := c.Param("menuItemId")
	if !isValidUUID(menuItemID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid menu item ID format"})
		return
	}

	var req models.UpdateInventoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	if req.Quantity == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity is required"})
		return
	}
	if *req.Quantity < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity must not be negative"})
		return
	}

	item, err := h.inventoryRepo.Update(c.Request.Context(), menuItemID, *req.Quantity)
	if err != nil {
		if errors.Is(err, repository.ErrMenuItemNotFound) || errors.Is(err, repository.ErrInventoryNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "menu item not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update inventory"})
		return
	}

	// Invalidate menu cache after inventory modification
	h.invalidateCache(c.Request.Context())

	c.JSON(http.StatusOK, item)
}

func (h *InventoryHandler) invalidateCache(ctx context.Context) {
	if h.cacheService != nil {
		if err := h.cacheService.Del(ctx, cache.KeyMenuAvailable); err != nil {
			log.Printf("[InventoryHandler] Warning: failed to invalidate cache key '%s': %v", cache.KeyMenuAvailable, err)
		}
	}
}
