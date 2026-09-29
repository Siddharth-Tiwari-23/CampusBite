package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campusbite/internal/auth"
	"campusbite/internal/config"
	"campusbite/internal/database"
	"campusbite/internal/models"
	"campusbite/internal/repository"
	"campusbite/internal/routes"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupIntegrationApp(t *testing.T) (*gin.Engine, *database.DB, *auth.TokenService) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("skipping integration test: config error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	db, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Skipf("skipping integration test: database unreachable: %v", err)
	}

	tokenService, err := auth.NewTokenService(cfg.JWTSecret, 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to create token service: %v", err)
	}

	router := routes.SetupRouter(db, tokenService)
	return router, db, tokenService
}

func TestAuth_Register(t *testing.T) {
	router, db, _ := setupIntegrationApp(t)
	defer db.Close()

	uniqueEmail := fmt.Sprintf("test_student_%d@campusbite.internal", time.Now().UnixNano())

	// 1. Successful Registration
	registerPayload := models.RegisterRequest{
		Name:     "Test Student",
		Email:    uniqueEmail,
		Password: "SecurePassword123!",
	}
	body, _ := json.Marshal(registerPayload)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	userMap, ok := resp["user"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'user' object in response, got %v", resp)
	}

	if _, hasHash := userMap["password_hash"]; hasHash {
		t.Fatal("SECURITY VIOLATION: password_hash leaked in registration response")
	}
	if userMap["email"] != uniqueEmail {
		t.Errorf("expected email %s, got %v", uniqueEmail, userMap["email"])
	}
	if userMap["role"] != models.RoleStudent {
		t.Errorf("expected role STUDENT, got %v", userMap["role"])
	}

	// 2. Duplicate Registration Attempt
	dupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBuffer(body))
	dupReq.Header.Set("Content-Type", "application/json")
	dupW := httptest.NewRecorder()
	router.ServeHTTP(dupW, dupReq)

	if dupW.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict for duplicate email, got %d", dupW.Code)
	}

	// 3. Invalid Inputs
	invalidTests := []struct {
		name    string
		payload models.RegisterRequest
	}{
		{"empty name", models.RegisterRequest{Name: "", Email: "valid@campusbite.internal", Password: "Password123"}},
		{"invalid email", models.RegisterRequest{Name: "Name", Email: "not-an-email", Password: "Password123"}},
		{"short password", models.RegisterRequest{Name: "Name", Email: "valid2@campusbite.internal", Password: "short"}},
	}

	for _, tc := range invalidTests {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(tc.payload)
			badReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBuffer(b))
			badReq.Header.Set("Content-Type", "application/json")
			badW := httptest.NewRecorder()
			router.ServeHTTP(badW, badReq)

			if badW.Code != http.StatusBadRequest {
				t.Errorf("expected 400 Bad Request for %s, got %d", tc.name, badW.Code)
			}
		})
	}
}

func TestAuth_Login(t *testing.T) {
	router, db, _ := setupIntegrationApp(t)
	defer db.Close()

	email := fmt.Sprintf("login_student_%d@campusbite.internal", time.Now().UnixNano())
	password := "LoginSecret#2026"

	// Register user first
	regBody, _ := json.Marshal(models.RegisterRequest{
		Name:     "Login Tester",
		Email:    email,
		Password: password,
	})
	regReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBuffer(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regW := httptest.NewRecorder()
	router.ServeHTTP(regW, regReq)
	if regW.Code != http.StatusCreated {
		t.Fatalf("failed to setup test user: %s", regW.Body.String())
	}

	// 1. Successful Login
	loginBody, _ := json.Marshal(models.LoginRequest{
		Email:    email,
		Password: password,
	})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBuffer(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)

	if loginW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid login, got %d: %s", loginW.Code, loginW.Body.String())
	}

	var authResp models.AuthResponse
	if err := json.Unmarshal(loginW.Body.Bytes(), &authResp); err != nil {
		t.Fatalf("failed to unmarshal login response: %v", err)
	}

	if authResp.Token == "" {
		t.Fatal("expected non-empty JWT token in login response")
	}
	if authResp.User.Email != email {
		t.Errorf("expected user email %s, got %s", email, authResp.User.Email)
	}

	// 2. Incorrect Password
	badPassBody, _ := json.Marshal(models.LoginRequest{
		Email:    email,
		Password: "IncorrectPassword123",
	})
	badPassReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBuffer(badPassBody))
	badPassReq.Header.Set("Content-Type", "application/json")
	badPassW := httptest.NewRecorder()
	router.ServeHTTP(badPassW, badPassReq)

	if badPassW.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for incorrect password, got %d", badPassW.Code)
	}

	// 3. Non-existent User
	nonExistBody, _ := json.Marshal(models.LoginRequest{
		Email:    "nonexistent@campusbite.internal",
		Password: "Password123!",
	})
	nonExistReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBuffer(nonExistBody))
	nonExistReq.Header.Set("Content-Type", "application/json")
	nonExistW := httptest.NewRecorder()
	router.ServeHTTP(nonExistW, nonExistReq)

	if nonExistW.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for non-existent user, got %d", nonExistW.Code)
	}
}

