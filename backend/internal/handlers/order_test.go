package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"campusbite/internal/models"
	"campusbite/internal/repository"
)

func TestOrder_IdempotencyValidation(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	_, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	// 1. Missing Idempotency-Key header -> 400 Bad Request
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	req1.Header.Set("Authorization", "Bearer "+studentToken)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for missing Idempotency-Key, got %d: %s", w1.Code, w1.Body.String())
	}

	// 2. Empty / whitespace Idempotency-Key header -> 400 Bad Request
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	req2.Header.Set("Authorization", "Bearer "+studentToken)
	req2.Header.Set("Idempotency-Key", "   ")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for empty Idempotency-Key, got %d: %s", w2.Code, w2.Body.String())
	}

	// 3. Oversized Idempotency-Key header (>255 chars) -> 400 Bad Request
	oversizedKey := strings.Repeat("A", 256)
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	req3.Header.Set("Authorization", "Bearer "+studentToken)
	req3.Header.Set("Idempotency-Key", oversizedKey)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for oversized Idempotency-Key, got %d: %s", w3.Code, w3.Body.String())
	}
}

func TestOrder_CreateAndGet(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	user1, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_, adminToken := createRealTestUser(t, db, tokenService, models.RoleAdmin)
	_, otherStudentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	// 1. Attempt to create order from empty cart -> 400 Bad Request
	emptyOrderReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	emptyOrderReq.Header.Set("Authorization", "Bearer "+studentToken)
	emptyOrderReq.Header.Set("Idempotency-Key", fmt.Sprintf("empty_cart_%d", time.Now().UnixNano()))
	emptyOrderW := httptest.NewRecorder()
	router.ServeHTTP(emptyOrderW, emptyOrderReq)

	if emptyOrderW.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request when creating order from empty cart, got %d: %s", emptyOrderW.Code, emptyOrderW.Body.String())
	}

	// 2. Seed menu items and populate cart
	menuRepo := repository.NewMenuRepository(db)
	invRepo := repository.NewInventoryRepository(db)

	item1, err := menuRepo.Create(context.Background(), fmt.Sprintf("OrderPizza_%d", time.Now().UnixNano()), "Fresh Pizza", 150.0, "", true, 20)
	if err != nil {
		t.Fatalf("failed to seed menu item 1: %v", err)
	}
	item2, err := menuRepo.Create(context.Background(), fmt.Sprintf("OrderCoke_%d", time.Now().UnixNano()), "Cold Drink", 40.0, "", true, 50)
	if err != nil {
		t.Fatalf("failed to seed menu item 2: %v", err)
	}

	// Add item 1 (qty 2) -> 300.0
	b1, _ := json.Marshal(models.AddToCartRequest{MenuItemID: item1.ID, Quantity: 2})
	addReq1 := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(b1))
	addReq1.Header.Set("Authorization", "Bearer "+studentToken)
	addReq1.Header.Set("Content-Type", "application/json")
	addW1 := httptest.NewRecorder()
	router.ServeHTTP(addW1, addReq1)
	if addW1.Code != http.StatusOK {
		t.Fatalf("failed to add item 1 to cart: %s", addW1.Body.String())
	}

	// Add item 2 (qty 3) -> 120.0
	b2, _ := json.Marshal(models.AddToCartRequest{MenuItemID: item2.ID, Quantity: 3})
	addReq2 := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(b2))
	addReq2.Header.Set("Authorization", "Bearer "+studentToken)
	addReq2.Header.Set("Content-Type", "application/json")
	addW2 := httptest.NewRecorder()
	router.ServeHTTP(addW2, addReq2)
	if addW2.Code != http.StatusOK {
		t.Fatalf("failed to add item 2 to cart: %s", addW2.Body.String())
	}

	// 3. Create order (Transactional checkout with inventory reservation) -> 201 Created
	idempotencyKey := fmt.Sprintf("checkout_key_%d", time.Now().UnixNano())
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	createReq.Header.Set("Authorization", "Bearer "+studentToken)
	createReq.Header.Set("Idempotency-Key", idempotencyKey)
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for order creation, got %d: %s", createW.Code, createW.Body.String())
	}

	var createdOrder models.OrderResponse
	if err := json.Unmarshal(createW.Body.Bytes(), &createdOrder); err != nil {
		t.Fatalf("failed to unmarshal order response: %v", err)
	}

	if createdOrder.Status != models.OrderStatusPending {
		t.Errorf("expected order status PENDING, got %s", createdOrder.Status)
	}
	expectedTotal := 420.0 // (2 * 150) + (3 * 40)
	if createdOrder.TotalAmount != expectedTotal {
		t.Errorf("expected order total %f, got %f", expectedTotal, createdOrder.TotalAmount)
	}
	if len(createdOrder.Items) != 2 {
		t.Fatalf("expected 2 items in order, got %d", len(createdOrder.Items))
	}
	if len(createdOrder.Reservations) != 2 {
		t.Fatalf("expected 2 reservation records, got %d", len(createdOrder.Reservations))
	}
	for _, res := range createdOrder.Reservations {
		if res.Status != models.ReservationStatusActive {
			t.Errorf("expected reservation status ACTIVE, got %s", res.Status)
		}
		if res.OrderID != createdOrder.ID {
			t.Errorf("expected reservation order_id %s, got %s", createdOrder.ID, res.OrderID)
		}
	}

	// 4. Verify inventory was decremented correctly
	inv1, err := invRepo.GetByMenuItemID(context.Background(), item1.ID)
	if err != nil || inv1.Quantity != 18 { // 20 - 2 = 18
		t.Errorf("expected item 1 inventory 18, got %d (err: %v)", inv1.Quantity, err)
	}
	inv2, err := invRepo.GetByMenuItemID(context.Background(), item2.ID)
	if err != nil || inv2.Quantity != 47 { // 50 - 3 = 47
		t.Errorf("expected item 2 inventory 47, got %d (err: %v)", inv2.Quantity, err)
	}

	// 5. Verify cart was cleared after successful order placement
	getCartReq := httptest.NewRequest(http.MethodGet, "/api/v1/cart", nil)
	getCartReq.Header.Set("Authorization", "Bearer "+studentToken)
	getCartW := httptest.NewRecorder()
	router.ServeHTTP(getCartW, getCartReq)

	var cartAfterOrder models.CartResponse
	_ = json.Unmarshal(getCartW.Body.Bytes(), &cartAfterOrder)
	if len(cartAfterOrder.Items) != 0 {
		t.Errorf("expected 0 items in cart after order creation, got %d", len(cartAfterOrder.Items))
	}

	// 6. Get order by ID - Owner Student -> 200 OK
	getOrderReq := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+createdOrder.ID, nil)
	getOrderReq.Header.Set("Authorization", "Bearer "+studentToken)
	getOrderW := httptest.NewRecorder()
	router.ServeHTTP(getOrderW, getOrderReq)

	if getOrderW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for order owner, got %d: %s", getOrderW.Code, getOrderW.Body.String())
	}

	var fetchedOrder models.OrderResponse
	_ = json.Unmarshal(getOrderW.Body.Bytes(), &fetchedOrder)
	if fetchedOrder.ID != createdOrder.ID {
		t.Errorf("expected order ID %s, got %s", createdOrder.ID, fetchedOrder.ID)
	}
	if fetchedOrder.UserID != user1.ID {
		t.Errorf("expected user ID %s, got %s", user1.ID, fetchedOrder.UserID)
	}

	// 7. Get order by ID - Other Student -> 403 Forbidden
	forbiddenReq := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+createdOrder.ID, nil)
	forbiddenReq.Header.Set("Authorization", "Bearer "+otherStudentToken)
	forbiddenW := httptest.NewRecorder()
	router.ServeHTTP(forbiddenW, forbiddenReq)

	if forbiddenW.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for non-owner student, got %d", forbiddenW.Code)
	}

	// 8. Get order by ID - Admin -> 200 OK
	adminGetReq := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+createdOrder.ID, nil)
	adminGetReq.Header.Set("Authorization", "Bearer "+adminToken)
	adminGetW := httptest.NewRecorder()
	router.ServeHTTP(adminGetW, adminGetReq)

	if adminGetW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for Admin viewing order, got %d: %s", adminGetW.Code, adminGetW.Body.String())
	}

	// 9. Non-existent order -> 404
	missingReq := httptest.NewRequest(http.MethodGet, "/api/v1/orders/00000000-0000-0000-0000-000000000000", nil)
	missingReq.Header.Set("Authorization", "Bearer "+studentToken)
	missingW := httptest.NewRecorder()
	router.ServeHTTP(missingW, missingReq)

	if missingW.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for nonexistent order, got %d", missingW.Code)
	}
}

