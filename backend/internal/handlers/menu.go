package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"campusbite/internal/cache"
	"campusbite/internal/models"
	"campusbite/internal/repository"

	"github.com/gin-gonic/gin"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func isValidUUID(id string) bool {
	return uuidRegex.MatchString(id)
}

// MenuHandler handles HTTP requests for menu catalog items with cache-aside read support.
type MenuHandler struct {
	menuRepo     *repository.MenuRepository
	cacheService cache.CacheService
	cacheTTL     time.Duration
}

// NewMenuHandler creates a new MenuHandler instance.
func NewMenuHandler(menuRepo *repository.MenuRepository, cacheService cache.CacheService, cacheTTL ...time.Duration) *MenuHandler {
	ttl := 60 * time.Second
	if len(cacheTTL) > 0 && cacheTTL[0] > 0 {
		ttl = cacheTTL[0]
	}
	return &MenuHandler{
		menuRepo:     menuRepo,
		cacheService: cacheService,
		cacheTTL:     ttl,
	}
}

// GetMenu returns all menu items in the catalog using cache-aside caching.
func (h *MenuHandler) GetMenu(c *gin.Context) {
	ctx := c.Request.Context()
	includeAll := c.Query("all") == "true"

	// 1. Attempt to read from Redis cache (only for public active catalog)
	if !includeAll && h.cacheService != nil {
		cachedData, err := h.cacheService.Get(ctx, cache.KeyMenuAvailable)
		if err == nil && cachedData != "" {
			var items []models.MenuItem
			if unmarshalErr := json.Unmarshal([]byte(cachedData), &items); unmarshalErr == nil {
				c.Header("X-Cache", "HIT")
				c.JSON(http.StatusOK, gin.H{
					"items": items,
				})
				return
			}
		} else if err != nil && !errors.Is(err, cache.ErrCacheMiss) {
			log.Printf("[MenuCache] Redis GET failure for key '%s': %v (falling back to PostgreSQL)", cache.KeyMenuAvailable, err)
		}
	}

	// 2. Cache miss or Redis error -> fetch from authoritative PostgreSQL database
	items, err := h.menuRepo.GetAll(ctx, !includeAll)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve menu items"})
		return
	}

	// 3. Populate Redis cache asynchronously/safely without blocking response on failure
	if !includeAll && h.cacheService != nil {
		if setErr := h.cacheService.Set(ctx, cache.KeyMenuAvailable, items, h.cacheTTL); setErr != nil {
			log.Printf("[MenuCache] Warning: failed to populate Redis cache: %v", setErr)
		}
	}

	c.Header("X-Cache", "MISS")
	c.JSON(http.StatusOK, gin.H{
		"items": items,
	})
}

// GetMenuItem returns details of a single menu item by ID.
func (h *MenuHandler) GetMenuItem(c *gin.Context) {
	id := c.Param("id")
	if !isValidUUID(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid menu item ID format"})
		return
	}

	item, err := h.menuRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrMenuItemNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "menu item not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve menu item"})
		return
	}

	c.JSON(http.StatusOK, item)
}

// CreateMenuItem handles new menu item creation (ADMIN only) and invalidates menu cache.
func (h *MenuHandler) CreateMenuItem(c *gin.Context) {
	var req models.CreateMenuItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if len(name) > 255 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not exceed 255 characters"})
		return
	}

	if req.Price == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "price is required"})
		return
	}
	if *req.Price < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "price must be greater than or equal to 0"})
		return
	}

	imageURL := ""
	if req.ImageURL != nil {
		imageURL = strings.TrimSpace(*req.ImageURL)
	}

	isAvailable := true
	if req.IsAvailable != nil {
		isAvailable = *req.IsAvailable
	}

	initialQuantity := 0
	if req.InitialQuantity != nil {
		if *req.InitialQuantity < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "initial quantity must not be negative"})
			return
		}
		initialQuantity = *req.InitialQuantity
	}

	item, err := h.menuRepo.Create(c.Request.Context(), name, req.Description, *req.Price, imageURL, isAvailable, initialQuantity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create menu item"})
		return
	}

	// Invalidate menu cache after successful database creation
	h.invalidateCache(c.Request.Context())

	c.JSON(http.StatusCreated, item)
}

// UpdateMenuItem handles updates to existing menu items (ADMIN only) and invalidates menu cache.
func (h *MenuHandler) UpdateMenuItem(c *gin.Context) {
	id := c.Param("id")
	if !isValidUUID(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid menu item ID format"})
		return
	}

	var req models.UpdateMenuItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	if req.Name != nil {
		trimmedName := strings.TrimSpace(*req.Name)
		if trimmedName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name cannot be empty"})
			return
		}
		if len(trimmedName) > 255 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name must not exceed 255 characters"})
			return
		}
		req.Name = &trimmedName
	}

	if req.Price != nil && *req.Price < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "price must be greater than or equal to 0"})
		return
	}

	if req.ImageURL != nil {
		trimmedURL := strings.TrimSpace(*req.ImageURL)
		req.ImageURL = &trimmedURL
	}

	item, err := h.menuRepo.Update(c.Request.Context(), id, req.Name, req.Description, req.Price, req.ImageURL, req.IsAvailable)
	if err != nil {
		if errors.Is(err, repository.ErrMenuItemNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "menu item not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update menu item"})
		return
	}

	// Invalidate menu cache after successful database update
	h.invalidateCache(c.Request.Context())

	c.JSON(http.StatusOK, item)
}

// DeleteMenuItem removes a menu item (ADMIN only) and invalidates menu cache.
func (h *MenuHandler) DeleteMenuItem(c *gin.Context) {
	id := c.Param("id")
	if !isValidUUID(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid menu item ID format"})
		return
	}

	err := h.menuRepo.Delete(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrMenuItemNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "menu item not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete menu item"})
		return
	}

	// Invalidate menu cache after successful database deletion
	h.invalidateCache(c.Request.Context())

	c.JSON(http.StatusOK, gin.H{
		"message": "menu item deleted successfully",
	})
}

func (h *MenuHandler) invalidateCache(ctx context.Context) {
	if h.cacheService != nil {
		if err := h.cacheService.Del(ctx, cache.KeyMenuAvailable); err != nil {
			log.Printf("[MenuCache] Warning: failed to invalidate cache key '%s': %v", cache.KeyMenuAvailable, err)
		}
	}
}
