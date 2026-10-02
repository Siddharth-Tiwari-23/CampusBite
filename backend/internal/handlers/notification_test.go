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

	"campusbite/internal/auth"
	"campusbite/internal/cache"
	"campusbite/internal/database"
	"campusbite/internal/models"
	"campusbite/internal/repository"
	"campusbite/internal/routes"
	"campusbite/internal/service"
	"campusbite/internal/ws"
)

func setupNotificationTestApp(t *testing.T) (*httptest.Server, *database.DB, *auth.TokenService, *ws.Hub) {
	t.Helper()
	_, db, tokenService := setupIntegrationApp(t)
	rzpService := service.NewRazorpayService("rzp_test_campusbite_mock_key", "rzp_test_campusbite_mock_secret", "rzp_test_campusbite_mock_webhook_secret")
	noOpCache := cache.NewNoOpCache()
	wsHub := ws.NewHub()

	router := routes.SetupRouter(db, tokenService, rzpService, noOpCache, wsHub)
	server := httptest.NewServer(router)

	return server, db, tokenService, wsHub
}

func TestNotification_Unauthorized(t *testing.T) {
	server, db, _, _ := setupNotificationTestApp(t)
	defer server.Close()
	defer db.Close()

	// GET /api/v1/notifications without token
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/notifications", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401 Unauthorized, got %d", resp.StatusCode)
	}
}

func TestNotification_ListAndPagination(t *testing.T) {
	server, db, tokenService, _ := setupNotificationTestApp(t)
	defer server.Close()
	defer db.Close()

	user, token := createRealTestUser(t, db, tokenService, models.RoleStudent)
	notifRepo := repository.NewNotificationRepository(db)
	ctx := context.Background()

	// 1. Initial list should be empty
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var listResp models.NotificationListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if listResp.Total != 0 || listResp.UnreadCount != 0 || len(listResp.Notifications) != 0 {
		t.Fatalf("expected empty notifications list, got %d items", len(listResp.Notifications))
	}

	// 2. Insert 5 notifications
	for i := 1; i <= 5; i++ {
		_, err := notifRepo.Create(
			ctx,
			user.ID,
			models.NotificationTypeOrderStatus,
			fmt.Sprintf("Title %d", i),
			fmt.Sprintf("Message %d", i),
			map[string]interface{}{"idx": i},
		)
		if err != nil {
			t.Fatalf("failed to create notification: %v", err)
		}
	}

	// 3. Query with limit 2, offset 0
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/notifications?limit=2&offset=0", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if listResp.Total != 5 {
		t.Errorf("expected total 5, got %d", listResp.Total)
	}
	if listResp.UnreadCount != 5 {
		t.Errorf("expected unread 5, got %d", listResp.UnreadCount)
	}
	if len(listResp.Notifications) != 2 {
		t.Errorf("expected 2 notifications on page 1, got %d", len(listResp.Notifications))
	}

	// 4. Query with limit 2, offset 4 (page 3)
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/notifications?limit=2&offset=4", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(listResp.Notifications) != 1 {
		t.Errorf("expected 1 notification on page 3, got %d", len(listResp.Notifications))
	}
}

func TestNotification_MarkAsRead_And_UserIsolation(t *testing.T) {
	server, db, tokenService, _ := setupNotificationTestApp(t)
	defer server.Close()
	defer db.Close()

	user1, token1 := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_, token2 := createRealTestUser(t, db, tokenService, models.RoleStudent)
	notifRepo := repository.NewNotificationRepository(db)
	ctx := context.Background()

	// Insert notification for User 1
	notif1, err := notifRepo.Create(
		ctx,
		user1.ID,
		models.NotificationTypeOrderStatus,
		"Order #1 Ready",
		"Your order is ready for pickup!",
		map[string]interface{}{"order_id": "test-order-1"},
	)
	if err != nil {
		t.Fatalf("failed to create notification: %v", err)
	}

	// User 2 attempts to mark User 1's notification as read -> should fail with 404
	req, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/v1/notifications/"+notif1.ID+"/read", nil)
	req.Header.Set("Authorization", "Bearer "+token2)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404 Not Found for cross-user mark-read, got %d", resp.StatusCode)
	}

	// User 1 marks their own notification as read -> 200 OK
	req, _ = http.NewRequest(http.MethodPatch, server.URL+"/api/v1/notifications/"+notif1.ID+"/read", nil)
	req.Header.Set("Authorization", "Bearer "+token1)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", resp.StatusCode)
	}

	var updated models.Notification
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !updated.IsRead {
		t.Errorf("expected is_read to be true, got false")
	}

	// Verify unread count is now 0 for User 1
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+token1)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var listResp models.NotificationListResponse
	_ = json.NewDecoder(resp.Body).Decode(&listResp)
	if listResp.UnreadCount != 0 {
		t.Errorf("expected unread_count 0, got %d", listResp.UnreadCount)
	}
}