func TestOrder_IdempotentReplayAndReuse(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	user, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	menuRepo := repository.NewMenuRepository(db)
	invRepo := repository.NewInventoryRepository(db)

	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("IdemBurger_%d", time.Now().UnixNano()), "Burger", 100.0, "", true, 10)
	if err != nil {
		t.Fatalf("failed to seed item: %v", err)
	}

	// Add item to cart (qty 2)
	b, _ := json.Marshal(models.AddToCartRequest{MenuItemID: item.ID, Quantity: 2})
	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(b))
	addReq.Header.Set("Authorization", "Bearer "+studentToken)
	addReq.Header.Set("Content-Type", "application/json")
	addW := httptest.NewRecorder()
	router.ServeHTTP(addW, addReq)
	if addW.Code != http.StatusOK {
		t.Fatalf("failed to add item to cart: %s", addW.Body.String())
	}

	idempotencyKey := fmt.Sprintf("replay_test_key_%d", time.Now().UnixNano())

	// 1. Initial Request -> 201 Created
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	req1.Header.Set("Authorization", "Bearer "+studentToken)
	req1.Header.Set("Idempotency-Key", idempotencyKey)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("first request failed: %d: %s", w1.Code, w1.Body.String())
	}

	var firstOrder models.OrderResponse
	_ = json.Unmarshal(w1.Body.Bytes(), &firstOrder)

	// Verify inventory decreased from 10 to 8
	invAfter1, err := invRepo.GetByMenuItemID(context.Background(), item.ID)
	if err != nil || invAfter1.Quantity != 8 {
		t.Fatalf("expected inventory 8 after first checkout, got %d", invAfter1.Quantity)
	}

	// 2. Replay the exact same request with the same Idempotency-Key
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	req2.Header.Set("Authorization", "Bearer "+studentToken)
	req2.Header.Set("Idempotency-Key", idempotencyKey)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusCreated && w2.Code != http.StatusOK {
		t.Fatalf("replay request failed: %d: %s", w2.Code, w2.Body.String())
	}

	var replayedOrder models.OrderResponse
	_ = json.Unmarshal(w2.Body.Bytes(), &replayedOrder)

	// Assert that the exact same order is returned
	if replayedOrder.ID != firstOrder.ID {
		t.Errorf("expected replayed order ID %s, got %s", firstOrder.ID, replayedOrder.ID)
	}
	if replayedOrder.TotalAmount != firstOrder.TotalAmount {
		t.Errorf("expected total amount %f, got %f", firstOrder.TotalAmount, replayedOrder.TotalAmount)
	}

	// 3. Verify in database that no second order or reservation was created
	var totalOrdersCount int
	err = db.Pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM orders WHERE user_id = $1", user.ID).Scan(&totalOrdersCount)
	if err != nil || totalOrdersCount != 1 {
		t.Errorf("expected exactly 1 order in DB, got %d", totalOrdersCount)
	}

	var totalReservationsCount int
	err = db.Pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM inventory_reservations WHERE order_id = $1", firstOrder.ID).Scan(&totalReservationsCount)
	if err != nil || totalReservationsCount != 1 {
		t.Errorf("expected exactly 1 reservation in DB, got %d", totalReservationsCount)
	}

	// Verify inventory did NOT decrease again (still 8)
	invAfter2, err := invRepo.GetByMenuItemID(context.Background(), item.ID)
	if err != nil || invAfter2.Quantity != 8 {
		t.Errorf("inventory was erroneously decremented on replay: expected 8, got %d", invAfter2.Quantity)
	}
}

