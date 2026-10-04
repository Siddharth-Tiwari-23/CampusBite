package config_test

import (
	"os"
	"strings"
	"testing"

	"campusbite/internal/config"
)

func TestConfig_ProductionValidation(t *testing.T) {
	// Base required variables
	baseEnv := map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost:5432/db",
		"JWT_SECRET":   "very-secure-production-jwt-secret-at-least-32-chars-long",
	}

	setEnv := func(envMap map[string]string) {
		os.Clearenv()
		for k, v := range envMap {
			os.Setenv(k, v)
		}
	}

	t.Run("Production with missing RAZORPAY_KEY_ID fails fast", func(t *testing.T) {
		env := map[string]string{
			"APP_ENV":                "production",
			"DATABASE_URL":           baseEnv["DATABASE_URL"],
			"JWT_SECRET":             baseEnv["JWT_SECRET"],
			"RAZORPAY_KEY_SECRET":    "live_secret_12345",
			"RAZORPAY_WEBHOOK_SECRET": "live_webhook_secret_12345",
		}
		setEnv(env)

		cfg, err := config.LoadFromEnv()
		if err == nil || cfg != nil {
			t.Fatalf("expected error for missing RAZORPAY_KEY_ID in production, got cfg: %+v", cfg)
		}
		if !strings.Contains(err.Error(), "RAZORPAY_KEY_ID") {
			t.Errorf("expected error message to mention RAZORPAY_KEY_ID, got: %v", err)
		}
	})

	t.Run("Production with missing RAZORPAY_KEY_SECRET fails fast", func(t *testing.T) {
		env := map[string]string{
			"APP_ENV":                "production",
			"DATABASE_URL":           baseEnv["DATABASE_URL"],
			"JWT_SECRET":             baseEnv["JWT_SECRET"],
			"RAZORPAY_KEY_ID":        "rzp_live_12345",
			"RAZORPAY_WEBHOOK_SECRET": "live_webhook_secret_12345",
		}
		setEnv(env)

		cfg, err := config.LoadFromEnv()
		if err == nil || cfg != nil {
			t.Fatalf("expected error for missing RAZORPAY_KEY_SECRET in production, got cfg: %+v", cfg)
		}
		if !strings.Contains(err.Error(), "RAZORPAY_KEY_SECRET") {
			t.Errorf("expected error message to mention RAZORPAY_KEY_SECRET, got: %v", err)
		}
	})

	t.Run("Production with missing RAZORPAY_WEBHOOK_SECRET fails fast", func(t *testing.T) {
		env := map[string]string{
			"APP_ENV":             "production",
			"DATABASE_URL":        baseEnv["DATABASE_URL"],
			"JWT_SECRET":          baseEnv["JWT_SECRET"],
			"RAZORPAY_KEY_ID":     "rzp_live_12345",
			"RAZORPAY_KEY_SECRET": "live_secret_12345",
		}
		setEnv(env)

		cfg, err := config.LoadFromEnv()
		if err == nil || cfg != nil {
			t.Fatalf("expected error for missing RAZORPAY_WEBHOOK_SECRET in production, got cfg: %+v", cfg)
		}
		if !strings.Contains(err.Error(), "RAZORPAY_WEBHOOK_SECRET") {
			t.Errorf("expected error message to mention RAZORPAY_WEBHOOK_SECRET, got: %v", err)
		}
	})

	t.Run("Production with mock credentials fails fast", func(t *testing.T) {
		env := map[string]string{
			"APP_ENV":                "production",
			"DATABASE_URL":           baseEnv["DATABASE_URL"],
			"JWT_SECRET":             baseEnv["JWT_SECRET"],
			"RAZORPAY_KEY_ID":        "rzp_test_campusbite_mock_key",
			"RAZORPAY_KEY_SECRET":    "rzp_test_campusbite_mock_secret",
			"RAZORPAY_WEBHOOK_SECRET": "rzp_test_campusbite_mock_webhook_secret",
		}
		setEnv(env)

		cfg, err := config.LoadFromEnv()
		if err == nil || cfg != nil {
			t.Fatalf("expected error for mock credentials in production, got cfg: %+v", cfg)
		}
		if !strings.Contains(err.Error(), "mock") {
			t.Errorf("expected error message to mention mock credentials, got: %v", err)
		}
	})

	t.Run("Production with valid real credentials succeeds", func(t *testing.T) {
		env := map[string]string{
			"APP_ENV":                "production",
			"DATABASE_URL":           baseEnv["DATABASE_URL"],
			"JWT_SECRET":             baseEnv["JWT_SECRET"],
			"RAZORPAY_KEY_ID":        "rzp_live_validkeyid123",
			"RAZORPAY_KEY_SECRET":    "live_validsecretkey456",
			"RAZORPAY_WEBHOOK_SECRET": "live_validwebhooksecret789",
		}
		setEnv(env)

		cfg, err := config.LoadFromEnv()
		if err != nil || cfg == nil {
			t.Fatalf("expected successful config load in production, got err: %v", err)
		}
		if !cfg.IsProduction() {
			t.Errorf("expected cfg.IsProduction() to be true")
		}
		if cfg.RazorpayKeyID != "rzp_live_validkeyid123" {
			t.Errorf("expected RazorpayKeyID rzp_live_validkeyid123, got %s", cfg.RazorpayKeyID)
		}
	})

	t.Run("Development mode falls back to mock credentials without error", func(t *testing.T) {
		env := map[string]string{
			"APP_ENV":      "development",
			"DATABASE_URL": baseEnv["DATABASE_URL"],
			"JWT_SECRET":   baseEnv["JWT_SECRET"],
		}
		setEnv(env)

		cfg, err := config.LoadFromEnv()
		if err != nil || cfg == nil {
			t.Fatalf("expected successful config load in development, got err: %v", err)
		}
		if cfg.IsProduction() {
			t.Errorf("expected cfg.IsProduction() to be false in development")
		}
		if !strings.Contains(cfg.RazorpayKeyID, "mock") {
			t.Errorf("expected default mock key in development, got %s", cfg.RazorpayKeyID)
		}
	})
}
