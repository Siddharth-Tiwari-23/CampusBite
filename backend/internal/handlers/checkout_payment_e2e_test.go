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

	"campusbite/internal/cache"
	"campusbite/internal/models"
	"campusbite/internal/repository"
	"campusbite/internal/routes"
	"campusbite/internal/service"
	"campusbite/internal/worker"
	"campusbite/internal/ws"
)

func TestCheckoutAndPayment_ComprehensiveE2E(t *testing.T) {
	_, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	rzpService := service.NewRazorpayService("rzp_test_mock_key", "rzp_test_mock_secret", "rzp_test_mock_webhook_secret")
	wsHub := ws.NewHub()

	appRouter := routes.SetupRouter(db, tokenService, rzpService, cache.NewNoOpCache(), wsHub)

	menuRepo := repository.NewMenuRepository(db)
	invRepo := repository.NewInventoryRepository(db)

	// 1. Setup test users
	userA, tokenA := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_, tokenB := createRealTestUser(t, db, tokenService, models.RoleStudent)

	// 2. Setup isolated menu items with known stock
	initialStock := 20
	itemPrice := 100.00
	itemA, err := menuRepo.Create(context.Background(), fmt.Sprintf("E2E_Burger_%d", time.Now().UnixNano()), "Delicious burger", itemPrice, "", true, initialStock)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), "UPDATE menu_items SET is_available = false WHERE id = $1", itemA.ID)
	})

	// -------------------------------------------------------------------------
	// SCENARIO 1: Happy Path Checkout -> Payment Order -> Verify -> Order Confirmed
	// -------------------------------------------------------------------------
	t.Run("HappyPath_Checkout_Payment_Confirmed", func(t *testing.T) {
		// A. Add 2 items to User A's cart
		addPayload, _ := json.Marshal(map[string]interface{}{
			"menu_item_id": itemA.ID,
			"quantity":     2,
		})
		reqAdd := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(addPayload))
		reqAdd.Header.Set("Authorization", "Bearer "+tokenA)
		reqAdd.Header.Set("Content-Type", "application/json")
		wAdd := httptest.NewRecorder()
		appRouter.ServeHTTP(wAdd, reqAdd)
		if wAdd.Code != http.StatusOK {
			t.Fatalf("failed to add item to cart: status %d, body: %s", wAdd.Code, wAdd.Body.String())
		}

		// B. Checkout with Idempotency-Key
		idempKey1 := fmt.Sprintf("idemp_e2e_happy_%d", time.Now().UnixNano())
		reqOrder := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBuffer([]byte(`{}`)))
		reqOrder.Header.Set("Authorization", "Bearer "+tokenA)
		reqOrder.Header.Set("Idempotency-Key", idempKey1)
		reqOrder.Header.Set("Content-Type", "application/json")
		wOrder := httptest.NewRecorder()
		appRouter.ServeHTTP(wOrder, reqOrder)

		if wOrder.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for order, got %d: %s", wOrder.Code, wOrder.Body.String())
		}

		var order models.OrderResponse
		if err := json.Unmarshal(wOrder.Body.Bytes(), &order); err != nil {
			t.Fatalf("failed to parse order response: %v", err)
		}
		if order.Status != models.OrderStatusPending {
			t.Errorf("expected order status PENDING, got %s", order.Status)
		}
		if order.TotalAmount != 200.00 {
			t.Errorf("expected total amount 200.00, got %.2f", order.TotalAmount)
		}

		// Verify inventory deducted (20 - 2 = 18)
		invItem, _ := invRepo.GetByMenuItemID(context.Background(), itemA.ID)
		if invItem == nil || invItem.Quantity != 18 {
			t.Errorf("expected stock 18 after checkout, got %v", invItem)
		}

		// C. Create Payment Order
		reqPay := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", order.ID), nil)
		reqPay.Header.Set("Authorization", "Bearer "+tokenA)
		wPay := httptest.NewRecorder()
		appRouter.ServeHTTP(wPay, reqPay)

		if wPay.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for create payment order, got %d: %s", wPay.Code, wPay.Body.String())
		}

		var payOrderResp models.CreatePaymentOrderResponse
		if err := json.Unmarshal(wPay.Body.Bytes(), &payOrderResp); err != nil {
			t.Fatalf("failed to parse payment order response: %v", err)
		}
		if payOrderResp.RazorpayOrderID == "" {
			t.Fatal("expected non-empty RazorpayOrderID")
		}

		// D. Verify Payment with Valid HMAC Signature
		mockPaymentID := fmt.Sprintf("pay_mock_%d", time.Now().UnixNano())
		validSignature := rzpService.ComputePaymentSignature(payOrderResp.RazorpayOrderID, mockPaymentID)

		verifyPayload, _ := json.Marshal(models.VerifyPaymentRequest{
			RazorpayOrderID:   payOrderResp.RazorpayOrderID,
			RazorpayPaymentID: mockPaymentID,
			RazorpaySignature: validSignature,
		})

		reqVerify := httptest.NewRequest(http.MethodPost, "/api/v1/payments/verify", bytes.NewBuffer(verifyPayload))
		reqVerify.Header.Set("Authorization", "Bearer "+tokenA)
		reqVerify.Header.Set("Content-Type", "application/json")
		wVerify := httptest.NewRecorder()
		appRouter.ServeHTTP(wVerify, reqVerify)

		if wVerify.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for payment verification, got %d: %s", wVerify.Code, wVerify.Body.String())
		}

		// E. Verify PostgreSQL state after confirmation
		orderRepo := repository.NewOrderRepository(db)
		confirmedOrder, err := orderRepo.GetByID(context.Background(), order.ID)
		if err != nil {
			t.Fatalf("failed to fetch order: %v", err)
		}
		if confirmedOrder.Status != models.OrderStatusConfirmed {
			t.Errorf("expected order status CONFIRMED, got %s", confirmedOrder.Status)
		}
		for _, res := range confirmedOrder.Reservations {
			if res.Status != models.ReservationStatusConsumed {
				t.Errorf("expected reservation status CONSUMED, got %s", res.Status)
			}
		}

		// F. Verify Persistent Notification exists in DB
		notifRepo := repository.NewNotificationRepository(db)
		notifResp, err := notifRepo.ListByUser(context.Background(), userA.ID, 10, 0)
		if err != nil || notifResp == nil || len(notifResp.Notifications) == 0 {
			t.Errorf("expected persistent notification for confirmed order, got %v", notifResp)
		}

		// G. Test Verification Idempotency Replay (Phase 7 #4)
		reqVerifyReplay := httptest.NewRequest(http.MethodPost, "/api/v1/payments/verify", bytes.NewBuffer(verifyPayload))
		reqVerifyReplay.Header.Set("Authorization", "Bearer "+tokenA)
		reqVerifyReplay.Header.Set("Content-Type", "application/json")
		wVerifyReplay := httptest.NewRecorder()
		appRouter.ServeHTTP(wVerifyReplay, reqVerifyReplay)
		if wVerifyReplay.Code != http.StatusOK {
			t.Errorf("expected 200 OK on payment verification replay, got %d", wVerifyReplay.Code)
		}
	})

	// -------------------------------------------------------------------------
	// SCENARIO 2: Failure Case 1 — Insufficient Inventory
	// -------------------------------------------------------------------------
	t.Run("Failure_InsufficientInventory", func(t *testing.T) {
		// Attempt to add and buy 999 units (stock is 18)
		addPayload, _ := json.Marshal(map[string]interface{}{
			"menu_item_id": itemA.ID,
			"quantity":     999,
		})
		reqAdd := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(addPayload))
		reqAdd.Header.Set("Authorization", "Bearer "+tokenB)
		reqAdd.Header.Set("Content-Type", "application/json")
		wAdd := httptest.NewRecorder()
		appRouter.ServeHTTP(wAdd, reqAdd)

		idempKey := fmt.Sprintf("idemp_excess_%d", time.Now().UnixNano())
		reqOrder := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBuffer([]byte(`{}`)))
		reqOrder.Header.Set("Authorization", "Bearer "+tokenB)
		reqOrder.Header.Set("Idempotency-Key", idempKey)
		reqOrder.Header.Set("Content-Type", "application/json")
		wOrder := httptest.NewRecorder()
		appRouter.ServeHTTP(wOrder, reqOrder)

		if wOrder.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for insufficient inventory, got %d: %s", wOrder.Code, wOrder.Body.String())
		}
	})

	// -------------------------------------------------------------------------
	// SCENARIO 3: Failure Case 2 — Duplicate Checkout using Same Idempotency-Key
	// -------------------------------------------------------------------------
	t.Run("Failure_DuplicateCheckoutSameKey", func(t *testing.T) {
		// Clear and add 1 item for User B
		clearReq := httptest.NewRequest(http.MethodDelete, "/api/v1/cart", nil)
		clearReq.Header.Set("Authorization", "Bearer "+tokenB)
		appRouter.ServeHTTP(httptest.NewRecorder(), clearReq)

		addPayload, _ := json.Marshal(map[string]interface{}{
			"menu_item_id": itemA.ID,
			"quantity":     1,
		})
		reqAdd := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(addPayload))
		reqAdd.Header.Set("Authorization", "Bearer "+tokenB)
		reqAdd.Header.Set("Content-Type", "application/json")
		appRouter.ServeHTTP(httptest.NewRecorder(), reqAdd)

		sharedKey := fmt.Sprintf("idemp_shared_%d", time.Now().UnixNano())

		// First Checkout
		reqOrder1 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBuffer([]byte(`{}`)))
		reqOrder1.Header.Set("Authorization", "Bearer "+tokenB)
		reqOrder1.Header.Set("Idempotency-Key", sharedKey)
		w1 := httptest.NewRecorder()
		appRouter.ServeHTTP(w1, reqOrder1)
		if w1.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for first order, got %d: %s", w1.Code, w1.Body.String())
		}
		var o1 models.OrderResponse
		_ = json.Unmarshal(w1.Body.Bytes(), &o1)

		// Second Checkout with same key (Replay)
		reqOrder2 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBuffer([]byte(`{}`)))
		reqOrder2.Header.Set("Authorization", "Bearer "+tokenB)
		reqOrder2.Header.Set("Idempotency-Key", sharedKey)
		w2 := httptest.NewRecorder()
		appRouter.ServeHTTP(w2, reqOrder2)

		if w2.Code != http.StatusOK && w2.Code != http.StatusCreated {
			t.Fatalf("expected 200/201 for idempotent duplicate, got %d", w2.Code)
		}
		var o2 models.OrderResponse
		_ = json.Unmarshal(w2.Body.Bytes(), &o2)

		if o1.ID != o2.ID {
			t.Errorf("expected identical order ID '%s', got '%s'", o1.ID, o2.ID)
		}
	})

	// -------------------------------------------------------------------------
	// SCENARIO 4: Failure Case 3 — Signature Tampering Rejected
	// -------------------------------------------------------------------------
	t.Run("Failure_SignatureTampering", func(t *testing.T) {
		tamperedPayload, _ := json.Marshal(models.VerifyPaymentRequest{
			RazorpayOrderID:   "order_tampered_123",
			RazorpayPaymentID: "pay_tampered_456",
			RazorpaySignature: "invalid_tampered_signature_hex_deadbeef",
		})
		reqTampered := httptest.NewRequest(http.MethodPost, "/api/v1/payments/verify", bytes.NewBuffer(tamperedPayload))
		reqTampered.Header.Set("Authorization", "Bearer "+tokenA)
		reqTampered.Header.Set("Content-Type", "application/json")
		wTampered := httptest.NewRecorder()
		appRouter.ServeHTTP(wTampered, reqTampered)

		if wTampered.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for tampered signature, got %d", wTampered.Code)
		}
	})

	// -------------------------------------------------------------------------
	// SCENARIO 5: Failure Case 7 & 8 — Unauthorized & Other User's Payment
	// -------------------------------------------------------------------------
	t.Run("Failure_UnauthorizedAndOtherUserPayment", func(t *testing.T) {
		orderRepo := repository.NewOrderRepository(db)
		ordersA, _ := orderRepo.ListByUserID(context.Background(), userA.ID)
		if len(ordersA) == 0 {
			t.Fatal("no orders found for user A")
		}
		orderAID := ordersA[0].ID

		// User B attempts to pay for User A's order -> 403 Forbidden
		reqPayOther := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", orderAID), nil)
		reqPayOther.Header.Set("Authorization", "Bearer "+tokenB)
		wPayOther := httptest.NewRecorder()
		appRouter.ServeHTTP(wPayOther, reqPayOther)

		if wPayOther.Code != http.StatusForbidden && wPayOther.Code != http.StatusBadRequest {
			t.Errorf("expected 403 Forbidden for paying another user's order, got %d", wPayOther.Code)
		}

		// Unauthenticated request -> 401 Unauthorized
		reqUnauth := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", orderAID), nil)
		wUnauth := httptest.NewRecorder()
		appRouter.ServeHTTP(wUnauth, reqUnauth)

		if wUnauth.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for missing auth header, got %d", wUnauth.Code)
		}
	})

	// -------------------------------------------------------------------------
	// SCENARIO 6: Failure Case 5 & 6 — Webhook Failed Event Restores Inventory Exactly Once
	// -------------------------------------------------------------------------
	t.Run("Failure_WebhookFailureRestoresInventoryExactlyOnce", func(t *testing.T) {
		// Add item for user B and create order
		addPayload, _ := json.Marshal(map[string]interface{}{
			"menu_item_id": itemA.ID,
			"quantity":     3,
		})
		reqAdd := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(addPayload))
		reqAdd.Header.Set("Authorization", "Bearer "+tokenB)
		reqAdd.Header.Set("Content-Type", "application/json")
		appRouter.ServeHTTP(httptest.NewRecorder(), reqAdd)

		invBefore, _ := invRepo.GetByMenuItemID(context.Background(), itemA.ID)
		stockBefore := invBefore.Quantity

		reqOrder := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBuffer([]byte(`{}`)))
		reqOrder.Header.Set("Authorization", "Bearer "+tokenB)
		reqOrder.Header.Set("Idempotency-Key", fmt.Sprintf("idemp_wh_fail_%d", time.Now().UnixNano()))
		wOrder := httptest.NewRecorder()
		appRouter.ServeHTTP(wOrder, reqOrder)
		var orderB models.OrderResponse
		_ = json.Unmarshal(wOrder.Body.Bytes(), &orderB)

		invAfterCheckout, _ := invRepo.GetByMenuItemID(context.Background(), itemA.ID)
		if invAfterCheckout.Quantity != stockBefore-3 {
			t.Errorf("expected stock deduction of 3, got %d (before: %d)", invAfterCheckout.Quantity, stockBefore)
		}

		// Create payment order
		reqPay := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", orderB.ID), nil)
		reqPay.Header.Set("Authorization", "Bearer "+tokenB)
		wPay := httptest.NewRecorder()
		appRouter.ServeHTTP(wPay, reqPay)
		var payResp models.CreatePaymentOrderResponse
		_ = json.Unmarshal(wPay.Body.Bytes(), &payResp)

		// Simulate Razorpay 'payment.failed' webhook
		whPayload := map[string]interface{}{
			"entity": "event",
			"event":  "payment.failed",
			"id":     fmt.Sprintf("evt_fail_%d", time.Now().UnixNano()),
			"payload": map[string]interface{}{
				"payment": map[string]interface{}{
					"entity": map[string]interface{}{
						"id":                fmt.Sprintf("pay_failed_%d", time.Now().UnixNano()),
						"order_id":          payResp.RazorpayOrderID,
						"amount":            30000,
						"status":            "failed",
						"error_code":        "BAD_REQUEST_ERROR",
						"error_description": "Card expired",
					},
				},
			},
		}
		whBytes, _ := json.Marshal(whPayload)
		whSig := rzpService.ComputeWebhookSignature(whBytes)

		reqWH1 := httptest.NewRequest(http.MethodPost, "/api/v1/payments/webhook", bytes.NewBuffer(whBytes))
		reqWH1.Header.Set("X-Razorpay-Signature", whSig)
		reqWH1.Header.Set("Content-Type", "application/json")
		wWH1 := httptest.NewRecorder()
		appRouter.ServeHTTP(wWH1, reqWH1)

		if wWH1.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for payment.failed webhook, got %d: %s", wWH1.Code, wWH1.Body.String())
		}

		// Stock should be restored back by 3
		invAfterFail1, _ := invRepo.GetByMenuItemID(context.Background(), itemA.ID)
		if invAfterFail1.Quantity != stockBefore {
			t.Errorf("expected stock restored to %d, got %d", stockBefore, invAfterFail1.Quantity)
		}

		// Replay duplicate webhook -> should NOT restore inventory twice (Idempotency)
		reqWH2 := httptest.NewRequest(http.MethodPost, "/api/v1/payments/webhook", bytes.NewBuffer(whBytes))
		reqWH2.Header.Set("X-Razorpay-Signature", whSig)
		reqWH2.Header.Set("Content-Type", "application/json")
		wWH2 := httptest.NewRecorder()
		appRouter.ServeHTTP(wWH2, reqWH2)

		if wWH2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for duplicate webhook, got %d", wWH2.Code)
		}

		invAfterFail2, _ := invRepo.GetByMenuItemID(context.Background(), itemA.ID)
		if invAfterFail2.Quantity != stockBefore {
			t.Errorf("inventory double-restored on duplicate webhook: expected %d, got %d", stockBefore, invAfterFail2.Quantity)
		}
	})

	// -------------------------------------------------------------------------
	// SCENARIO 7: Failure Case 9 — Reservation Expiry Restores Inventory
	// -------------------------------------------------------------------------
	t.Run("Failure_ReservationExpiryRestoresInventory", func(t *testing.T) {
		// Add item and place order
		addPayload, _ := json.Marshal(map[string]interface{}{
			"menu_item_id": itemA.ID,
			"quantity":     4,
		})
		reqAdd := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(addPayload))
		reqAdd.Header.Set("Authorization", "Bearer "+tokenA)
		reqAdd.Header.Set("Content-Type", "application/json")
		appRouter.ServeHTTP(httptest.NewRecorder(), reqAdd)

		invBefore, _ := invRepo.GetByMenuItemID(context.Background(), itemA.ID)
		stockBefore := invBefore.Quantity

		reqOrder := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBuffer([]byte(`{}`)))
		reqOrder.Header.Set("Authorization", "Bearer "+tokenA)
		reqOrder.Header.Set("Idempotency-Key", fmt.Sprintf("idemp_exp_%d", time.Now().UnixNano()))
		wOrder := httptest.NewRecorder()
		appRouter.ServeHTTP(wOrder, reqOrder)
		var orderExp models.OrderResponse
		_ = json.Unmarshal(wOrder.Body.Bytes(), &orderExp)

		// Force reservation expiry in PostgreSQL
		_, err := db.Pool.Exec(context.Background(), `
			UPDATE inventory_reservations
			SET expires_at = NOW() - INTERVAL '1 minute'
			WHERE order_id = $1
		`, orderExp.ID)
		if err != nil {
			t.Fatalf("failed to update reservation expires_at: %v", err)
		}

		// Run reservation expiry worker logic
		resWorker := worker.NewReservationWorker(db, 100*time.Millisecond)
		processed, err := resWorker.ProcessExpiredReservations(context.Background())
		if err != nil {
			t.Fatalf("failed to release expired reservations: %v", err)
		}
		if processed < 1 {
			t.Fatalf("expected at least 1 reservation processed, got %d", processed)
		}

		// Stock should be restored (+4)
		invAfterExpiry, _ := invRepo.GetByMenuItemID(context.Background(), itemA.ID)
		if invAfterExpiry.Quantity != stockBefore {
			t.Errorf("expected stock restored to %d after expiry, got %d", stockBefore, invAfterExpiry.Quantity)
		}
	})
}