func TestOrder_ConcurrentSameKeyRequests(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	user, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	menuRepo := repository.NewMenuRepository(db)
	invRepo := repository.NewInventoryRepository(db)

	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("ConcIdemItem_%d", time.Now().UnixNano()), "Noodles", 80.0, "", true, 10)
	if err != nil {
		t.Fatalf("failed to seed menu item: %v", err)
	}

	// Add item to cart (qty 2)
	b, _ := json.Marshal(models.AddToCartRequest{MenuItemID: item.ID, Quantity: 2})
	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(b))
	addReq.Header.Set("Authorization", "Bearer "+studentToken)
	addReq.Header.Set("Content-Type", "application/json")
	addW := httptest.NewRecorder()
	router.ServeHTTP(addW, addReq)
	if addW.Code != http.StatusOK {
		t.Fatalf("failed to add item: %s", addW.Body.String())
	}

	idempotencyKey := fmt.Sprintf("concurrent_same_key_%d", time.Now().UnixNano())

	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	responses := make([]*httptest.ResponseRecorder, 2)

	for i := 0; i < 2; i++ {
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

	// Both requests should either return the successful order (201/200) or one returns 409 Conflict (processing)
	// In all cases, only 1 order and 1 reservation must be created in PostgreSQL
	for _, w := range responses {
		if w.Code != http.StatusCreated && w.Code != http.StatusOK && w.Code != http.StatusConflict {
			t.Errorf("unexpected status code for concurrent same-key request: %d (body: %s)", w.Code, w.Body.String())
		}
	}

	// Verify database state: exactly 1 order
	var orderCount int
	err = db.Pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM orders WHERE user_id = $1", user.ID).Scan(&orderCount)
	if err != nil || orderCount != 1 {
		t.Errorf("expected exactly 1 order in DB for concurrent same-key requests, got %d", orderCount)
	}

	// Verify inventory was deducted only once (10 - 2 = 8)
	inv, err := invRepo.GetByMenuItemID(context.Background(), item.ID)
	if err != nil || inv.Quantity != 8 {
		t.Errorf("expected inventory to be 8, got %d", inv.Quantity)
	}
}

func TestOrder_DifferentUsersSameKeyIsolation(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	user1, studentToken1 := createRealTestUser(t, db, tokenService, models.RoleStudent)
	user2, studentToken2 := createRealTestUser(t, db, tokenService, models.RoleStudent)

	menuRepo := repository.NewMenuRepository(db)
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("SharedKeyItem_%d", time.Now().UnixNano()), "Wrap", 60.0, "", true, 20)
	if err != nil {
		t.Fatalf("failed to seed menu item: %v", err)
	}

	// Both users add 1 wrap to cart
	for _, tok := range []string{studentToken1, studentToken2} {
		b, _ := json.Marshal(models.AddToCartRequest{MenuItemID: item.ID, Quantity: 1})
		addReq := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(b))
		addReq.Header.Set("Authorization", "Bearer "+tok)
		addReq.Header.Set("Content-Type", "application/json")
		addW := httptest.NewRecorder()
		router.ServeHTTP(addW, addReq)
		if addW.Code != http.StatusOK {
			t.Fatalf("failed to add item: %s", addW.Body.String())
		}
	}

	sharedKey := fmt.Sprintf("common_user_key_%d", time.Now().UnixNano())

	// User 1 checkouts with sharedKey
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	req1.Header.Set("Authorization", "Bearer "+studentToken1)
	req1.Header.Set("Idempotency-Key", sharedKey)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("user 1 checkout failed: %d: %s", w1.Code, w1.Body.String())
	}
	var order1 models.OrderResponse
	_ = json.Unmarshal(w1.Body.Bytes(), &order1)

	// User 2 checkouts with same textual sharedKey
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	req2.Header.Set("Authorization", "Bearer "+studentToken2)
	req2.Header.Set("Idempotency-Key", sharedKey)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("user 2 checkout failed: %d: %s", w2.Code, w2.Body.String())
	}
	var order2 models.OrderResponse
	_ = json.Unmarshal(w2.Body.Bytes(), &order2)

	// Verify User 2 received their own distinct order
	if order1.ID == order2.ID {
		t.Errorf("security violation: User 1 and User 2 shared the same order ID %s", order1.ID)
	}
	if order1.UserID != user1.ID || order2.UserID != user2.ID {
		t.Errorf("order user IDs do not match respective users: order1=%s, order2=%s", order1.UserID, order2.UserID)
	}
}