func TestAuth_MeEndpoint(t *testing.T) {
	router, db, _ := setupIntegrationApp(t)
	defer db.Close()

	email := fmt.Sprintf("me_student_%d@campusbite.internal", time.Now().UnixNano())
	password := "MeSecret#2026"

	// Register and login to get token
	regBody, _ := json.Marshal(models.RegisterRequest{
		Name:     "Profile Student",
		Email:    email,
		Password: password,
	})
	regReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBuffer(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regW := httptest.NewRecorder()
	router.ServeHTTP(regW, regReq)

	loginBody, _ := json.Marshal(models.LoginRequest{
		Email:    email,
		Password: password,
	})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBuffer(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)

	var authResp models.AuthResponse
	_ = json.Unmarshal(loginW.Body.Bytes(), &authResp)

	// 1. Authorized /me
	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+authResp.Token)
	meW := httptest.NewRecorder()
	router.ServeHTTP(meW, meReq)

	if meW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /me, got %d: %s", meW.Code, meW.Body.String())
	}

	var meResp map[string]models.UserResponse
	if err := json.Unmarshal(meW.Body.Bytes(), &meResp); err != nil {
		t.Fatalf("failed to decode /me response: %v", err)
	}
	if meResp["user"].Email != email {
		t.Errorf("expected email %s, got %s", email, meResp["user"].Email)
	}

	// 2. Unauthorized /me without header
	unauthReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	unauthW := httptest.NewRecorder()
	router.ServeHTTP(unauthW, unauthReq)

	if unauthW.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for /me without token, got %d", unauthW.Code)
	}
}

func TestAuth_RoleAuthorization(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	// 1. Create ADMIN user directly in DB
	userRepo := repository.NewUserRepository(db)
	adminEmail := fmt.Sprintf("admin_%d@campusbite.internal", time.Now().UnixNano())
	hash, _ := auth.HashPassword("AdminSecret#2026")
	adminUser, err := userRepo.Create(context.Background(), "Admin Officer", adminEmail, hash, models.RoleAdmin)
	if err != nil {
		t.Fatalf("failed to create admin user: %v", err)
	}

	adminToken, err := tokenService.GenerateToken(adminUser.ID, adminUser.Role)
	if err != nil {
		t.Fatalf("failed to generate admin token: %v", err)
	}

	// 2. Create STUDENT user
	studentEmail := fmt.Sprintf("student_rbac_%d@campusbite.internal", time.Now().UnixNano())
	studentUser, err := userRepo.Create(context.Background(), "Student Learner", studentEmail, hash, models.RoleStudent)
	if err != nil {
		t.Fatalf("failed to create student user: %v", err)
	}

	studentToken, err := tokenService.GenerateToken(studentUser.ID, studentUser.Role)
	if err != nil {
		t.Fatalf("failed to generate student token: %v", err)
	}

	// 3. STUDENT accessing /api/v1/auth/admin-test -> must return 403
	studentReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/admin-test", nil)
	studentReq.Header.Set("Authorization", "Bearer "+studentToken)
	studentW := httptest.NewRecorder()
	router.ServeHTTP(studentW, studentReq)

	if studentW.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for STUDENT accessing admin-test, got %d", studentW.Code)
	}

	// 4. ADMIN accessing /api/v1/auth/admin-test -> must return 200
	adminReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/admin-test", nil)
	adminReq.Header.Set("Authorization", "Bearer "+adminToken)
	adminW := httptest.NewRecorder()
	router.ServeHTTP(adminW, adminReq)

	if adminW.Code != http.StatusOK {
		t.Errorf("expected 200 OK for ADMIN accessing admin-test, got %d: %s", adminW.Code, adminW.Body.String())
	}
}
