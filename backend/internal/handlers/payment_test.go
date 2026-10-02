package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"campusbite/internal/auth"
	"campusbite/internal/cache"
	"campusbite/internal/database"
	"campusbite/internal/models"
	"campusbite/internal/repository"
	"campusbite/internal/routes"
	"campusbite/internal/service"

	"github.com/gin-gonic/gin"
)

// helper to create a full checkout order with items and active inventory reservations
func createTestOrder(t *testing.T, router *gin.Engine, db *database.DB, tokenService *auth.TokenService) (*models.User, string, *models.OrderResponse) {
	user, token := createRealTestUser(t, db, tokenService, models.RoleStudent)

	menuRepo := repository.NewMenuRepository(db)
	itemPrice := 150.0
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("Pizza_%d", time.Now().UnixNano()), "Fresh Pizza", itemPrice, "", true, 20)
	if err != nil {
		t.Fatalf("failed to create menu item for test order: %v", err)
	}

	cartRepo := repository.NewCartRepository(db)
	_, err = cartRepo.AddItem(context.Background(), user.ID, item.ID, 2)
	if err != nil {
		t.Fatalf("failed to add item to cart: %v", err)
	}

	// Checkout via POST /api/v1/orders with Idempotency-Key
	idempotencyKey := fmt.Sprintf("order-key-%d", time.Now().UnixNano())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", idempotencyKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("failed to checkout order: status %d, body %s", w.Code, w.Body.String())
	}

	var orderResp models.OrderResponse
	if err := json.Unmarshal(w.Body.Bytes(), &orderResp); err != nil {
		t.Fatalf("failed to decode order response: %v", err)
	}

	return user, token, &orderResp
}

func TestPayment_CreateOrder(t *testing.T) {
	testKeySecret := "rzp_test_campusbite_mock_secret"
	rzpService := service.NewRazorpayService("rzp_test_campusbite_mock_key", testKeySecret, "rzp_test_campusbite_mock_webhook_secret")

	db, tokenService := func() (*database.DB, *auth.TokenService) {
		routerTemp, db, ts := setupIntegrationApp(t)
		_ = routerTemp
		return db, ts
	}()
	defer db.Close()

	router := routes.SetupRouter(db, tokenService, rzpService, cache.NewNoOpCache())

	user1, studentToken1, order := createTestOrder(t, router, db, tokenService)
	_, studentToken2 := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_, adminToken := createTestTokens(t, tokenService)

	// 1. Valid payment order creation by owner
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", order.ID), nil)
	req.Header.Set("Authorization", "Bearer "+studentToken1)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for payment order creation, got %d: %s", w.Code, w.Body.String())
	}

	var paymentResp models.CreatePaymentOrderResponse
	if err := json.Unmarshal(w.Body.Bytes(), &paymentResp); err != nil {
		t.Fatalf("failed to parse payment response: %v", err)
	}

	if paymentResp.OrderID != order.ID {
		t.Errorf("expected order_id %s, got %s", order.ID, paymentResp.OrderID)
	}
	if paymentResp.Amount != 30000 { // 2 * 150 = 300.00 -> 30000 paise
		t.Errorf("expected amount 30000 paise, got %d", paymentResp.Amount)
	}
	if paymentResp.Currency != "INR" {
		t.Errorf("expected currency INR, got %s", paymentResp.Currency)
	}
	if paymentResp.RazorpayOrderID == "" {
		t.Errorf("expected non-empty razorpay_order_id")
	}

	// 2. Ownership check: Student 2 cannot initiate payment for Student 1's order -> 403 Forbidden
	req2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", order.ID), nil)
	req2.Header.Set("Authorization", "Bearer "+studentToken2)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for non-owner student, got %d: %s", w2.Code, w2.Body.String())
	}

	// 3. Admin can access order payment -> 200 OK
	req3 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", order.ID), nil)
	req3.Header.Set("Authorization", "Bearer "+adminToken)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Errorf("expected 200 OK for admin initiating payment, got %d: %s", w3.Code, w3.Body.String())
	}

	// 4. Non-existent order -> 404 Not Found
	nonExistentOrderID := "550e8400-e29b-41d4-a716-446655440000"
	req4 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", nonExistentOrderID), nil)
	req4.Header.Set("Authorization", "Bearer "+studentToken1)
	w4 := httptest.NewRecorder()
	router.ServeHTTP(w4, req4)
	if w4.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for non-existent order, got %d: %s", w4.Code, w4.Body.String())
	}

	_ = user1
}

