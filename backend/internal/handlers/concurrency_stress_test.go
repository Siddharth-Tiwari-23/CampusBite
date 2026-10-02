package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"campusbite/internal/auth"
	"campusbite/internal/cache"
	"campusbite/internal/database"
	"campusbite/internal/models"
	"campusbite/internal/repository"
	"campusbite/internal/routes"
	"campusbite/internal/service"
	"campusbite/internal/worker"
	"campusbite/internal/ws"
)

// ============================================================================
// 1. INVENTORY CONCURRENCY TEST: 150 Concurrent Checkouts for 30 Stock
// ============================================================================
func TestConcurrency_Inventory_150Requests_30Stock(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	ctx := context.Background()
	initialStock := 30
	concurrentUsers := 150

	menuRepo := repository.NewMenuRepository(db)
	cartRepo := repository.NewCartRepository(db)
	invRepo := repository.NewInventoryRepository(db)

	// Create single menu item with exactly 30 stock
	itemName := fmt.Sprintf("ConcurBurger_%d", time.Now().UnixNano())
	item, err := menuRepo.Create(ctx, itemName, "High concurrency burger", 100.0, "", true, initialStock)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}

	// Create 150 distinct students and have each add 1 unit to their cart
	studentTokens := make([]string, concurrentUsers)
	studentUsers := make([]*models.User, concurrentUsers)

	for i := 0; i < concurrentUsers; i++ {
		u, tok := createRealTestUser(t, db, tokenService, models.RoleStudent)
		studentUsers[i] = u
		studentTokens[i] = tok

		_, err = cartRepo.AddItem(ctx, u.ID, item.ID, 1)
		if err != nil {
			t.Fatalf("failed to add item to cart for user %d: %v", i, err)
		}
	}

	// Concurrency barrier: all 150 goroutines fire simultaneously
	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(concurrentUsers)

	var successCount int64
	var rejectedCount int64
	var unexpectedCount int64

	for i := 0; i < concurrentUsers; i++ {
		idx := i
		go func(token string) {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Idempotency-Key", fmt.Sprintf("concur_150_key_%d_%d", idx, time.Now().UnixNano()))

			<-startBarrier // await synchronized release
			router.ServeHTTP(w, req)

			switch w.Code {
			case http.StatusCreated:
				atomic.AddInt64(&successCount, 1)
			case http.StatusBadRequest:
				atomic.AddInt64(&rejectedCount, 1)
			default:
				atomic.AddInt64(&unexpectedCount, 1)
			}
		}(studentTokens[idx])
	}

	// Trigger simultaneous checkout flood
	close(startBarrier)
	wg.Wait()

	if unexpectedCount > 0 {
		t.Fatalf("encountered %d unexpected HTTP response codes during concurrency run", unexpectedCount)
	}

	// Invariant 1: Successful orders must be exactly 30
	if successCount != int64(initialStock) {
		t.Errorf("expected exactly %d successful orders, got %d", initialStock, successCount)
	}

	// Invariant 2: Rejected orders must be exactly 120 (150 - 30)
	if rejectedCount != int64(concurrentUsers-initialStock) {
		t.Errorf("expected exactly %d rejected orders (insufficient stock), got %d", concurrentUsers-initialStock, rejectedCount)
	}

	// Invariant 3: Verify PostgreSQL inventory table state directly via SQL
	inv, err := invRepo.GetByMenuItemID(ctx, item.ID)
	if err != nil {
		t.Fatalf("failed to query inventory: %v", err)
	}
	if inv.Quantity != 0 {
		t.Errorf("expected final inventory quantity 0, got %d", inv.Quantity)
	}

	// Invariant 4: No inventory quantity can be negative
	var negativeCount int
	err = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM inventory WHERE quantity < 0").Scan(&negativeCount)
	if err != nil {
		t.Fatalf("failed to check for negative inventory: %v", err)
	}
	if negativeCount > 0 {
		t.Fatalf("CRITICAL: found %d rows with negative inventory quantity!", negativeCount)
	}

	// Invariant 5: Verify exact count of active reservations in database
	var activeReservations int
	err = db.Pool.QueryRow(ctx, `
		SELECT COUNT(*) 
		FROM inventory_reservations 
		WHERE menu_item_id = $1 AND status = 'ACTIVE'
	`, item.ID).Scan(&activeReservations)
	if err != nil {
		t.Fatalf("failed to query active reservations: %v", err)
	}
	if activeReservations != initialStock {
		t.Errorf("expected %d active reservations in DB, got %d", initialStock, activeReservations)
	}

	// Invariant 6: Verify total orders created matches successful count
	var totalOrders int
	err = db.Pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT o.id) 
		FROM orders o 
		JOIN order_items oi ON o.id = oi.order_id 
		WHERE oi.menu_item_id = $1
	`, item.ID).Scan(&totalOrders)
	if err != nil {
		t.Fatalf("failed to query total orders: %v", err)
	}
	if totalOrders != initialStock {
		t.Errorf("expected %d total orders in DB, got %d", initialStock, totalOrders)
	}
}

// ============================================================================
// 2. EXTREME SMALL-STOCK CONCURRENCY TESTS
// ============================================================================
func TestConcurrency_SmallStock_1Stock_2Requests(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	ctx := context.Background()
	menuRepo := repository.NewMenuRepository(db)
	cartRepo := repository.NewCartRepository(db)
	invRepo := repository.NewInventoryRepository(db)

	item, err := menuRepo.Create(ctx, fmt.Sprintf("OneStock_%d", time.Now().UnixNano()), "Single Item", 50.0, "", true, 1)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}

	u1, tok1 := createRealTestUser(t, db, tokenService, models.RoleStudent)
	u2, tok2 := createRealTestUser(t, db, tokenService, models.RoleStudent)

	_, _ = cartRepo.AddItem(ctx, u1.ID, item.ID, 1)
	_, _ = cartRepo.AddItem(ctx, u2.ID, item.ID, 1)

	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	codes := make([]int, 2)
	tokens := []string{tok1, tok2}

	for i := 0; i < 2; i++ {
		idx := i
		go func(token string) {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Idempotency-Key", fmt.Sprintf("race_1stock_%d_%d", idx, time.Now().UnixNano()))

			<-startBarrier
			router.ServeHTTP(w, req)
			codes[idx] = w.Code
		}(tokens[idx])
	}

	close(startBarrier)
	wg.Wait()

	var success, badReq int
	for _, c := range codes {
		if c == http.StatusCreated {
			success++
		} else if c == http.StatusBadRequest {
			badReq++
		}
	}

	if success != 1 || badReq != 1 {
		t.Fatalf("expected exactly 1 success and 1 bad request for 1-stock race, got %d success, %d badReq", success, badReq)
	}

	inv, _ := invRepo.GetByMenuItemID(ctx, item.ID)
	if inv.Quantity != 0 {
		t.Errorf("expected final inventory 0, got %d", inv.Quantity)
	}
}

func TestConcurrency_SmallStock_5Stock_20Requests(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	ctx := context.Background()
	menuRepo := repository.NewMenuRepository(db)
	cartRepo := repository.NewCartRepository(db)
	invRepo := repository.NewInventoryRepository(db)

	initialStock := 5
	concurrentRequests := 20

	item, err := menuRepo.Create(ctx, fmt.Sprintf("FiveStock_%d", time.Now().UnixNano()), "Five items", 75.0, "", true, initialStock)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}

	tokens := make([]string, concurrentRequests)
	for i := 0; i < concurrentRequests; i++ {
		u, tok := createRealTestUser(t, db, tokenService, models.RoleStudent)
		tokens[i] = tok
		_, _ = cartRepo.AddItem(ctx, u.ID, item.ID, 1)
	}

	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(concurrentRequests)

	var successCount, badReqCount int64

	for i := 0; i < concurrentRequests; i++ {
		idx := i
		go func(token string) {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Idempotency-Key", fmt.Sprintf("race_5stock_%d_%d", idx, time.Now().UnixNano()))

			<-startBarrier
			router.ServeHTTP(w, req)

			if w.Code == http.StatusCreated {
				atomic.AddInt64(&successCount, 1)
			} else if w.Code == http.StatusBadRequest {
				atomic.AddInt64(&badReqCount, 1)
			}
		}(tokens[idx])
	}

	close(startBarrier)
	wg.Wait()

	if successCount != int64(initialStock) {
		t.Errorf("expected %d successes for 5-stock race, got %d", initialStock, successCount)
	}
	if badReqCount != int64(concurrentRequests-initialStock) {
		t.Errorf("expected %d bad requests for 5-stock race, got %d", concurrentRequests-initialStock, badReqCount)
	}

	inv, _ := invRepo.GetByMenuItemID(ctx, item.ID)
	if inv.Quantity != 0 {
		t.Errorf("expected final inventory 0, got %d", inv.Quantity)
	}
}

// ============================================================================
// 3. SAME IDEMPOTENCY KEY CONCURRENCY: 50 Concurrent Requests
// ============================================================================
func TestConcurrency_SameIdempotencyKey_50Requests(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	ctx := context.Background()
	user, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	menuRepo := repository.NewMenuRepository(db)
	cartRepo := repository.NewCartRepository(db)
	invRepo := repository.NewInventoryRepository(db)

	item, err := menuRepo.Create(ctx, fmt.Sprintf("SameKeyItem_%d", time.Now().UnixNano()), "Idempotent Noodles", 90.0, "", true, 20)
	if err != nil {
		t.Fatalf("failed to seed menu item: %v", err)
	}

	// User adds 2 units to cart
	_, err = cartRepo.AddItem(ctx, user.ID, item.ID, 2)
	if err != nil {
		t.Fatalf("failed to add item: %v", err)
	}

	idempotencyKey := fmt.Sprintf("concurrent_50_same_key_%d", time.Now().UnixNano())
	concurrentRequests := 50

	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(concurrentRequests)

	responses := make([]*httptest.ResponseRecorder, concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		idx := i
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
			req.Header.Set("Authorization", "Bearer "+studentToken)
			req.Header.Set("Idempotency-Key", idempotencyKey)

			<-startBarrier
			router.ServeHTTP(w, req)
			responses[idx] = w
		}()
	}

	close(startBarrier)
	wg.Wait()

	// Every response must be either:
	// - 201 Created (the initial winning request)
	// - 200 OK (subsequent requests returning cached completed order)
	// - 409 Conflict (concurrent requests hitting while first is in-flight)
	var createdCount, okCount, conflictCount, unexpectedCount int
	for _, w := range responses {
		switch w.Code {
		case http.StatusCreated:
			createdCount++
		case http.StatusOK:
			okCount++
		case http.StatusConflict:
			conflictCount++
		default:
			unexpectedCount++
		}
	}

	if unexpectedCount > 0 {
		t.Errorf("encountered unexpected HTTP status codes: %d unexpected", unexpectedCount)
	}
	if createdCount < 1 {
		t.Errorf("expected at least 1 StatusCreated (201), got %d", createdCount)
	}

	// Invariant 1: Exactly 1 order in PostgreSQL database for this user
	var orderCount int
	err = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM orders WHERE user_id = $1", user.ID).Scan(&orderCount)
	if err != nil || orderCount != 1 {
		t.Fatalf("CRITICAL: expected exactly 1 order in DB, found %d", orderCount)
	}

	// Invariant 2: Exactly 1 reservation created in PostgreSQL
	var resCount int
	err = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM inventory_reservations ir JOIN orders o ON ir.order_id = o.id WHERE o.user_id = $1", user.ID).Scan(&resCount)
	if err != nil || resCount != 1 {
		t.Fatalf("CRITICAL: expected exactly 1 inventory reservation in DB, found %d (err: %v)", resCount, err)
	}

	// Invariant 3: Exactly 1 idempotency_keys row recorded
	var keyCount int
	err = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM idempotency_keys WHERE user_id = $1 AND key = $2", user.ID, idempotencyKey).Scan(&keyCount)
	if err != nil || keyCount != 1 {
		t.Fatalf("CRITICAL: expected exactly 1 idempotency key in DB, found %d", keyCount)
	}

	// Invariant 4: Inventory quantity deducted exactly once (20 - 2 = 18)
	inv, err := invRepo.GetByMenuItemID(ctx, item.ID)
	if err != nil || inv.Quantity != 18 {
		t.Fatalf("expected inventory to be 18, got %d (err: %v)", inv.Quantity, err)
	}
}

// ============================================================================
// 4. DIFFERENT IDEMPOTENCY KEYS: Independent Operations
// ============================================================================
func TestConcurrency_DifferentIdempotencyKeys_Independent(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	ctx := context.Background()
	user, token := createRealTestUser(t, db, tokenService, models.RoleStudent)

	menuRepo := repository.NewMenuRepository(db)
	cartRepo := repository.NewCartRepository(db)

	item1, _ := menuRepo.Create(ctx, fmt.Sprintf("DiffKeyItem1_%d", time.Now().UnixNano()), "Item 1", 100.0, "", true, 20)
	item2, _ := menuRepo.Create(ctx, fmt.Sprintf("DiffKeyItem2_%d", time.Now().UnixNano()), "Item 2", 50.0, "", true, 20)

	// Order 1
	_, _ = cartRepo.AddItem(ctx, user.ID, item1.ID, 1)
	key1 := fmt.Sprintf("key_order_1_%d", time.Now().UnixNano())
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	req1.Header.Set("Authorization", "Bearer "+token)
	req1.Header.Set("Idempotency-Key", key1)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("order 1 failed: %d: %s", w1.Code, w1.Body.String())
	}
	var resp1 models.OrderResponse
	_ = json.Unmarshal(w1.Body.Bytes(), &resp1)

	// Order 2
	_, _ = cartRepo.AddItem(ctx, user.ID, item2.ID, 2)
	key2 := fmt.Sprintf("key_order_2_%d", time.Now().UnixNano())
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Idempotency-Key", key2)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("order 2 failed: %d: %s", w2.Code, w2.Body.String())
	}
	var resp2 models.OrderResponse
	_ = json.Unmarshal(w2.Body.Bytes(), &resp2)

	// Verify distinct orders
	if resp1.ID == resp2.ID {
		t.Fatalf("expected distinct order IDs for different idempotency keys, got same: %s", resp1.ID)
	}

	var userOrderCount int
	_ = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM orders WHERE user_id = $1", user.ID).Scan(&userOrderCount)
	if userOrderCount != 2 {
		t.Errorf("expected 2 distinct orders for user, got %d", userOrderCount)
	}
}

// ============================================================================
// 5. PAYMENT VERIFY + WEBHOOK RACE: Simultaneous Verification & Webhook
// ============================================================================
func TestConcurrency_PaymentVerify_Webhook_Race_Strict(t *testing.T) {
	testKeySecret := "rzp_test_campusbite_mock_secret"
	testWebhookSecret := "rzp_test_campusbite_mock_webhook_secret"
	rzpService := service.NewRazorpayService("rzp_test_campusbite_mock_key", testKeySecret, testWebhookSecret)

	db, tokenService := func() (*database.DB, *auth.TokenService) {
		_, db, ts := setupIntegrationApp(t)
		return db, ts
	}()
	defer db.Close()

	router := routes.SetupRouter(db, tokenService, rzpService, cache.NewNoOpCache())

	_, studentToken, order := createTestOrder(t, router, db, tokenService)

	// 1. Create payment order
	reqPay := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", order.ID), nil)
	reqPay.Header.Set("Authorization", "Bearer "+studentToken)
	wPay := httptest.NewRecorder()
	router.ServeHTTP(wPay, reqPay)
	if wPay.Code != http.StatusOK {
		t.Fatalf("failed to create payment order: %d: %s", wPay.Code, wPay.Body.String())
	}

	var createResp models.CreatePaymentOrderResponse
	_ = json.Unmarshal(wPay.Body.Bytes(), &createResp)

	providerOrderID := createResp.RazorpayOrderID
	providerPaymentID := fmt.Sprintf("pay_race_strict_%d", time.Now().UnixNano())

	// Prepare verify payload
	clientSig := rzpService.ComputePaymentSignature(providerOrderID, providerPaymentID)
	verifyReqBody, _ := json.Marshal(models.VerifyPaymentRequest{
		RazorpayOrderID:   providerOrderID,
		RazorpayPaymentID: providerPaymentID,
		RazorpaySignature: clientSig,
	})

	// Prepare webhook payload
	webhookPayload := models.RazorpayWebhookPayload{
		Entity:    "event",
		AccountID: "acc_race_strict",
		Event:     "payment.captured",
		Payload: models.RazorpayWebhookData{
			Payment: &models.RazorpayPaymentEntityWrapper{
				Entity: models.RazorpayPaymentEntity{
					ID:      providerPaymentID,
					OrderID: providerOrderID,
					Amount:  createResp.Amount,
					Status:  "captured",
				},
			},
		},
		CreatedAt: time.Now().Unix(),
	}
	webhookBytes, _ := json.Marshal(webhookPayload)
	webhookSig := rzpService.ComputeWebhookSignature(webhookBytes)

	// Simultaneous race
	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	var codeVerify, codeWebhook int

	go func() {
		defer wg.Done()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/payments/verify", bytes.NewBuffer(verifyReqBody))
		r.Header.Set("Authorization", "Bearer "+studentToken)
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		<-startBarrier
		router.ServeHTTP(rec, r)
		codeVerify = rec.Code
	}()

	go func() {
		defer wg.Done()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/payments/webhook", bytes.NewBuffer(webhookBytes))
		r.Header.Set("X-Razorpay-Signature", webhookSig)
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		<-startBarrier
		router.ServeHTTP(rec, r)
		codeWebhook = rec.Code
	}()

	close(startBarrier)
	wg.Wait()

	if codeVerify != http.StatusOK {
		t.Errorf("expected verify status 200 OK, got %d", codeVerify)
	}
	if codeWebhook != http.StatusOK {
		t.Errorf("expected webhook status 200 OK, got %d", codeWebhook)
	}

	ctx := context.Background()

	// Invariant 1: Order status is CONFIRMED
	orderRepo := repository.NewOrderRepository(db)
	finalOrder, err := orderRepo.GetByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("failed to retrieve order: %v", err)
	}
	if finalOrder.Status != models.OrderStatusConfirmed {
		t.Errorf("expected order status CONFIRMED, got %s", finalOrder.Status)
	}

	// Invariant 2: Payment status is SUCCESS
	paymentRepo := repository.NewPaymentRepository(db, orderRepo)
	finalPayment, err := paymentRepo.GetPaymentByOrderID(ctx, order.ID)
	if err != nil {
		t.Fatalf("failed to retrieve payment: %v", err)
	}
	if finalPayment.Status != models.PaymentStatusSuccess {
		t.Errorf("expected payment status SUCCESS, got %s", finalPayment.Status)
	}

	// Invariant 3: Active reservations are CONSUMED
	for _, res := range finalOrder.Reservations {
		if res.Status != models.ReservationStatusConsumed {
			t.Errorf("expected reservation status CONSUMED, got %s", res.Status)
		}
	}

	// Invariant 4: No double stock deduction in inventory table
	invRepo := repository.NewInventoryRepository(db)
	inv, _ := invRepo.GetByMenuItemID(ctx, order.Items[0].MenuItemID)
	// Initial stock was 20, order had 2 units -> stock must be exactly 18
	if inv.Quantity != 18 {
		t.Errorf("expected stock to remain 18 without double deduction, got %d", inv.Quantity)
	}
}

// ============================================================================
// 6. PAYMENT FAILURE / SUCCESS IDEMPOTENCY
// ============================================================================
func TestPayment_StateTransitions_Idempotency(t *testing.T) {
	testKeySecret := "rzp_test_campusbite_mock_secret"
	testWebhookSecret := "rzp_test_campusbite_mock_webhook_secret"
	rzpService := service.NewRazorpayService("rzp_test_campusbite_mock_key", testKeySecret, testWebhookSecret)

	db, tokenService := func() (*database.DB, *auth.TokenService) {
		_, db, ts := setupIntegrationApp(t)
		return db, ts
	}()
	defer db.Close()

	router := routes.SetupRouter(db, tokenService, rzpService, cache.NewNoOpCache())

	// Part A: Duplicate Payment Success Replay
	_, studentTokenA, orderA := createTestOrder(t, router, db, tokenService)
	reqA := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", orderA.ID), nil)
	reqA.Header.Set("Authorization", "Bearer "+studentTokenA)
	wA := httptest.NewRecorder()
	router.ServeHTTP(wA, reqA)
	var respA models.CreatePaymentOrderResponse
	_ = json.Unmarshal(wA.Body.Bytes(), &respA)

	validSigA := rzpService.ComputePaymentSignature(respA.RazorpayOrderID, "pay_success_idem_1")
	verifyBodyA, _ := json.Marshal(models.VerifyPaymentRequest{
		RazorpayOrderID:   respA.RazorpayOrderID,
		RazorpayPaymentID: "pay_success_idem_1",
		RazorpaySignature: validSigA,
	})

	// Verify 1
	r1 := httptest.NewRequest(http.MethodPost, "/api/v1/payments/verify", bytes.NewBuffer(verifyBodyA))
	r1.Header.Set("Authorization", "Bearer "+studentTokenA)
	r1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first verification failed: %d: %s", w1.Code, w1.Body.String())
	}

	// Verify 2 (replay)
	r2 := httptest.NewRequest(http.MethodPost, "/api/v1/payments/verify", bytes.NewBuffer(verifyBodyA))
	r2.Header.Set("Authorization", "Bearer "+studentTokenA)
	r2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("replayed verification failed: %d: %s", w2.Code, w2.Body.String())
	}

	// Part B: Duplicate Payment Failure Replay
	_, studentTokenB, orderB := createTestOrder(t, router, db, tokenService)
	reqB := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/payment", orderB.ID), nil)
	reqB.Header.Set("Authorization", "Bearer "+studentTokenB)
	wB := httptest.NewRecorder()
	router.ServeHTTP(wB, reqB)
	var respB models.CreatePaymentOrderResponse
	_ = json.Unmarshal(wB.Body.Bytes(), &respB)

	webhookFailPayload := models.RazorpayWebhookPayload{
		Entity:    "event",
		AccountID: "acc_fail_idem",
		Event:     "payment.failed",
		Payload: models.RazorpayWebhookData{
			Payment: &models.RazorpayPaymentEntityWrapper{
				Entity: models.RazorpayPaymentEntity{
					ID:          "pay_fail_idem_1",
					OrderID:     respB.RazorpayOrderID,
					Amount:      respB.Amount,
					Status:      "failed",
					ErrorReason: "card_declined",
				},
			},
		},
		CreatedAt: time.Now().Unix(),
	}
	webhookFailBytes, _ := json.Marshal(webhookFailPayload)
	webhookFailSig := rzpService.ComputeWebhookSignature(webhookFailBytes)

	// Send failure webhook 1
	rF1 := httptest.NewRequest(http.MethodPost, "/api/v1/payments/webhook", bytes.NewBuffer(webhookFailBytes))
	rF1.Header.Set("X-Razorpay-Signature", webhookFailSig)
	rF1.Header.Set("Content-Type", "application/json")
	wF1 := httptest.NewRecorder()
	router.ServeHTTP(wF1, rF1)
	if wF1.Code != http.StatusOK {
		t.Fatalf("first failure webhook failed: %d: %s", wF1.Code, wF1.Body.String())
	}

	// Send failure webhook 2 (replay)
	rF2 := httptest.NewRequest(http.MethodPost, "/api/v1/payments/webhook", bytes.NewBuffer(webhookFailBytes))
	rF2.Header.Set("X-Razorpay-Signature", webhookFailSig)
	rF2.Header.Set("Content-Type", "application/json")
	wF2 := httptest.NewRecorder()
	router.ServeHTTP(wF2, rF2)
	if wF2.Code != http.StatusOK {
		t.Fatalf("duplicate failure webhook failed: %d: %s", wF2.Code, wF2.Body.String())
	}

	// Invariant: Stock must be restored exactly once (20 total), not 22 (double restoration)
	ctx := context.Background()
	invRepo := repository.NewInventoryRepository(db)
	invB, _ := invRepo.GetByMenuItemID(ctx, orderB.Items[0].MenuItemID)
	if invB.Quantity != 20 {
		t.Errorf("expected inventory to be exactly 20 after replayed failure, got %d", invB.Quantity)
	}
}

// ============================================================================
// 7. RESERVATION EXPIRY CONCURRENCY: Worker Race
// ============================================================================
func TestConcurrency_ReservationExpiry_WorkerRace(t *testing.T) {
	_, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	ctx := context.Background()
	initialStock := 25
	orderQty := 5

	user, _ := createRealTestUser(t, db, tokenService, models.RoleStudent)
	menuRepo := repository.NewMenuRepository(db)
	cartRepo := repository.NewCartRepository(db)
	orderRepo := repository.NewOrderRepository(db)

	item, err := menuRepo.Create(ctx, fmt.Sprintf("ExpRaceBurger_%d", time.Now().UnixNano()), "Worker race burger", 120.0, "", true, initialStock)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}

	_, _ = cartRepo.AddItem(ctx, user.ID, item.ID, orderQty)
	idempKey := fmt.Sprintf("idemp_worker_race_%d", time.Now().UnixNano())
	order, err := orderRepo.CreateFromCart(ctx, user.ID, idempKey, "")
	if err != nil {
		t.Fatalf("failed to create order: %v", err)
	}

	// Stock is now initialStock - orderQty = 20
	var resID string
	err = db.Pool.QueryRow(ctx, "SELECT id FROM inventory_reservations WHERE order_id = $1", order.ID).Scan(&resID)
	if err != nil {
		t.Fatalf("failed to find reservation id: %v", err)
	}

	// Backdate expires_at to past
	_, err = db.Pool.Exec(ctx, "UPDATE inventory_reservations SET expires_at = NOW() - INTERVAL '5 minutes' WHERE id = $1", resID)
	if err != nil {
		t.Fatalf("failed to backdate reservation: %v", err)
	}

	// 10 concurrent worker goroutines attempt to expire this same reservation
	concurrency := 10
	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(concurrency)

	var handledCount int64
	resWorker := worker.NewReservationWorker(db, 30*time.Second)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			<-startBarrier
			handled, expErr := resWorker.ExpireSingleReservation(ctx, resID)
			if expErr == nil && handled {
				atomic.AddInt64(&handledCount, 1)
			}
		}()
	}

	close(startBarrier)
	wg.Wait()

	// Exactly 1 worker goroutine succeeds
	if handledCount != 1 {
		t.Errorf("expected exactly 1 worker goroutine to successfully claim and expire reservation, got %d", handledCount)
	}

	// Final stock must be exactly restored to initialStock (25)
	invRepo := repository.NewInventoryRepository(db)
	inv, _ := invRepo.GetByMenuItemID(ctx, item.ID)
	if inv.Quantity != initialStock {
		t.Errorf("expected inventory restored to %d, got %d (double restoration occurred!)", initialStock, inv.Quantity)
	}
}

// ============================================================================
// 8. REDIS FAILURE TEST: Fail-Open Rate Limiter & PostgreSQL Fallback
// ============================================================================
func TestFailure_Redis_Down_FailOpen_And_PostgresFallback(t *testing.T) {
	// Setup app with NoOpCache simulating Redis outage/bypass
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	ctx := context.Background()
	_, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	menuRepo := repository.NewMenuRepository(db)
	item, err := menuRepo.Create(ctx, fmt.Sprintf("FallbackItem_%d", time.Now().UnixNano()), "Fallback food", 100.0, "", true, 10)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}

	// 1. Menu fetch works via PostgreSQL directly (fallback when Redis is down)
	reqMenu := httptest.NewRequest(http.MethodGet, "/api/v1/menu", nil)
	wMenu := httptest.NewRecorder()
	router.ServeHTTP(wMenu, reqMenu)
	if wMenu.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for menu fetch under NoOpCache, got %d: %s", wMenu.Code, wMenu.Body.String())
	}

	// 2. High-frequency requests do not fail (Rate limiter fails open gracefully)
	for i := 0; i < 50; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/menu", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected rate limiter to fail open without Redis error, got %d on request %d", w.Code, i)
		}
	}

	// 3. Order creation and payment proceed with PostgreSQL single source of truth
	cartRepo := repository.NewCartRepository(db)
	_, _ = cartRepo.AddItem(ctx, item.ID, item.ID, 1)

	reqOrder := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	reqOrder.Header.Set("Authorization", "Bearer "+studentToken)
	reqOrder.Header.Set("Idempotency-Key", fmt.Sprintf("fallback_key_%d", time.Now().UnixNano()))
	wOrder := httptest.NewRecorder()
	router.ServeHTTP(wOrder, reqOrder)
	if wOrder.Code != http.StatusCreated && wOrder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected order status: %d", wOrder.Code)
	}
}

// ============================================================================
// 9. WEBSOCKET ISOLATION & RECONNECT TEST
// ============================================================================
func TestConcurrency_WebSocket_Isolation_And_Reconnect(t *testing.T) {
	server, db, tokenService, wsHub := setupWSTestServer(t)
	defer server.Close()
	defer db.Close()

	ctx := context.Background()
	userA, tokenA := createRealTestUser(t, db, tokenService, models.RoleStudent)
	userB, tokenB := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_ = userB
	adminUser, adminToken := createRealTestUser(t, db, tokenService, models.RoleAdmin)
	_ = adminUser

	// Connect Client A
	connA, _, err := dialWS(t, server.URL, tokenA, false)
	if err != nil {
		t.Fatalf("failed to connect User A WS: %v", err)
	}
	defer connA.Close()

	// Connect Client B
	connB, _, err := dialWS(t, server.URL, tokenB, false)
	if err != nil {
		t.Fatalf("failed to connect User B WS: %v", err)
	}
	defer connB.Close()

	// Connect Admin Client
	connAdmin, _, err := dialWS(t, server.URL, adminToken, false)
	if err != nil {
		t.Fatalf("failed to connect Admin WS: %v", err)
	}
	defer connAdmin.Close()

	time.Sleep(100 * time.Millisecond) // allow hub registrations

	// 1. Send targeted notification to User A only
	eventA := ws.Event{
		Type:      ws.EventNotification,
		Payload:   map[string]interface{}{"title": "Order Ready", "message": "Pickup at counter 1"},
		Timestamp: time.Now(),
	}
	wsHub.SendToUser(userA.ID, eventA)

	// User A must receive it
	_ = connA.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msgA, err := connA.ReadMessage()
	if err != nil {
		t.Fatalf("User A failed to receive targeted event: %v", err)
	}
	if !bytes.Contains(msgA, []byte("Order Ready")) {
		t.Errorf("expected User A to receive 'Order Ready', got: %s", string(msgA))
	}

	// User B must NOT receive User A's notification (isolation check)
	_ = connB.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	_, _, err = connB.ReadMessage()
	if err == nil {
		t.Fatalf("SECURITY VIOLATION: User B received User A's private notification!")
	}

	// 2. Broadcast Admin event
	adminEvent := ws.Event{
		Type:      ws.EventNewOrder,
		OrderID:   "test-broadcast-order",
		Timestamp: time.Now(),
	}
	wsHub.BroadcastToAdmins(adminEvent)

	// Admin receives it
	_ = connAdmin.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msgAdmin, err := connAdmin.ReadMessage()
	if err != nil {
		t.Fatalf("Admin failed to receive broadcast event: %v", err)
	}
	if !bytes.Contains(msgAdmin, []byte("test-broadcast-order")) {
		t.Errorf("expected Admin to receive 'test-broadcast-order', got: %s", string(msgAdmin))
	}

	// 3. Disconnect & REST Recovery Scenario
	_ = connA.Close() // Simulate network disconnect

	// While disconnected, insert a persistent notification into PostgreSQL
	notifRepo := repository.NewNotificationRepository(db)
	_, err = notifRepo.Create(ctx, userA.ID, "MISSED_UPDATE", "Order Completed", "Your meal was picked up", nil)
	if err != nil {
		t.Fatalf("failed to insert missed notification: %v", err)
	}

	// Client reconnects and retrieves missed notifications via REST API
	reqNotif := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	reqNotif.Header.Set("Authorization", "Bearer "+tokenA)
	wNotif := httptest.NewRecorder()
	// Use router directly
	router := routes.SetupRouter(db, tokenService, service.NewRazorpayService("k", "s", "w"), cache.NewNoOpCache(), wsHub)
	router.ServeHTTP(wNotif, reqNotif)

	if wNotif.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for notification recovery via REST, got %d: %s", wNotif.Code, wNotif.Body.String())
	}
	if !bytes.Contains(wNotif.Body.Bytes(), []byte("Order Completed")) {
		t.Errorf("expected restored notifications to contain 'Order Completed', got: %s", wNotif.Body.String())
	}
}