func TestNotification_MarkAllAsRead(t *testing.T) {
	server, db, tokenService, _ := setupNotificationTestApp(t)
	defer server.Close()
	defer db.Close()

	user1, token1 := createRealTestUser(t, db, tokenService, models.RoleStudent)
	user2, token2 := createRealTestUser(t, db, tokenService, models.RoleStudent)
	notifRepo := repository.NewNotificationRepository(db)
	ctx := context.Background()

	// Insert 3 unread for User 1 and 2 unread for User 2
	for i := 1; i <= 3; i++ {
		_, _ = notifRepo.Create(ctx, user1.ID, models.NotificationTypeSystem, fmt.Sprintf("U1 Notif %d", i), "Body", nil)
	}
	for i := 1; i <= 2; i++ {
		_, _ = notifRepo.Create(ctx, user2.ID, models.NotificationTypeSystem, fmt.Sprintf("U2 Notif %d", i), "Body", nil)
	}

	// User 1 marks all as read
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/notifications/read-all", nil)
	req.Header.Set("Authorization", "Bearer "+token1)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if count, ok := result["marked_count"].(float64); !ok || int(count) != 3 {
		t.Errorf("expected marked_count 3, got %v", result["marked_count"])
	}

	// Verify User 2's notifications are STILL unread (isolation)
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+token2)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var u2List models.NotificationListResponse
	_ = json.NewDecoder(resp.Body).Decode(&u2List)
	if u2List.UnreadCount != 2 {
		t.Errorf("expected User 2 to still have 2 unread notifications, got %d", u2List.UnreadCount)
	}
}

func TestNotification_OrderStatusLifecycle_Persistence(t *testing.T) {
	server, db, tokenService, _ := setupNotificationTestApp(t)
	defer server.Close()
	defer db.Close()

	_, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_, adminToken := createRealTestUser(t, db, tokenService, models.RoleAdmin)
	menuRepo := repository.NewMenuRepository(db)
	ctx := context.Background()

	// 1. Create a menu item with inventory
	menuItem, err := menuRepo.Create(ctx, fmt.Sprintf("Cheese Pizza %d", time.Now().UnixNano()), "Delicious", 150.00, "", true, 20)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}

	// 2. Add to student's cart via HTTP
	addBody, _ := json.Marshal(models.AddToCartRequest{MenuItemID: menuItem.ID, Quantity: 2})
	addReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/cart/items", bytes.NewBuffer(addBody))
	addReq.Header.Set("Authorization", "Bearer "+studentToken)
	addReq.Header.Set("Content-Type", "application/json")
	addResp, err := http.DefaultClient.Do(addReq)
	if err != nil {
		t.Fatalf("failed to add to cart: %v", err)
	}
	addResp.Body.Close()
	if addResp.StatusCode != http.StatusOK {
		t.Fatalf("add to cart failed with status %d", addResp.StatusCode)
	}

	// 3. Checkout order
	idempotencyKey := fmt.Sprintf("idemp_notif_test_%d", time.Now().UnixNano())
	orderReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/orders", nil)
	orderReq.Header.Set("Authorization", "Bearer "+studentToken)
	orderReq.Header.Set("Idempotency-Key", idempotencyKey)
	orderResp, err := http.DefaultClient.Do(orderReq)
	if err != nil {
		t.Fatalf("failed to checkout: %v", err)
	}
	defer orderResp.Body.Close()
	if orderResp.StatusCode != http.StatusCreated {
		t.Fatalf("checkout failed with status %d", orderResp.StatusCode)
	}

	var order models.OrderResponse
	if err := json.NewDecoder(orderResp.Body).Decode(&order); err != nil {
		t.Fatalf("failed to decode order: %v", err)
	}

	// 4. Admin transitions status to PREPARING
	reqBody, _ := json.Marshal(map[string]string{"status": models.OrderStatusPreparing})
	req, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/v1/orders/"+order.ID+"/status", bytes.NewBuffer(reqBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", resp.StatusCode)
	}

	// 5. Admin transitions status to READY
	reqBody, _ = json.Marshal(map[string]string{"status": models.OrderStatusReady})
	req, _ = http.NewRequest(http.MethodPatch, server.URL+"/api/v1/orders/"+order.ID+"/status", bytes.NewBuffer(reqBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", resp.StatusCode)
	}

	// 6. Offline recovery test: Student fetches notifications and verifies both PREPARING and READY notifications exist
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+studentToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var listResp models.NotificationListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if listResp.Total < 2 {
		t.Fatalf("expected at least 2 notifications for status changes, got %d", listResp.Total)
	}

	foundPreparing := false
	foundReady := false
	for _, n := range listResp.Notifications {
		if n.Type == models.NotificationTypeOrderStatus {
			if n.Title == "Order Preparing" {
				foundPreparing = true
			}
			if n.Title == "Order Ready" {
				foundReady = true
			}
		}
	}

	if !foundPreparing {
		t.Errorf("expected to find 'Order Preparing' notification in student history")
	}
	if !foundReady {
		t.Errorf("expected to find 'Order Ready' notification in student history")
	}
}

