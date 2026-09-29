package handlers

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"

	"campusbite/internal/auth"
	"campusbite/internal/middleware"
	"campusbite/internal/models"
	"campusbite/internal/repository"

	"github.com/gin-gonic/gin"
)

// AuthHandler handles authentication and user identity requests.
type AuthHandler struct {
	userRepo     *repository.UserRepository
	tokenService *auth.TokenService
}

// NewAuthHandler creates a new AuthHandler instance.
func NewAuthHandler(userRepo *repository.UserRepository, tokenService *auth.TokenService) *AuthHandler {
	return &AuthHandler{
		userRepo:     userRepo,
		tokenService: tokenService,
	}
}

func isValidEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}
	// Check that the address matches the exact email and contains a domain dot
	return addr.Address == email && strings.Contains(email, "@") && strings.Contains(email, ".")
}

// Register creates a new student user account.
func (h *AuthHandler) Register(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	name := strings.TrimSpace(req.Name)
	if len(name) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if len(name) > 255 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not exceed 255 characters"})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !isValidEmail(email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid email address is required"})
		return
	}

	if err := auth.ValidatePassword(req.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process password"})
		return
	}

	user, err := h.userRepo.Create(c.Request.Context(), name, email, passwordHash, models.RoleStudent)
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateEmail) {
			c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user account"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "user registered successfully",
		"user":    user.ToResponse(),
	})
}

// Login authenticates a user and returns a signed JWT.
func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and password are required"})
		return
	}

	user, err := h.userRepo.GetByEmail(c.Request.Context(), email)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			// Intentionally generic error to prevent email enumeration
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "authentication failed"})
		return
	}

	if !auth.CheckPassword(req.Password, user.PasswordHash) {
		// Intentionally generic error to prevent password brute-force insight
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	token, err := h.tokenService.GenerateToken(user.ID, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate authentication token"})
		return
	}

	c.JSON(http.StatusOK, models.AuthResponse{
		Token: token,
		User:  user.ToResponse(),
	})
}

// Me returns the identity profile of the currently authenticated user.
func (h *AuthHandler) Me(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	user, err := h.userRepo.GetByID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve user profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": user.ToResponse(),
	})
}

// AdminTest is a protected verification endpoint requiring the ADMIN role.
func (h *AuthHandler) AdminTest(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "admin access granted",
	})
}
