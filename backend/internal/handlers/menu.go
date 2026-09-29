package handlers

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"campusbite/internal/models"
	"campusbite/internal/repository"

	"github.com/gin-gonic/gin"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func isValidUUID(id string) bool {
	return uuidRegex.MatchString(id)
}

// MenuHandler handles HTTP requests for menu catalog items.
type MenuHandler struct {
	menuRepo *repository.MenuRepository
}

// NewMenuHandler creates a new MenuHandler instance.
func NewMenuHandler(menuRepo *repository.MenuRepository) *MenuHandler {
	return &MenuHandler{menuRepo: menuRepo}
}

// GetMenu returns all menu items in the catalog.
func (h *MenuHandler) GetMenu(c *gin.Context) {
	items, err := h.menuRepo.GetAll(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve menu items"})
		return
	}

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

// CreateMenuItem handles new menu item creation (ADMIN only).
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

	item, err := h.menuRepo.Create(c.Request.Context(), name, req.Description, *req.Price, isAvailable, initialQuantity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create menu item"})
		return
	}

	c.JSON(http.StatusCreated, item)
}

// UpdateMenuItem handles updates to existing menu items (ADMIN only).
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

	item, err := h.menuRepo.Update(c.Request.Context(), id, req.Name, req.Description, req.Price, req.IsAvailable)
	if err != nil {
		if errors.Is(err, repository.ErrMenuItemNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "menu item not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update menu item"})
		return
	}

	c.JSON(http.StatusOK, item)
}

// DeleteMenuItem removes a menu item or soft-disables it if order history exists (ADMIN only).
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

	c.JSON(http.StatusOK, gin.H{
		"message": "menu item deleted successfully",
	})
}