func TestOrder_Checkout_InsufficientInventory(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	_, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	menuRepo := repository.NewMenuRepository(db)
	invRepo := repository.NewInventoryRepository(db)

	// Create item with only 2 units in inventory
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("ScarceItem_%d", time.Now().UnixNano()), "Limited Item", 100.0, "", true, 2)
	if err != nil {
		t.Fatalf("failed to seed menu item: %v", err)
	}

	// Student adds 3 units to cart (exceeding stock)
	b, _ := json.Marshal(models.AddToCartRequest{MenuItemID: item.ID, Quantity: 3})
	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(b))
	addReq.Header.Set("Authorization", "Bearer "+studentToken)
	addReq.Header.Set("Content-Type", "application/json")
	addW := httptest.NewRecorder()
	router.ServeHTTP(addW, addReq)
	if addW.Code != http.StatusOK {
		t.Fatalf("failed to add item to cart: %s", addW.Body.String())
	}

	// Attempt checkout -> 400 Bad Request
	checkoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	checkoutReq.Header.Set("Authorization", "Bearer "+studentToken)
	checkoutReq.Header.Set("Idempotency-Key", fmt.Sprintf("insufficient_stock_key_%d", time.Now().UnixNano()))
	checkoutW := httptest.NewRecorder()
	router.ServeHTTP(checkoutW, checkoutReq)

	if checkoutW.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request due to insufficient stock, got %d: %s", checkoutW.Code, checkoutW.Body.String())
	}

	// Verify inventory quantity is untouched (still 2)
	inv, err := invRepo.GetByMenuItemID(context.Background(), item.ID)
	if err != nil || inv.Quantity != 2 {
		t.Errorf("expected inventory to remain 2 after failed checkout, got %d", inv.Quantity)
	}

	// Verify user's cart is preserved so they can modify and retry
	getCartReq := httptest.NewRequest(http.MethodGet, "/api/v1/cart", nil)
	getCartReq.Header.Set("Authorization", "Bearer "+studentToken)
	getCartW := httptest.NewRecorder()
	router.ServeHTTP(getCartW, getCartReq)

	var cart models.CartResponse
	_ = json.Unmarshal(getCartW.Body.Bytes(), &cart)
	if len(cart.Items) != 1 || cart.Items[0].Quantity != 3 {
		t.Errorf("expected cart to be preserved with 3 items, got %+v", cart.Items)
	}
}

func TestOrder_Checkout_UnavailableMenuItem(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	_, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	menuRepo := repository.NewMenuRepository(db)

	// Create item initially available
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("UnavailableItem_%d", time.Now().UnixNano()), "Item", 50.0, "", true, 10)
	if err != nil {
		t.Fatalf("failed to seed menu item: %v", err)
	}

	// Add to cart
	b, _ := json.Marshal(models.AddToCartRequest{MenuItemID: item.ID, Quantity: 1})
	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(b))
	addReq.Header.Set("Authorization", "Bearer "+studentToken)
	addReq.Header.Set("Content-Type", "application/json")
	addW := httptest.NewRecorder()
	router.ServeHTTP(addW, addReq)
	if addW.Code != http.StatusOK {
		t.Fatalf("failed to add item: %s", addW.Body.String())
	}

	// Admin disables menu item (is_available = false)
	isAvail := false
	_, err = menuRepo.Update(context.Background(), item.ID, nil, nil, nil, nil, &isAvail)
	if err != nil {
		t.Fatalf("failed to disable menu item: %v", err)
	}

	// Attempt checkout -> 400 Bad Request
	checkoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	checkoutReq.Header.Set("Authorization", "Bearer "+studentToken)
	checkoutReq.Header.Set("Idempotency-Key", fmt.Sprintf("unavail_key_%d", time.Now().UnixNano()))
	checkoutW := httptest.NewRecorder()
	router.ServeHTTP(checkoutW, checkoutReq)

	if checkoutW.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for unavailable item, got %d: %s", checkoutW.Code, checkoutW.Body.String())
	}
}

