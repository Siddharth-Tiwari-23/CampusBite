package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campusbite/internal/auth"
	"campusbite/internal/middleware"
	"campusbite/internal/models"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupTestRouter(tokenService *auth.TokenService) *gin.Engine {
	r := gin.New()
	authMiddleware := middleware.AuthMiddleware(tokenService)

	// Protected endpoint returning user context
	r.GET("/protected", authMiddleware, func(c *gin.Context) {
		userID, _ := middleware.GetUserID(c)
		userRole, _ := middleware.GetUserRole(c)
		c.JSON(http.StatusOK, gin.H{"userID": userID, "userRole": userRole})
	})

	// Admin-only endpoint
	r.GET("/admin-only", authMiddleware, middleware.RequireRole(models.RoleAdmin), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "admin granted"})
	})

	return r
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	tokenService, _ := auth.NewTokenService("test-secret-1234567890123456", 1*time.Hour)
	router := setupTestRouter(tokenService)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", w.Code)
	}
}

func TestAuthMiddleware_MalformedHeader(t *testing.T) {
	tokenService, _ := auth.NewTokenService("test-secret-1234567890123456", 1*time.Hour)
	router := setupTestRouter(tokenService)

	tests := []string{
		"Basic dXNlcjpwYXNz",
		"Bearer",
		"Token some-random-token",
		"bearer ",
	}

	for _, header := range tests {
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", header)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("header %q expected 401, got %d", header, w.Code)
		}
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	tokenService, _ := auth.NewTokenService("test-secret-1234567890123456", 1*time.Hour)
	router := setupTestRouter(tokenService)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid.jwt.token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", w.Code)
	}
}

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	shortService, _ := auth.NewTokenService("test-secret-1234567890123456", 10*time.Millisecond)
	router := setupTestRouter(shortService)

	token, _ := shortService.GenerateToken("user-1", "STUDENT")
	time.Sleep(20 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for expired token, got %d", w.Code)
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	tokenService, _ := auth.NewTokenService("test-secret-1234567890123456", 1*time.Hour)
	router := setupTestRouter(tokenService)

	token, _ := tokenService.GenerateToken("user-42", "STUDENT")

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", w.Code)
	}
}

func TestRequireRole_StudentAccessingAdminEndpoint(t *testing.T) {
	tokenService, _ := auth.NewTokenService("test-secret-1234567890123456", 1*time.Hour)
	router := setupTestRouter(tokenService)

	studentToken, _ := tokenService.GenerateToken("student-1", models.RoleStudent)

	req := httptest.NewRequest(http.MethodGet, "/admin-only", nil)
	req.Header.Set("Authorization", "Bearer "+studentToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for STUDENT accessing ADMIN endpoint, got %d", w.Code)
	}
}

func TestRequireRole_AdminAccessingAdminEndpoint(t *testing.T) {
	tokenService, _ := auth.NewTokenService("test-secret-1234567890123456", 1*time.Hour)
	router := setupTestRouter(tokenService)

	adminToken, _ := tokenService.GenerateToken("admin-1", models.RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/admin-only", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for ADMIN accessing ADMIN endpoint, got %d", w.Code)
	}
}
