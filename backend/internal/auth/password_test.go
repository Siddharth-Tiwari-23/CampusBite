package auth_test

import (
	"strings"
	"testing"

	"campusbite/internal/auth"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name        string
		password    string
		expectError bool
	}{
		{"valid password", "SecretPass123!", false},
		{"minimum length password", "12345678", false},
		{"empty password", "", true},
		{"too short password", "short", true},
		{"too long password", strings.Repeat("a", 73), true},
		{"maximum length password", strings.Repeat("a", 72), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := auth.ValidatePassword(tt.password)
			if (err != nil) != tt.expectError {
				t.Errorf("ValidatePassword(%q) error = %v, expectError = %v", tt.password, err, tt.expectError)
			}
		})
	}
}

func TestHashAndCheckPassword(t *testing.T) {
	password := "SecurePassword#2026"

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if hash == password {
		t.Fatal("HashPassword returned plaintext password")
	}

	if !auth.CheckPassword(password, hash) {
		t.Fatal("CheckPassword returned false for valid password")
	}

	if auth.CheckPassword("WrongPassword#2026", hash) {
		t.Fatal("CheckPassword returned true for incorrect password")
	}
}