func TestNotification_PaymentSuccess_Persistence(t *testing.T) {
	server, db, tokenService, _ := setupNotificationTestApp(t)
	defer server.Close()
	defer db.Close()

	student, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)
	menuRepo := repository.NewMenuRepository(db)
	orderRepo := repository.NewOrderRepository(db)
	paymentRepo := repository.NewPaymentRepository(db, orderRepo)
	ctx := context.Background()

	// 1. Create item and checkout
	item, err := menuRepo.Create(ctx, fmt.Sprintf("Burger_%d", time.Now().UnixNano()), "Juicy", 100.00, "", true, 10)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}

	addBody, _ := json.Marshal(models.AddToCartRequest{MenuItemID: item.ID, Quantity: 1})
	addReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/cart/items", bytes.NewBuffer(addBody))
	addReq.Header.Set("Authorization", "Bearer "+studentToken)
	addReq.Header.Set("Content-Type", "application/json")
	addResp, err := http.DefaultClient.Do(addReq)
	if err != nil {
		t.Fatalf("failed to add to cart: %v", err)
	}
	addResp.Body.Close()

	idempKey := fmt.Sprintf("idemp_pay_test_%d", time.Now().UnixNano())
	order, err := orderRepo.CreateFromCart(ctx, student.ID, idempKey, "")
	if err != nil {
		t.Fatalf("failed to create order: %v", err)
	}

	// 2. Create pending payment
	providerOrderID := fmt.Sprintf("order_rzp_mock_%d", time.Now().UnixNano())
	_, err = paymentRepo.CreateOrGetPayment(ctx, order.ID, providerOrderID, order.TotalAmount)
	if err != nil {
		t.Fatalf("failed to create payment: %v", err)
	}

	// 3. Confirm payment success
	providerPaymentID := fmt.Sprintf("pay_rzp_mock_%d", time.Now().UnixNano())
	_, _, err = paymentRepo.ConfirmPaymentSuccess(ctx, providerOrderID, providerPaymentID)
	if err != nil {
		t.Fatalf("failed to confirm payment: %v", err)
	}

	// 4. Check that student has received the payment confirmation notification
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+studentToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var listResp models.NotificationListResponse
	_ = json.NewDecoder(resp.Body).Decode(&listResp)

	foundConfirmed := false
	for _, n := range listResp.Notifications {
		if n.Type == models.NotificationTypePayment && n.Title == "Order Confirmed" {
			foundConfirmed = true
			break
		}
	}

	if !foundConfirmed {
		t.Errorf("expected to find 'Order Confirmed' payment notification")
	}
}

func TestNotification_PaymentFailure_Persistence(t *testing.T) {
	server, db, tokenService, _ := setupNotificationTestApp(t)
	defer server.Close()
	defer db.Close()

	student, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)
	menuRepo := repository.NewMenuRepository(db)
	orderRepo := repository.NewOrderRepository(db)
	paymentRepo := repository.NewPaymentRepository(db, orderRepo)
	ctx := context.Background()

	// 1. Create item and checkout
	item, err := menuRepo.Create(ctx, fmt.Sprintf("Sandwich_%d", time.Now().UnixNano()), "Fresh", 80.00, "", true, 10)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}

	addBody, _ := json.Marshal(models.AddToCartRequest{MenuItemID: item.ID, Quantity: 1})
	addReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/cart/items", bytes.NewBuffer(addBody))
	addReq.Header.Set("Authorization", "Bearer "+studentToken)
	addReq.Header.Set("Content-Type", "application/json")
	addResp, err := http.DefaultClient.Do(addReq)
	if err != nil {
		t.Fatalf("failed to add to cart: %v", err)
	}
	addResp.Body.Close()

	idempKey := fmt.Sprintf("idemp_pay_fail_%d", time.Now().UnixNano())
	order, err := orderRepo.CreateFromCart(ctx, student.ID, idempKey, "")
	if err != nil {
		t.Fatalf("failed to create order: %v", err)
	}

	// 2. Create pending payment
	providerOrderID := fmt.Sprintf("order_rzp_fail_%d", time.Now().UnixNano())
	_, err = paymentRepo.CreateOrGetPayment(ctx, order.ID, providerOrderID, order.TotalAmount)
	if err != nil {
		t.Fatalf("failed to create payment: %v", err)
	}

	// 3. Handle payment failure
	providerPaymentID := fmt.Sprintf("pay_rzp_fail_%d", time.Now().UnixNano())
	err = paymentRepo.HandlePaymentFailure(ctx, providerOrderID, providerPaymentID, "insufficient_funds")
	if err != nil {
		t.Fatalf("failed to handle payment failure: %v", err)
	}

	// 4. Check that student has received the payment failure notification
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+studentToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var listResp models.NotificationListResponse
	_ = json.NewDecoder(resp.Body).Decode(&listResp)

	foundFailed := false
	for _, n := range listResp.Notifications {
		if n.Type == models.NotificationTypePayment && n.Title == "Payment Failed" {
			foundFailed = true
			break
		}
	}

	if !foundFailed {
		t.Errorf("expected to find 'Payment Failed' notification")
	}
}