func TestPayment_Verify_SuccessAndIdempotency(t *testing.T) {
	testKeySecret := "rzp_test_campusbite_mock_secret"
	rzpService := service.NewRazorpayService("rzp_test_campusbite_mock_key", testKeySecret, "rzp_test_campusbite_mock_webhook_secret")

	db, tokenService := func() (*database.DB, *auth.TokenService) {
		routerTemp, db, ts := setupIntegrationApp(t)
		_ = routerTemp
		return db, ts
	}()
	defer db.Close()

	router := routes.SetupRouter(db, tokenService, rzpService, cache.NewNoOpCache())

	_, studentToken, order := createTestOrder(t, router, db, tokenService)

	// 1. Create payment order first
	req1 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", order.ID), nil)
	req1.Header.Set("Authorization", "Bearer "+studentToken)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on payment creation, got %d: %s", w1.Code, w1.Body.String())
	}

	var createResp models.CreatePaymentOrderResponse
	_ = json.Unmarshal(w1.Body.Bytes(), &createResp)

	providerOrderID := createResp.RazorpayOrderID
	providerPaymentID := "pay_test_" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Compute valid signature
	validSignature := rzpService.ComputePaymentSignature(providerOrderID, providerPaymentID)

	// 2. Test invalid signature rejection -> 400 Bad Request
	invalidReqBody, _ := json.Marshal(models.VerifyPaymentRequest{
		RazorpayOrderID:   providerOrderID,
		RazorpayPaymentID: providerPaymentID,
		RazorpaySignature: "invalid_tampered_signature",
	})
	reqInvalid := httptest.NewRequest(http.MethodPost, "/api/v1/payments/verify", bytes.NewBuffer(invalidReqBody))
	reqInvalid.Header.Set("Authorization", "Bearer "+studentToken)
	reqInvalid.Header.Set("Content-Type", "application/json")
	wInvalid := httptest.NewRecorder()
	router.ServeHTTP(wInvalid, reqInvalid)
	if wInvalid.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid signature, got %d: %s", wInvalid.Code, wInvalid.Body.String())
	}

	// 3. Test valid signature verification -> 200 OK
	validReqBody, _ := json.Marshal(models.VerifyPaymentRequest{
		RazorpayOrderID:   providerOrderID,
		RazorpayPaymentID: providerPaymentID,
		RazorpaySignature: validSignature,
	})
	reqValid := httptest.NewRequest(http.MethodPost, "/api/v1/payments/verify", bytes.NewBuffer(validReqBody))
	reqValid.Header.Set("Authorization", "Bearer "+studentToken)
	reqValid.Header.Set("Content-Type", "application/json")
	wValid := httptest.NewRecorder()
	router.ServeHTTP(wValid, reqValid)
	if wValid.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid payment verification, got %d: %s", wValid.Code, wValid.Body.String())
	}

	// 4. Verify DB state: order -> CONFIRMED, reservation -> CONSUMED, payment -> SUCCESS
	orderRepo := repository.NewOrderRepository(db)
	updatedOrder, err := orderRepo.GetByID(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("failed to retrieve updated order: %v", err)
	}
	if updatedOrder.Status != models.OrderStatusConfirmed {
		t.Errorf("expected order status CONFIRMED, got %s", updatedOrder.Status)
	}
	for _, res := range updatedOrder.Reservations {
		if res.Status != models.ReservationStatusConsumed {
			t.Errorf("expected reservation status CONSUMED, got %s", res.Status)
		}
	}

	paymentRepo := repository.NewPaymentRepository(db, orderRepo)
	payment, err := paymentRepo.GetPaymentByOrderID(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("failed to retrieve payment: %v", err)
	}
	if payment.Status != models.PaymentStatusSuccess {
		t.Errorf("expected payment status SUCCESS, got %s", payment.Status)
	}

	// 5. Test Verification Idempotency: Repeating the verification call succeeds with 200 OK
	reqRepeat := httptest.NewRequest(http.MethodPost, "/api/v1/payments/verify", bytes.NewBuffer(validReqBody))
	reqRepeat.Header.Set("Authorization", "Bearer "+studentToken)
	reqRepeat.Header.Set("Content-Type", "application/json")
	wRepeat := httptest.NewRecorder()
	router.ServeHTTP(wRepeat, reqRepeat)
	if wRepeat.Code != http.StatusOK {
		t.Errorf("expected 200 OK on repeated verification, got %d: %s", wRepeat.Code, wRepeat.Body.String())
	}
}