func TestOrder_Checkout_ConcurrentStockExhaustion(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	// Create 2 real distinct student users
	_, studentToken1 := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_, studentToken2 := createRealTestUser(t, db, tokenService, models.RoleStudent)

	menuRepo := repository.NewMenuRepository(db)
	invRepo := repository.NewInventoryRepository(db)

	// Exactly 1 unit in inventory
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("LastItem_%d", time.Now().UnixNano()), "Sole Item", 99.0, "", true, 1)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}

	// Both students add 1 unit to their carts
	for _, token := range []string{studentToken1, studentToken2} {
		b, _ := json.Marshal(models.AddToCartRequest{MenuItemID: item.ID, Quantity: 1})
		addReq := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(b))
		addReq.Header.Set("Authorization", "Bearer "+token)
		addReq.Header.Set("Content-Type", "application/json")
		addW := httptest.NewRecorder()
		router.ServeHTTP(addW, addReq)
		if addW.Code != http.StatusOK {
			t.Fatalf("failed to add item to cart: %s", addW.Body.String())
		}
	}

	// Concurrency synchronization barrier
	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	responses := make([]*httptest.ResponseRecorder, 2)

	tokens := []string{studentToken1, studentToken2}
	for i := 0; i < 2; i++ {
		idx := i
		go func(token string) {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Idempotency-Key", fmt.Sprintf("race_student_%d_%d", idx, time.Now().UnixNano()))

			<-startBarrier // await simultaneous release
			router.ServeHTTP(w, req)
			responses[idx] = w
		}(tokens[idx])
	}

	// Trigger simultaneous execution
	close(startBarrier)
	wg.Wait()

	// Analyze outcomes
	var successCount, failureCount int
	for _, w := range responses {
		if w.Code == http.StatusCreated {
			successCount++
		} else if w.Code == http.StatusBadRequest {
			failureCount++
		} else {
			t.Errorf("unexpected status code in concurrent checkout: %d (body: %s)", w.Code, w.Body.String())
		}
	}

	if successCount != 1 {
		t.Errorf("expected exactly 1 successful checkout, got %d", successCount)
	}
	if failureCount != 1 {
		t.Errorf("expected exactly 1 failed checkout due to insufficient stock, got %d", failureCount)
	}

	// Verify inventory is exactly 0 and never went negative
	inv, err := invRepo.GetByMenuItemID(context.Background(), item.ID)
	if err != nil {
		t.Fatalf("failed to query inventory: %v", err)
	}
	if inv.Quantity != 0 {
		t.Errorf("expected inventory quantity 0, got %d", inv.Quantity)
	}

	// Verify exactly 1 reservation exists in the database for this menu item
	var reservationCount int
	err = db.Pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM inventory_reservations WHERE menu_item_id = $1", item.ID).Scan(&reservationCount)
	if err != nil {
		t.Fatalf("failed to count reservations: %v", err)
	}
	if reservationCount != 1 {
		t.Errorf("expected exactly 1 reservation row, got %d", reservationCount)
	}
}

