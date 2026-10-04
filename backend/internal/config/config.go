package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv                     string // "development", "test", "production"
	DatabaseURL                string
	RedisURL                   string
	JWTSecret                  string
	Port                       string
	RazorpayKeyID              string
	RazorpayKeySecret          string
	RazorpayWebhookSecret      string
	MenuCacheTTL               time.Duration
	ReservationWorkerInterval  time.Duration
	IdempotencyCleanupInterval time.Duration
	GeminiAPIKey               string
	GeminiModel                string
	AdminEmail                 string
	AdminPassword              string
}

// IsProduction returns true if the application environment is configured for production.
func (c *Config) IsProduction() bool {
	if c == nil {
		return false
	}
	env := strings.ToLower(strings.TrimSpace(c.AppEnv))
	return env == "production" || env == "prod"
}

// Load loads configuration from environment variables and optional .env files.
func Load() (*Config, error) {
	return loadConfig(false)
}

// LoadFromEnv loads configuration strictly from current process environment variables without reading .env files.
func LoadFromEnv() (*Config, error) {
	return loadConfig(true)
}

func loadConfig(skipDotenv bool) (*Config, error) {
	if !skipDotenv {
		// Attempt to load from current working directory or repo root (.env)
		_ = godotenv.Load()
		_ = godotenv.Load("../.env")
		_ = godotenv.Load("../../.env")
		_ = godotenv.Load("../../../.env")
	}

	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = os.Getenv("ENV")
	}
	if appEnv == "" && os.Getenv("GIN_MODE") == "release" {
		appEnv = "production"
	}
	if appEnv == "" {
		appEnv = "development"
	}
	appEnv = strings.ToLower(strings.TrimSpace(appEnv))
	isProduction := appEnv == "production" || appEnv == "prod"

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is required")
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET environment variable is required")
	}

	razorpayKeyID := os.Getenv("RAZORPAY_KEY_ID")
	razorpayKeySecret := os.Getenv("RAZORPAY_KEY_SECRET")
	razorpayWebhookSecret := os.Getenv("RAZORPAY_WEBHOOK_SECRET")

	if isProduction {
		if razorpayKeyID == "" {
			return nil, fmt.Errorf("RAZORPAY_KEY_ID environment variable is required in production")
		}
		if strings.Contains(razorpayKeyID, "mock") {
			return nil, fmt.Errorf("mock RAZORPAY_KEY_ID is not allowed in production mode")
		}
		if razorpayKeySecret == "" {
			return nil, fmt.Errorf("RAZORPAY_KEY_SECRET environment variable is required in production")
		}
		if strings.Contains(razorpayKeySecret, "mock") {
			return nil, fmt.Errorf("mock RAZORPAY_KEY_SECRET is not allowed in production mode")
		}
		if razorpayWebhookSecret == "" {
			return nil, fmt.Errorf("RAZORPAY_WEBHOOK_SECRET environment variable is required in production")
		}
		if strings.Contains(razorpayWebhookSecret, "mock") {
			return nil, fmt.Errorf("mock RAZORPAY_WEBHOOK_SECRET is not allowed in production mode")
		}
	} else {
		// In development or test mode, default to safe mock credentials if not provided
		if razorpayKeyID == "" {
			razorpayKeyID = "rzp_test_campusbite_mock_key"
		}
		if razorpayKeySecret == "" {
			razorpayKeySecret = "rzp_test_campusbite_mock_secret"
		}
		if razorpayWebhookSecret == "" {
			razorpayWebhookSecret = "rzp_test_campusbite_mock_webhook_secret"
		}
	}

	menuCacheTTL := 60 * time.Second
	if ttlStr := os.Getenv("MENU_CACHE_TTL_SECONDS"); ttlStr != "" {
		if ttlSec, err := strconv.Atoi(ttlStr); err == nil && ttlSec > 0 {
			menuCacheTTL = time.Duration(ttlSec) * time.Second
		}
	}

	reservationWorkerInterval := 30 * time.Second
	if val := os.Getenv("RESERVATION_WORKER_INTERVAL_SECONDS"); val != "" {
		if sec, err := strconv.Atoi(val); err == nil && sec > 0 {
			reservationWorkerInterval = time.Duration(sec) * time.Second
		}
	}

	idempotencyCleanupInterval := 3600 * time.Second
	if val := os.Getenv("IDEMPOTENCY_CLEANUP_INTERVAL_SECONDS"); val != "" {
		if sec, err := strconv.Atoi(val); err == nil && sec > 0 {
			idempotencyCleanupInterval = time.Duration(sec) * time.Second
		}
	}

	geminiAPIKey := os.Getenv("GEMINI_API_KEY")

	geminiModel := os.Getenv("GEMINI_MODEL")
	if geminiModel == "" {
		geminiModel = "gemini-2.5-flash"
	}

	adminEmail := os.Getenv("ADMIN_EMAIL")
	if adminEmail == "" {
		adminEmail = "admin@campusbite.com"
	}

	adminPassword := os.Getenv("ADMIN_PASSWORD")

	return &Config{
		AppEnv:                     appEnv,
		DatabaseURL:                databaseURL,
		RedisURL:                   redisURL,
		JWTSecret:                  jwtSecret,
		Port:                       port,
		RazorpayKeyID:              razorpayKeyID,
		RazorpayKeySecret:          razorpayKeySecret,
		RazorpayWebhookSecret:      razorpayWebhookSecret,
		MenuCacheTTL:               menuCacheTTL,
		ReservationWorkerInterval:  reservationWorkerInterval,
		IdempotencyCleanupInterval: idempotencyCleanupInterval,
		GeminiAPIKey:               geminiAPIKey,
		GeminiModel:                geminiModel,
		AdminEmail:                 adminEmail,
		AdminPassword:              adminPassword,
	}, nil
}