func TestPayment_Webhook_CapturedAndFailed(t *testing.T) {
	testWebhookSecret := "rzp_test_campusbite_mock_webhook_secret"
	rzpService := service.NewRazorpayService("rzp_test_campusbite_mock_key", "rzp_test_campusbite_mock_secret", testWebhookSecret)

	db, tokenService := func() (*database.DB, *auth.TokenService) {
		routerTemp, db, ts := setupIntegrationApp(t)
		_ = routerTemp
		return db, ts
	}()
	defer db.Close()

	router := routes.SetupRouter(db, tokenService, rzpService, cache.NewNoOpCache())

	// Scenario A: Webhook payment.captured
	_, studentTokenA, orderA := createTestOrder(t, router, db, tokenService)

	// Initiate payment order A
	reqA := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", orderA.ID), nil)
	reqA.Header.Set("Authorization", "Bearer "+studentTokenA)
	wA := httptest.NewRecorder()
	router.ServeHTTP(wA, reqA)
	var respA models.CreatePaymentOrderResponse
	_ = json.Unmarshal(wA.Body.Bytes(), &respA)

	webhookPayloadA := models.RazorpayWebhookPayload{
		Entity:    "event",
		AccountID: "acc_test",
		Event:     "payment.captured",
		Payload: models.RazorpayWebhookData{
			Payment: &models.RazorpayPaymentEntityWrapper{
				Entity: models.RazorpayPaymentEntity{
					ID:      "pay_webhook_cap_" + fmt.Sprintf("%d", time.Now().UnixNano()),
					OrderID: respA.RazorpayOrderID,
					Amount:  respA.Amount,
					Status:  "captured",
				},
			},
		},
		CreatedAt: time.Now().Unix(),
	}
	payloadBytesA, _ := json.Marshal(webhookPayloadA)
	validWebhookSigA := rzpService.ComputeWebhookSignature(payloadBytesA)

	// Send webhook without signature -> 400 Bad Request
	reqNoSig := httptest.NewRequest(http.MethodPost, "/api/v1/payments/webhook", bytes.NewBuffer(payloadBytesA))
	wNoSig := httptest.NewRecorder()
	router.ServeHTTP(wNoSig, reqNoSig)
	if wNoSig.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for missing signature, got %d", wNoSig.Code)
	}

	// Send webhook with valid signature -> 200 OK
	reqWebA := httptest.NewRequest(http.MethodPost, "/api/v1/payments/webhook", bytes.NewBuffer(payloadBytesA))
	reqWebA.Header.Set("X-Razorpay-Signature", validWebhookSigA)
	reqWebA.Header.Set("Content-Type", "application/json")
	wWebA := httptest.NewRecorder()
	router.ServeHTTP(wWebA, reqWebA)
	if wWebA.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for payment.captured webhook, got %d: %s", wWebA.Code, wWebA.Body.String())
	}

	// Verify order A is CONFIRMED
	orderRepo := repository.NewOrderRepository(db)
	updatedOrderA, _ := orderRepo.GetByID(context.Background(), orderA.ID)
	if updatedOrderA.Status != models.OrderStatusConfirmed {
		t.Errorf("expected order A status CONFIRMED, got %s", updatedOrderA.Status)
	}

	// Send duplicate webhook A -> deduplication ignores and returns 200 OK
	reqWebADup := httptest.NewRequest(http.MethodPost, "/api/v1/payments/webhook", bytes.NewBuffer(payloadBytesA))
	reqWebADup.Header.Set("X-Razorpay-Signature", validWebhookSigA)
	reqWebADup.Header.Set("Content-Type", "application/json")
	wWebADup := httptest.NewRecorder()
	router.ServeHTTP(wWebADup, reqWebADup)
	if wWebADup.Code != http.StatusOK {
		t.Errorf("expected 200 OK for duplicate webhook, got %d", wWebADup.Code)
	}

	// Scenario B: Webhook payment.failed -> Restores Inventory Quantity
	_, studentTokenB, orderB := createTestOrder(t, router, db, tokenService)

	// Check inventory before failure
	inventoryRepo := repository.NewInventoryRepository(db)
	menuItemID := orderB.Items[0].MenuItemID
	invBefore, _ := inventoryRepo.GetByMenuItemID(context.Background(), menuItemID)

	reqB := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", orderB.ID), nil)
	reqB.Header.Set("Authorization", "Bearer "+studentTokenB)
	wB := httptest.NewRecorder()
	router.ServeHTTP(wB, reqB)
	var respB models.CreatePaymentOrderResponse
	_ = json.Unmarshal(wB.Body.Bytes(), &respB)

	webhookPayloadB := models.RazorpayWebhookPayload{
		Entity:    "event",
		AccountID: "acc_test",
		Event:     "payment.failed",
		Payload: models.RazorpayWebhookData{
			Payment: &models.RazorpayPaymentEntityWrapper{
				Entity: models.RazorpayPaymentEntity{
					ID:          "pay_webhook_fail_" + fmt.Sprintf("%d", time.Now().UnixNano()),
					OrderID:     respB.RazorpayOrderID,
					Amount:      respB.Amount,
					Status:      "failed",
					ErrorReason: "payment_declined_by_bank",
				},
			},
		},
		CreatedAt: time.Now().Unix(),
	}
	payloadBytesB, _ := json.Marshal(webhookPayloadB)
	validWebhookSigB := rzpService.ComputeWebhookSignature(payloadBytesB)

	reqWebB := httptest.NewRequest(http.MethodPost, "/api/v1/payments/webhook", bytes.NewBuffer(payloadBytesB))
	reqWebB.Header.Set("X-Razorpay-Signature", validWebhookSigB)
	reqWebB.Header.Set("Content-Type", "application/json")
	wWebB := httptest.NewRecorder()
	router.ServeHTTP(wWebB, reqWebB)
	if wWebB.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for payment.failed webhook, got %d: %s", wWebB.Code, wWebB.Body.String())
	}

	// Verify order B status is CANCELLED
	updatedOrderB, _ := orderRepo.GetByID(context.Background(), orderB.ID)
	if updatedOrderB.Status != models.OrderStatusCancelled {
		t.Errorf("expected order B status CANCELLED, got %s", updatedOrderB.Status)
	}

	// Verify inventory was restored back
	invAfter, _ := inventoryRepo.GetByMenuItemID(context.Background(), menuItemID)
	if invAfter.Quantity != invBefore.Quantity+orderB.Items[0].Quantity {
		t.Errorf("expected inventory restored to %d, got %d", invBefore.Quantity+orderB.Items[0].Quantity, invAfter.Quantity)
	}

	// Verify reservations are RELEASED
	for _, res := range updatedOrderB.Reservations {
		if res.Status != models.ReservationStatusReleased {
			t.Errorf("expected reservation status RELEASED, got %s", res.Status)
		}
	}
}

