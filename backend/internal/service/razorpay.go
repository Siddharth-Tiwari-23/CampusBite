package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RazorpayService defines the interface for interacting with Razorpay and validating signatures.
type RazorpayService interface {
	CreateRazorpayOrder(ctx context.Context, amountInPaise int64, receipt string) (string, error)
	VerifyPaymentSignature(razorpayOrderID, razorpayPaymentID, signature string) bool
	VerifyWebhookSignature(bodyBytes []byte, signatureHeader string) bool
	GetKeyID() string
	ComputePaymentSignature(razorpayOrderID, razorpayPaymentID string) string
	ComputeWebhookSignature(bodyBytes []byte) string
	IsProduction() bool
}

type razorpayService struct {
	keyID         string
	keySecret     string
	webhookSecret string
	httpClient    *http.Client
	isProduction  bool
}

// NewRazorpayService returns a new instance of RazorpayService.
// In production mode (isProduction=true), real Razorpay credentials are required and mock behavior is strictly forbidden.
func NewRazorpayService(keyID, keySecret, webhookSecret string, isProduction ...bool) RazorpayService {
	prod := false
	if len(isProduction) > 0 {
		prod = isProduction[0]
	}
	return &razorpayService{
		keyID:         keyID,
		keySecret:     keySecret,
		webhookSecret: webhookSecret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		isProduction: prod,
	}
}

func (s *razorpayService) IsProduction() bool {
	return s.isProduction
}

func (s *razorpayService) GetKeyID() string {
	return s.keyID
}

// CreateRazorpayOrder calls the Razorpay API to create an order.
// In test/development mode with mock credentials, it generates a mock order ID.
// In production mode, mock order generation is strictly forbidden.
func (s *razorpayService) CreateRazorpayOrder(ctx context.Context, amountInPaise int64, receipt string) (string, error) {
	// If in development/test mode AND using mock keys, return a mock Razorpay order ID.
	// In production mode (s.isProduction == true), mock behavior is strictly forbidden.
	if !s.isProduction && (strings.Contains(s.keyID, "mock") || strings.Contains(s.keySecret, "mock") || s.keyID == "") {
		randomBytes := make([]byte, 8)
		_, _ = rand.Read(randomBytes)
		return fmt.Sprintf("order_%s", hex.EncodeToString(randomBytes)), nil
	}

	if s.keyID == "" || s.keySecret == "" {
		return "", fmt.Errorf("razorpay credentials not configured")
	}

	payload := map[string]interface{}{
		"amount":   amountInPaise,
		"currency": "INR",
		"receipt":  receipt,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal razorpay order request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.razorpay.com/v1/orders", bytes.NewReader(jsonBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create razorpay http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(s.keyID, s.keySecret)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("razorpay api request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read razorpay api response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("razorpay api returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", fmt.Errorf("failed to parse razorpay order response: %w", err)
	}

	if result.ID == "" {
		return "", fmt.Errorf("empty order id received from razorpay: %s", string(bodyBytes))
	}

	return result.ID, nil
}

// ComputePaymentSignature calculates HMAC-SHA256 signature for razorpay payment verification.
func (s *razorpayService) ComputePaymentSignature(razorpayOrderID, razorpayPaymentID string) string {
	message := razorpayOrderID + "|" + razorpayPaymentID
	mac := hmac.New(sha256.New, []byte(s.keySecret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyPaymentSignature verifies HMAC-SHA256(razorpay_order_id + "|" + razorpay_payment_id, keySecret).
// In production mode, mock signatures like "sig_test_" are strictly rejected.
func (s *razorpayService) VerifyPaymentSignature(razorpayOrderID, razorpayPaymentID, signature string) bool {
	if razorpayOrderID == "" || razorpayPaymentID == "" || signature == "" {
		return false
	}
	// In test/development mode with mock credentials, allow test signatures
	if !s.isProduction && (strings.Contains(s.keySecret, "mock") || s.keySecret == "") && strings.HasPrefix(signature, "sig_test_") {
		return true
	}
	if s.keySecret == "" {
		return false
	}
	expectedSignature := s.ComputePaymentSignature(razorpayOrderID, razorpayPaymentID)
	return hmac.Equal([]byte(expectedSignature), []byte(signature))
}

// ComputeWebhookSignature calculates HMAC-SHA256 signature for razorpay webhook verification.
func (s *razorpayService) ComputeWebhookSignature(bodyBytes []byte) string {
	mac := hmac.New(sha256.New, []byte(s.webhookSecret))
	mac.Write(bodyBytes)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyWebhookSignature verifies HMAC-SHA256(rawBody, webhookSecret).
// In production mode, mock signatures like "sig_test_" are strictly rejected.
func (s *razorpayService) VerifyWebhookSignature(bodyBytes []byte, signatureHeader string) bool {
	if len(bodyBytes) == 0 || signatureHeader == "" {
		return false
	}
	// In test/development mode with mock credentials, allow test signatures
	if !s.isProduction && (strings.Contains(s.webhookSecret, "mock") || s.webhookSecret == "") && strings.HasPrefix(signatureHeader, "sig_test_") {
		return true
	}
	if s.webhookSecret == "" {
		return false
	}
	expectedSignature := s.ComputeWebhookSignature(bodyBytes)
	return hmac.Equal([]byte(expectedSignature), []byte(signatureHeader))
}
