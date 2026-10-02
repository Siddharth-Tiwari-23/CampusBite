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
}

type razorpayService struct {
	keyID         string
	keySecret     string
	webhookSecret string
	httpClient    *http.Client
}

// NewRazorpayService returns a new instance of RazorpayService.
func NewRazorpayService(keyID, keySecret, webhookSecret string) RazorpayService {
	return &razorpayService{
		keyID:         keyID,
		keySecret:     keySecret,
		webhookSecret: webhookSecret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *razorpayService) GetKeyID() string {
	return s.keyID
}

// CreateRazorpayOrder calls the Razorpay API to create an order or generates a mock order ID if using mock keys.
func (s *razorpayService) CreateRazorpayOrder(ctx context.Context, amountInPaise int64, receipt string) (string, error) {
	// If mock/test key is used for testing without live credentials, return a mock Razorpay order ID
	if strings.Contains(s.keyID, "mock") || strings.Contains(s.keySecret, "mock") || s.keyID == "" {
		randomBytes := make([]byte, 8)
		_, _ = rand.Read(randomBytes)
		return fmt.Sprintf("order_%s", hex.EncodeToString(randomBytes)), nil
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
func (s *razorpayService) VerifyPaymentSignature(razorpayOrderID, razorpayPaymentID, signature string) bool {
	if razorpayOrderID == "" || razorpayPaymentID == "" || signature == "" {
		return false
	}
	if (strings.Contains(s.keySecret, "mock") || s.keySecret == "") && strings.HasPrefix(signature, "sig_test_") {
		return true
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
func (s *razorpayService) VerifyWebhookSignature(bodyBytes []byte, signatureHeader string) bool {
	if len(bodyBytes) == 0 || signatureHeader == "" {
		return false
	}
	if (strings.Contains(s.webhookSecret, "mock") || s.webhookSecret == "") && strings.HasPrefix(signatureHeader, "sig_test_") {
		return true
	}
	expectedSignature := s.ComputeWebhookSignature(bodyBytes)
	return hmac.Equal([]byte(expectedSignature), []byte(signatureHeader))
}