func TestPayment_ConcurrentWebhookAndVerifyRace(t *testing.T) {
	testKeySecret := "rzp_test_campusbite_mock_secret"
	testWebhookSecret := "rzp_test_campusbite_mock_webhook_secret"
	rzpService := service.NewRazorpayService("rzp_test_campusbite_mock_key", testKeySecret, testWebhookSecret)

	db, tokenService := func() (*database.DB, *auth.TokenService) {
		routerTemp, db, ts := setupIntegrationApp(t)
		_ = routerTemp
		return db, ts
	}()
	defer db.Close()

	router := routes.SetupRouter(db, tokenService, rzpService, cache.NewNoOpCache())

	_, studentToken, order := createTestOrder(t, router, db, tokenService)

	// Initiate payment order
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", order.ID), nil)
	req.Header.Set("Authorization", "Bearer "+studentToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var resp models.CreatePaymentOrderResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	providerOrderID := resp.RazorpayOrderID
	providerPaymentID := "pay_race_" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Client verification request
	clientSig := rzpService.ComputePaymentSignature(providerOrderID, providerPaymentID)
	verifyReqBody, _ := json.Marshal(models.VerifyPaymentRequest{
		RazorpayOrderID:   providerOrderID,
		RazorpayPaymentID: providerPaymentID,
		RazorpaySignature: clientSig,
	})

	// Webhook request
	webhookPayload := models.RazorpayWebhookPayload{
		Entity:    "event",
		AccountID: "acc_race",
		Event:     "payment.captured",
		Payload: models.RazorpayWebhookData{
			Payment: &models.RazorpayPaymentEntityWrapper{
				Entity: models.RazorpayPaymentEntity{
					ID:      providerPaymentID,
					OrderID: providerOrderID,
					Amount:  resp.Amount,
					Status:  "captured",
				},
			},
		},
		CreatedAt: time.Now().Unix(),
	}
	webhookBytes, _ := json.Marshal(webhookPayload)
	webhookSig := rzpService.ComputeWebhookSignature(webhookBytes)

	var wg sync.WaitGroup
	wg.Add(2)

	var codeVerify, codeWebhook int

	go func() {
		defer wg.Done()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/payments/verify", bytes.NewBuffer(verifyReqBody))
		r.Header.Set("Authorization", "Bearer "+studentToken)
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		codeVerify = rec.Code
	}()

	go func() {
		defer wg.Done()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/payments/webhook", bytes.NewBuffer(webhookBytes))
		r.Header.Set("X-Razorpay-Signature", webhookSig)
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		codeWebhook = rec.Code
	}()

	wg.Wait()

	if codeVerify != http.StatusOK {
		t.Errorf("expected verify status 200 OK, got %d", codeVerify)
	}
	if codeWebhook != http.StatusOK {
		t.Errorf("expected webhook status 200 OK, got %d", codeWebhook)
	}

	// Ensure final state is clean and consistent
	orderRepo := repository.NewOrderRepository(db)
	finalOrder, err := orderRepo.GetByID(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("failed to retrieve final order: %v", err)
	}
	if finalOrder.Status != models.OrderStatusConfirmed {
		t.Errorf("expected final order status CONFIRMED, got %s", finalOrder.Status)
	}

	paymentRepo := repository.NewPaymentRepository(db, orderRepo)
	finalPayment, err := paymentRepo.GetPaymentByOrderID(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("failed to retrieve final payment: %v", err)
	}
	if finalPayment.Status != models.PaymentStatusSuccess {
		t.Errorf("expected final payment status SUCCESS, got %s", finalPayment.Status)
	}
}

func TestPayment_ExactAmountVerification_30INR(t *testing.T) {
	testKeySecret := "rzp_test_campusbite_mock_secret"
	rzpService := service.NewRazorpayService("rzp_test_campusbite_mock_key", testKeySecret, "rzp_test_campusbite_mock_webhook_secret")

	db, tokenService := func() (*database.DB, *auth.TokenService) {
		routerTemp, db, ts := setupIntegrationApp(t)
		_ = routerTemp
		return db, ts
	}()
	defer db.Close()

	router := routes.SetupRouter(db, tokenService, rzpService, cache.NewNoOpCache())

	user, token := createRealTestUser(t, db, tokenService, models.RoleStudent)

	// Create item with price exactly 30.00 INR
	menuRepo := repository.NewMenuRepository(db)
	itemPrice := 30.00
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("Chai_Test_%d", time.Now().UnixNano()), "Hot Chai", itemPrice, "", true, 50)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}

	cartRepo := repository.NewCartRepository(db)
	_, err = cartRepo.AddItem(context.Background(), user.ID, item.ID, 1)
	if err != nil {
		t.Fatalf("failed to add item to cart: %v", err)
	}

	// Checkout via POST /api/v1/orders
	idempotencyKey := fmt.Sprintf("order-key-30-%d", time.Now().UnixNano())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", idempotencyKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("failed to checkout order: status %d, body %s", w.Code, w.Body.String())
	}

	var orderResp models.OrderResponse
	if err := json.Unmarshal(w.Body.Bytes(), &orderResp); err != nil {
		t.Fatalf("failed to decode order response: %v", err)
	}

	if orderResp.TotalAmount != 30.00 {
		t.Fatalf("expected order TotalAmount to be exactly 30.00, got %.2f", orderResp.TotalAmount)
	}

	// Create Payment Order
	reqPay := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", orderResp.ID), nil)
	reqPay.Header.Set("Authorization", "Bearer "+token)
	wPay := httptest.NewRecorder()
	router.ServeHTTP(wPay, reqPay)

	if wPay.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for payment order creation, got %d: %s", wPay.Code, wPay.Body.String())
	}

	var paymentResp models.CreatePaymentOrderResponse
	if err := json.Unmarshal(wPay.Body.Bytes(), &paymentResp); err != nil {
		t.Fatalf("failed to parse payment response: %v", err)
	}

	if paymentResp.Amount != 3000 {
		t.Fatalf("expected Razorpay payment amount to be exactly 3000 paise (30.00 INR), got %d paise", paymentResp.Amount)
	}
}

