package service_test

import (
	"context"
	"strings"
	"testing"

	"campusbite/internal/service"
)

func TestRazorpayService_ProductionHardening(t *testing.T) {
	keyID := "rzp_live_realKey123"
	keySecret := "live_realSecret456"
	webhookSecret := "live_realWebhook789"

	// Production instance (isProduction = true)
	prodService := service.NewRazorpayService(keyID, keySecret, webhookSecret, true)

	t.Run("Production instance reports IsProduction true", func(t *testing.T) {
		if !prodService.IsProduction() {
			t.Errorf("expected IsProduction() to be true")
		}
	})

	t.Run("Production rejects mock payment signature prefix sig_test_", func(t *testing.T) {
		orderID := "order_test123"
		paymentID := "pay_test123"
		mockSig := "sig_test_mock_signature_value"

		if prodService.VerifyPaymentSignature(orderID, paymentID, mockSig) {
			t.Errorf("expected production service to REJECT mock signature prefix sig_test_")
		}
	})

	t.Run("Production verifies genuine HMAC-SHA256 payment signature", func(t *testing.T) {
		orderID := "order_valid123"
		paymentID := "pay_valid123"
		validSig := prodService.ComputePaymentSignature(orderID, paymentID)

		if !prodService.VerifyPaymentSignature(orderID, paymentID, validSig) {
			t.Errorf("expected production service to ACCEPT valid HMAC-SHA256 payment signature")
		}
	})

	t.Run("Production rejects tampered payment signature", func(t *testing.T) {
		orderID := "order_valid123"
		paymentID := "pay_valid123"
		tamperedSig := "deadbeef" + prodService.ComputePaymentSignature(orderID, paymentID)[8:]

		if prodService.VerifyPaymentSignature(orderID, paymentID, tamperedSig) {
			t.Errorf("expected production service to REJECT tampered payment signature")
		}
	})

	t.Run("Production rejects mock webhook signature prefix sig_test_", func(t *testing.T) {
		body := []byte(`{"event":"payment.captured"}`)
		mockSig := "sig_test_mock_webhook_signature"

		if prodService.VerifyWebhookSignature(body, mockSig) {
			t.Errorf("expected production service to REJECT mock webhook signature prefix sig_test_")
		}
	})

	t.Run("Production verifies genuine HMAC-SHA256 webhook signature", func(t *testing.T) {
		body := []byte(`{"event":"payment.captured"}`)
		validSig := prodService.ComputeWebhookSignature(body)

		if !prodService.VerifyWebhookSignature(body, validSig) {
			t.Errorf("expected production service to ACCEPT valid HMAC-SHA256 webhook signature")
		}
	})

	t.Run("Production does not generate mock order ID for mock keys", func(t *testing.T) {
		// Even if initialized with mock-named keys in production mode
		prodWithMockKey := service.NewRazorpayService("rzp_test_mock_key", "rzp_test_mock_secret", "rzp_test_mock_webhook", true)
		
		// Attempting order creation in production mode should hit real HTTP endpoint (or fail), never return mock order_xxx
		_, err := prodWithMockKey.CreateRazorpayOrder(context.Background(), 10000, "rcpt_1")
		// Since it tries to call https://api.razorpay.com with invalid mock keys, it will either get network error or HTTP 401, never a generated mock order
		if err == nil {
			t.Errorf("expected error when creating real Razorpay order with mock credentials against live API")
		}
	})
}

func TestRazorpayService_DevelopmentMode(t *testing.T) {
	mockKeyID := "rzp_test_campusbite_mock_key"
	mockKeySecret := "rzp_test_campusbite_mock_secret"
	mockWebhookSecret := "rzp_test_campusbite_mock_webhook_secret"

	// Dev / test instance (isProduction = false)
	devService := service.NewRazorpayService(mockKeyID, mockKeySecret, mockWebhookSecret, false)

	t.Run("Development instance reports IsProduction false", func(t *testing.T) {
		if devService.IsProduction() {
			t.Errorf("expected IsProduction() to be false")
		}
	})

	t.Run("Development allows mock order creation", func(t *testing.T) {
		orderID, err := devService.CreateRazorpayOrder(context.Background(), 5000, "rcpt_dev_1")
		if err != nil {
			t.Fatalf("expected successful mock order creation in dev mode, got: %v", err)
		}
		if !strings.HasPrefix(orderID, "order_") {
			t.Errorf("expected mock order ID to start with order_, got: %s", orderID)
		}
	})

	t.Run("Development allows mock payment signature prefix sig_test_", func(t *testing.T) {
		if !devService.VerifyPaymentSignature("order_123", "pay_123", "sig_test_valid_mock_signature") {
			t.Errorf("expected development service to ACCEPT sig_test_ prefix with mock key")
		}
	})

	t.Run("Development allows mock webhook signature prefix sig_test_", func(t *testing.T) {
		if !devService.VerifyWebhookSignature([]byte(`{}`), "sig_test_webhook_signature") {
			t.Errorf("expected development service to ACCEPT sig_test_ webhook signature with mock key")
		}
	})
}
