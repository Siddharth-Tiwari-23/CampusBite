package auth_test

import (
	"errors"
	"testing"
	"time"

	"campusbite/internal/auth"
)

func TestTokenService_GenerateAndValidateToken(t *testing.T) {
	secret := "test-secret-key-1234567890123456"
	service, err := auth.NewTokenService(secret, 1*time.Hour)
	if err != nil {
		t.Fatalf("NewTokenService failed: %v", err)
	}

	userID := "550e8400-e29b-41d4-a716-446655440000"
	role := "STUDENT"

	tokenString, err := service.GenerateToken(userID, role)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	if tokenString == "" {
		t.Fatal("expected non-empty token string")
	}

	claims, err := service.ValidateToken(tokenString)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("expected UserID %s, got %s", userID, claims.UserID)
	}
	if claims.Role != role {
		t.Errorf("expected Role %s, got %s", role, claims.Role)
	}
}

func TestTokenService_ExpiredToken(t *testing.T) {
	secret := "test-secret-key-1234567890123456"

	shortService, err := auth.NewTokenService(secret, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewTokenService failed: %v", err)
	}

	tokenString, err := shortService.GenerateToken("user-123", "STUDENT")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// Sleep until expired
	time.Sleep(30 * time.Millisecond)

	_, err = shortService.ValidateToken(tokenString)
	if !errors.Is(err, auth.ErrExpiredToken) {
		t.Errorf("expected ErrExpiredToken, got %v", err)
	}
}

func TestTokenService_TamperedToken(t *testing.T) {
	secret := "test-secret-key-1234567890123456"
	service, err := auth.NewTokenService(secret, 1*time.Hour)
	if err != nil {
		t.Fatalf("NewTokenService failed: %v", err)
	}

	tokenString, err := service.GenerateToken("user-123", "STUDENT")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// Tamper with the token string
	tampered := tokenString + "tampered"
	_, err = service.ValidateToken(tampered)
	if !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken for tampered token, got %v", err)
	}
}

func TestTokenService_WrongSecret(t *testing.T) {
	service1, _ := auth.NewTokenService("secret-one-1234567890123456", 1*time.Hour)
	service2, _ := auth.NewTokenService("secret-two-1234567890123456", 1*time.Hour)

	tokenString, err := service1.GenerateToken("user-123", "ADMIN")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	_, err = service2.ValidateToken(tokenString)
	if !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken when validated with wrong secret, got %v", err)
	}
}