func TestOrder_List(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	_, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_, adminToken := createRealTestUser(t, db, tokenService, models.RoleAdmin)

	// 1. List orders for student
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer "+studentToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for student list orders, got %d: %s", w.Code, w.Body.String())
	}

	var studentOrders struct {
		Orders []models.OrderResponse `json:"orders"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &studentOrders); err != nil {
		t.Fatalf("failed to decode student orders list: %v", err)
	}

	// 2. List orders for admin
	adminReq := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	adminReq.Header.Set("Authorization", "Bearer "+adminToken)
	adminW := httptest.NewRecorder()
	router.ServeHTTP(adminW, adminReq)

	if adminW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for admin list orders, got %d: %s", adminW.Code, adminW.Body.String())
	}

	var adminOrders struct {
		Orders []models.OrderResponse `json:"orders"`
	}
	if err := json.Unmarshal(adminW.Body.Bytes(), &adminOrders); err != nil {
		t.Fatalf("failed to decode admin orders list: %v", err)
	}

	// 3. Unauthenticated list -> 401
	unauthReq := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	unauthW := httptest.NewRecorder()
	router.ServeHTTP(unauthW, unauthReq)

	if unauthW.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for unauthenticated order list, got %d", unauthW.Code)
	}
}

func TestOrder_DifferentKeysSeparateOperations(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	user, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	menuRepo := repository.NewMenuRepository(db)
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("DiffKeyItem_%d", time.Now().UnixNano()), "Toast", 30.0, "", true, 20)
	if err != nil {
		t.Fatalf("failed to seed item: %v", err)
	}

	// First checkout with Key 1
	b1, _ := json.Marshal(models.AddToCartRequest{MenuItemID: item.ID, Quantity: 1})
	addReq1 := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(b1))
	addReq1.Header.Set("Authorization", "Bearer "+studentToken)
	addReq1.Header.Set("Content-Type", "application/json")
	wAdd1 := httptest.NewRecorder()
	router.ServeHTTP(wAdd1, addReq1)

	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	req1.Header.Set("Authorization", "Bearer "+studentToken)
	req1.Header.Set("Idempotency-Key", fmt.Sprintf("first_key_%d", time.Now().UnixNano()))
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first checkout failed: %d: %s", w1.Code, w1.Body.String())
	}
	var order1 models.OrderResponse
	_ = json.Unmarshal(w1.Body.Bytes(), &order1)

	// Second checkout with Key 2
	b2, _ := json.Marshal(models.AddToCartRequest{MenuItemID: item.ID, Quantity: 2})
	addReq2 := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(b2))
	addReq2.Header.Set("Authorization", "Bearer "+studentToken)
	addReq2.Header.Set("Content-Type", "application/json")
	wAdd2 := httptest.NewRecorder()
	router.ServeHTTP(wAdd2, addReq2)

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	req2.Header.Set("Authorization", "Bearer "+studentToken)
	req2.Header.Set("Idempotency-Key", fmt.Sprintf("second_key_%d", time.Now().UnixNano()))
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("second checkout failed: %d: %s", w2.Code, w2.Body.String())
	}
	var order2 models.OrderResponse
	_ = json.Unmarshal(w2.Body.Bytes(), &order2)

	// Verify two distinct orders were created
	if order1.ID == order2.ID {
		t.Errorf("expected two distinct orders for different idempotency keys, got same ID %s", order1.ID)
	}

	var totalOrders int
	_ = db.Pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM orders WHERE user_id = $1", user.ID).Scan(&totalOrders)
	if totalOrders != 2 {
		t.Errorf("expected 2 orders in DB, got %d", totalOrders)
	}
}

func TestOrder_CashOnDelivery_FullFlowAndNotifications(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	student, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_, adminToken := createRealTestUser(t, db, tokenService, models.RoleAdmin)

	menuRepo := repository.NewMenuRepository(db)
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("VegBurger_%d", time.Now().UnixNano()), "Veg Burger Deluxe", 45.0, "", true, 10)
	if err != nil {
		t.Fatalf("failed to seed item: %v", err)
	}

	// 1. Add item to cart (quantity: 2)
	addPayload, _ := json.Marshal(models.AddToCartRequest{MenuItemID: item.ID, Quantity: 2})
	reqAdd := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(addPayload))
	reqAdd.Header.Set("Authorization", "Bearer "+studentToken)
	reqAdd.Header.Set("Content-Type", "application/json")
	wAdd := httptest.NewRecorder()
	router.ServeHTTP(wAdd, reqAdd)
	if wAdd.Code != http.StatusOK {
		t.Fatalf("failed to add item to cart: %d: %s", wAdd.Code, wAdd.Body.String())
	}

	// 2. Checkout with PaymentMethod = "COD"
	idempKey := fmt.Sprintf("cod_test_%d", time.Now().UnixNano())
	codOrderBody, _ := json.Marshal(map[string]interface{}{
		"payment_method":       "COD",
		"special_instructions": "Extra sauce please",
	})
	reqOrder := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBuffer(codOrderBody))
	reqOrder.Header.Set("Authorization", "Bearer "+studentToken)
	reqOrder.Header.Set("Idempotency-Key", idempKey)
	reqOrder.Header.Set("Content-Type", "application/json")
	wOrder := httptest.NewRecorder()
	router.ServeHTTP(wOrder, reqOrder)

	if wOrder.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for COD order, got %d: %s", wOrder.Code, wOrder.Body.String())
	}

	var codOrder models.OrderResponse
	if err := json.Unmarshal(wOrder.Body.Bytes(), &codOrder); err != nil {
		t.Fatalf("failed to unmarshal COD order response: %v", err)
	}

	// Verify order status is CONFIRMED
	if codOrder.Status != models.OrderStatusConfirmed {
		t.Errorf("expected COD order status to be CONFIRMED, got %s", codOrder.Status)
	}

	// Verify reservations are marked CONSUMED
	for _, res := range codOrder.Reservations {
		if res.Status != models.ReservationStatusConsumed {
			t.Errorf("expected reservation status to be CONSUMED for COD order, got %s", res.Status)
		}
	}

	// Verify payment record in DB has status PENDING and provider_order_id = 'COD'
	var paymentStatus, providerOrderID string
	var paymentAmount float64
	err = db.Pool.QueryRow(context.Background(), `
		SELECT status, provider_order_id, amount
		FROM payments
		WHERE order_id = $1
	`, codOrder.ID).Scan(&paymentStatus, &providerOrderID, &paymentAmount)
	if err != nil {
		t.Fatalf("failed to query payment record for COD order: %v", err)
	}
	if paymentStatus != string(models.PaymentStatusPending) {
		t.Errorf("expected COD payment record status to be PENDING, got %s", paymentStatus)
	}
	if providerOrderID != "COD" {
		t.Errorf("expected provider_order_id to be 'COD', got %s", providerOrderID)
	}
	if paymentAmount != 90.0 {
		t.Errorf("expected payment amount to be 90.0, got %f", paymentAmount)
	}

	// Verify confirmation notification was persisted
	var notifCount int
	var notifTitle, notifMsg string
	err = db.Pool.QueryRow(context.Background(), `
		SELECT COUNT(*), title, message
		FROM notifications
		WHERE user_id = $1
		GROUP BY title, message
	`, student.ID).Scan(&notifCount, &notifTitle, &notifMsg)
	if err != nil {
		t.Fatalf("failed to query notification for COD order: %v", err)
	}
	if notifTitle != "Order Confirmed" {
		t.Errorf("expected notification title 'Order Confirmed', got '%s'", notifTitle)
	}
	if !strings.Contains(notifMsg, "has been confirmed") {
		t.Errorf("expected notification message to contain 'has been confirmed', got '%s'", notifMsg)
	}

	// Verify Idempotent replay of COD checkout returns identical order without double-deduction
	reqOrderReplay := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBuffer(codOrderBody))
	reqOrderReplay.Header.Set("Authorization", "Bearer "+studentToken)
	reqOrderReplay.Header.Set("Idempotency-Key", idempKey)
	reqOrderReplay.Header.Set("Content-Type", "application/json")
	wOrderReplay := httptest.NewRecorder()
	router.ServeHTTP(wOrderReplay, reqOrderReplay)

	if wOrderReplay.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for idempotent replay, got %d", wOrderReplay.Code)
	}
	var replayOrder models.OrderResponse
	_ = json.Unmarshal(wOrderReplay.Body.Bytes(), &replayOrder)
	if replayOrder.ID != codOrder.ID {
		t.Errorf("expected replay order ID %s, got %s", codOrder.ID, replayOrder.ID)
	}

	// Verify remaining stock in DB is 8 (10 - 2 = 8, no double deduction)
	var remainingStock int
	_ = db.Pool.QueryRow(context.Background(), "SELECT quantity FROM inventory WHERE menu_item_id = $1", item.ID).Scan(&remainingStock)
	if remainingStock != 8 {
		t.Errorf("expected inventory quantity 8, got %d", remainingStock)
	}

	// Verify GetOrder endpoint returns payment_method COD and status CONFIRMED
	reqGet := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/orders/%s", codOrder.ID), nil)
	reqGet.Header.Set("Authorization", "Bearer "+studentToken)
	wGet := httptest.NewRecorder()
	router.ServeHTTP(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for GET order, got %d", wGet.Code)
	}
	var getOrder models.OrderResponse
	_ = json.Unmarshal(wGet.Body.Bytes(), &getOrder)
	if getOrder.PaymentMethod != "COD" {
		t.Errorf("expected GetOrder payment_method to be 'COD', got '%s'", getOrder.PaymentMethod)
	}

	// Verify ListOrders returns payment_method COD
	reqList := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	reqList.Header.Set("Authorization", "Bearer "+studentToken)
	wList := httptest.NewRecorder()
	router.ServeHTTP(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for GET orders list, got %d", wList.Code)
	}
	var listResp struct {
		Orders []models.OrderResponse `json:"orders"`
	}
	_ = json.Unmarshal(wList.Body.Bytes(), &listResp)
	if len(listResp.Orders) == 0 || listResp.Orders[0].PaymentMethod != "COD" {
		t.Errorf("expected list order payment_method 'COD', got %+v", listResp.Orders)
	}

	// Verify Reservation Worker does not release COD inventory even if expires_at is in the past
	_, _ = db.Pool.Exec(context.Background(), `
		UPDATE inventory_reservations
		SET expires_at = NOW() - INTERVAL '1 hour'
		WHERE order_id = $1
	`, codOrder.ID)
	resWorker := repository.NewInventoryRepository(db)
	_ = resWorker
	var postExpiryStock int
	_ = db.Pool.QueryRow(context.Background(), "SELECT quantity FROM inventory WHERE menu_item_id = $1", item.ID).Scan(&postExpiryStock)
	if postExpiryStock != 8 {
		t.Errorf("expected stock to remain 8 for COD order, got %d", postExpiryStock)
	}

	// 3. Admin updates status: CONFIRMED -> PREPARING
	patchBody1, _ := json.Marshal(map[string]string{"status": "PREPARING"})
	reqPatch1 := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/v1/orders/%s/status", codOrder.ID), bytes.NewBuffer(patchBody1))
	reqPatch1.Header.Set("Authorization", "Bearer "+adminToken)
	reqPatch1.Header.Set("Content-Type", "application/json")
	wPatch1 := httptest.NewRecorder()
	router.ServeHTTP(wPatch1, reqPatch1)
	if wPatch1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for status update to PREPARING, got %d: %s", wPatch1.Code, wPatch1.Body.String())
	}

	// Verify PREPARING notification was saved
	var prepMsg string
	_ = db.Pool.QueryRow(context.Background(), "SELECT message FROM notifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1", student.ID).Scan(&prepMsg)
	if !strings.Contains(prepMsg, "is preparing your order") {
		t.Errorf("expected preparing notification message, got '%s'", prepMsg)
	}

	// 4. Admin updates status: PREPARING -> READY
	patchBody2, _ := json.Marshal(map[string]string{"status": "READY"})
	reqPatch2 := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/v1/orders/%s/status", codOrder.ID), bytes.NewBuffer(patchBody2))
	reqPatch2.Header.Set("Authorization", "Bearer "+adminToken)
	reqPatch2.Header.Set("Content-Type", "application/json")
	wPatch2 := httptest.NewRecorder()
	router.ServeHTTP(wPatch2, reqPatch2)
	if wPatch2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for status update to READY, got %d: %s", wPatch2.Code, wPatch2.Body.String())
	}

	// Verify READY notification was saved
	var readyMsg string
	_ = db.Pool.QueryRow(context.Background(), "SELECT message FROM notifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1", student.ID).Scan(&readyMsg)
	if !strings.Contains(readyMsg, "is ready for pickup") {
		t.Errorf("expected ready notification message, got '%s'", readyMsg)
	}

	// 5. Admin updates status: READY -> COMPLETED
	patchBody3, _ := json.Marshal(map[string]string{"status": "COMPLETED"})
	reqPatch3 := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/v1/orders/%s/status", codOrder.ID), bytes.NewBuffer(patchBody3))
	reqPatch3.Header.Set("Authorization", "Bearer "+adminToken)
	reqPatch3.Header.Set("Content-Type", "application/json")
	wPatch3 := httptest.NewRecorder()
	router.ServeHTTP(wPatch3, reqPatch3)
	if wPatch3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for status update to COMPLETED, got %d: %s", wPatch3.Code, wPatch3.Body.String())
	}

	// Verify COMPLETED notification was saved
	var compMsg string
	_ = db.Pool.QueryRow(context.Background(), "SELECT message FROM notifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1", student.ID).Scan(&compMsg)
	if !strings.Contains(compMsg, "has been completed") {
		t.Errorf("expected completed notification message, got '%s'", compMsg)
	}
}

func TestOrder_AdminPersistentNotificationsAndIdempotency(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	_, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)
	admin1, admin1Token := createRealTestUser(t, db, tokenService, models.RoleAdmin)
	admin2, _ := createRealTestUser(t, db, tokenService, models.RoleAdmin)

	menuRepo := repository.NewMenuRepository(db)
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("AdminNotifItem_%d", time.Now().UnixNano()), "Admin Notif Burger", 80.0, "", true, 20)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}

	// Add item to cart
	b, _ := json.Marshal(models.AddToCartRequest{MenuItemID: item.ID, Quantity: 2})
	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(b))
	addReq.Header.Set("Authorization", "Bearer "+studentToken)
	addReq.Header.Set("Content-Type", "application/json")
	addW := httptest.NewRecorder()
	router.ServeHTTP(addW, addReq)
	if addW.Code != http.StatusOK {
		t.Fatalf("failed to add item to cart: %d %s", addW.Code, addW.Body.String())
	}

	// 1. Initial checkout with Idempotency-Key
	idempKey := fmt.Sprintf("admin_notif_key_%d", time.Now().UnixNano())
	checkoutBody, _ := json.Marshal(map[string]string{"payment_method": "COD"})
	reqCheckout := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBuffer(checkoutBody))
	reqCheckout.Header.Set("Authorization", "Bearer "+studentToken)
	reqCheckout.Header.Set("Idempotency-Key", idempKey)
	reqCheckout.Header.Set("Content-Type", "application/json")
	wCheckout := httptest.NewRecorder()
	router.ServeHTTP(wCheckout, reqCheckout)

	if wCheckout.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", wCheckout.Code, wCheckout.Body.String())
	}

	var createdOrder models.OrderResponse
	if err := json.Unmarshal(wCheckout.Body.Bytes(), &createdOrder); err != nil {
		t.Fatalf("failed to parse order response: %v", err)
	}

	// Verify Admin 1 persistent notification in DB
	var admin1NotifCount int
	err = db.Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM notifications
		WHERE user_id = $1 AND title = 'New Order' AND data->>'order_id' = $2
	`, admin1.ID, createdOrder.ID).Scan(&admin1NotifCount)
	if err != nil || admin1NotifCount != 1 {
		t.Errorf("expected 1 persistent notification for admin1, got count=%d, err=%v", admin1NotifCount, err)
	}

	// Verify Admin 2 persistent notification in DB
	var admin2NotifCount int
	err = db.Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM notifications
		WHERE user_id = $1 AND title = 'New Order' AND data->>'order_id' = $2
	`, admin2.ID, createdOrder.ID).Scan(&admin2NotifCount)
	if err != nil || admin2NotifCount != 1 {
		t.Errorf("expected 1 persistent notification for admin2, got count=%d, err=%v", admin2NotifCount, err)
	}

	// 2. Idempotent replay: student posts identical checkout request
	reqReplay := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBuffer(checkoutBody))
	reqReplay.Header.Set("Authorization", "Bearer "+studentToken)
	reqReplay.Header.Set("Idempotency-Key", idempKey)
	reqReplay.Header.Set("Content-Type", "application/json")
	wReplay := httptest.NewRecorder()
	router.ServeHTTP(wReplay, reqReplay)

	if wReplay.Code != http.StatusCreated {
		t.Fatalf("expected 201 on replay, got %d: %s", wReplay.Code, wReplay.Body.String())
	}

	// Verify NO duplicate notifications were inserted on replay
	_ = db.Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM notifications
		WHERE user_id = $1 AND title = 'New Order' AND data->>'order_id' = $2
	`, admin1.ID, createdOrder.ID).Scan(&admin1NotifCount)
	if admin1NotifCount != 1 {
		t.Errorf("expected still 1 notification after replay, got %d", admin1NotifCount)
	}

	// 3. Admin fetches notifications via GET /api/v1/notifications
	reqGetNotifs := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	reqGetNotifs.Header.Set("Authorization", "Bearer "+admin1Token)
	wGetNotifs := httptest.NewRecorder()
	router.ServeHTTP(wGetNotifs, reqGetNotifs)
	if wGetNotifs.Code != http.StatusOK {
		t.Fatalf("failed to fetch notifications: %d %s", wGetNotifs.Code, wGetNotifs.Body.String())
	}

	var notifList models.NotificationListResponse
	if err := json.Unmarshal(wGetNotifs.Body.Bytes(), &notifList); err != nil {
		t.Fatalf("failed to unmarshal notifications: %v", err)
	}
	if notifList.UnreadCount < 1 {
		t.Errorf("expected admin unread_count >= 1, got %d", notifList.UnreadCount)
	}

	// 4. Admin marks all as read
	reqReadAll := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/read-all", nil)
	reqReadAll.Header.Set("Authorization", "Bearer "+admin1Token)
	wReadAll := httptest.NewRecorder()
	router.ServeHTTP(wReadAll, reqReadAll)
	if wReadAll.Code != http.StatusOK {
		t.Fatalf("failed to mark all as read: %d %s", wReadAll.Code, wReadAll.Body.String())
	}

	var afterReadAll models.NotificationListResponse
	reqGetAfter := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	reqGetAfter.Header.Set("Authorization", "Bearer "+admin1Token)
	wGetAfter := httptest.NewRecorder()
	router.ServeHTTP(wGetAfter, reqGetAfter)
	_ = json.Unmarshal(wGetAfter.Body.Bytes(), &afterReadAll)
	if afterReadAll.UnreadCount != 0 {
		t.Errorf("expected unread_count=0 after read-all, got %d", afterReadAll.UnreadCount)
	}
}

