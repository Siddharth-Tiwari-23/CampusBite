package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"campusbite/internal/models"
	"campusbite/internal/repository"
	"campusbite/internal/ws"
)

func TestAdminAndRealtimeUser_ComprehensiveE2E(t *testing.T) {
	server, db, tokenService, wsHub := setupWSTestServer(t)
	defer server.Close()
	defer db.Close()

	menuRepo := repository.NewMenuRepository(db)
	cartRepo := repository.NewCartRepository(db)
	notifRepo := repository.NewNotificationRepository(db)
	orderRepo := repository.NewOrderRepository(db)
	client := server.Client()

	// 1. Setup Student and Admin users
	student, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_, otherToken := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_, adminToken := createRealTestUser(t, db, tokenService, models.RoleAdmin)

	// 2. Setup isolated menu item
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("AdminE2E_Thali_%d", time.Now().UnixNano()), "Special Thali", 150.00, "", true, 50)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), "UPDATE menu_items SET is_available = false WHERE id = $1", item.ID)
	})

	// 3. Connect Student and Admin over WebSocket
	connStudent, _, err := dialWS(t, server.URL, studentToken, false)
	if err != nil {
		t.Fatalf("failed to connect student websocket: %v", err)
	}
	defer connStudent.Close()

	connAdmin, _, err := dialWS(t, server.URL, adminToken, false)
	if err != nil {
		t.Fatalf("failed to connect admin websocket: %v", err)
	}
	defer connAdmin.Close()

	time.Sleep(50 * time.Millisecond)

	if !wsHub.IsUserConnected(student.ID) {
		t.Errorf("expected student to be registered in websocket hub")
	}

	// -------------------------------------------------------------------------
	// STUDENT FLOW: Add to Tray -> Checkout -> Payment -> Order Confirmed
	// -------------------------------------------------------------------------
	_, err = cartRepo.AddItem(context.Background(), student.ID, item.ID, 2)
	if err != nil {
		t.Fatalf("failed to add item to cart: %v", err)
	}

	idempKey := fmt.Sprintf("idemp_admin_e2e_%d", time.Now().UnixNano())
	reqCheckout, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/orders", bytes.NewBuffer([]byte(`{}`)))
	reqCheckout.Header.Set("Authorization", "Bearer "+studentToken)
	reqCheckout.Header.Set("Idempotency-Key", idempKey)
	reqCheckout.Header.Set("Content-Type", "application/json")

	respCheckout, err := client.Do(reqCheckout)
	if err != nil || respCheckout.StatusCode != http.StatusCreated {
		t.Fatalf("checkout failed: %v, status: %d", err, respCheckout.StatusCode)
	}
	var createdOrder models.OrderResponse
	_ = json.NewDecoder(respCheckout.Body).Decode(&createdOrder)
	respCheckout.Body.Close()

	// Admin should receive NEW_ORDER event over WebSocket
	_ = connAdmin.SetReadDeadline(time.Now().Add(2 * time.Second))
	var adminNewOrder ws.Event
	err = connAdmin.ReadJSON(&adminNewOrder)
	if err != nil {
		t.Fatalf("admin failed to receive NEW_ORDER event: %v", err)
	}
	if adminNewOrder.Type != ws.EventNewOrder || adminNewOrder.OrderID != createdOrder.ID {
		t.Errorf("expected NEW_ORDER event for %s, got %+v", createdOrder.ID, adminNewOrder)
	}

	// Student initiates payment order
	reqPay, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/orders/%s/payment", server.URL, createdOrder.ID), nil)
	reqPay.Header.Set("Authorization", "Bearer "+studentToken)
	respPay, err := client.Do(reqPay)
	if err != nil || respPay.StatusCode != http.StatusOK {
		t.Fatalf("failed to create payment order: %v, status: %d", err, respPay.StatusCode)
	}
	var payResp models.CreatePaymentOrderResponse
	_ = json.NewDecoder(respPay.Body).Decode(&payResp)
	respPay.Body.Close()

	// Student verifies payment (mock key valid signature)
	mockPayID := fmt.Sprintf("pay_test_%d", time.Now().UnixNano())
	verifyBody, _ := json.Marshal(models.VerifyPaymentRequest{
		RazorpayOrderID:   payResp.RazorpayOrderID,
		RazorpayPaymentID: mockPayID,
		RazorpaySignature: "sig_test_valid_mock_signature",
	})
	reqVerify, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/payments/verify", bytes.NewBuffer(verifyBody))
	reqVerify.Header.Set("Authorization", "Bearer "+studentToken)
	reqVerify.Header.Set("Content-Type", "application/json")
	respVerify, err := client.Do(reqVerify)
	if err != nil || respVerify.StatusCode != http.StatusOK {
		t.Fatalf("payment verification failed: %v, status: %d", err, respVerify.StatusCode)
	}
	respVerify.Body.Close()

	// Student receives ORDER_STATUS_UPDATED -> CONFIRMED over WebSocket
	_ = connStudent.SetReadDeadline(time.Now().Add(2 * time.Second))
	var wsEvtConfirmed ws.Event
	err = connStudent.ReadJSON(&wsEvtConfirmed)
	if err != nil {
		t.Fatalf("student failed to receive CONFIRMED ws event: %v", err)
	}
	if wsEvtConfirmed.Type != ws.EventOrderStatusUpdated || wsEvtConfirmed.Status != models.OrderStatusConfirmed {
		t.Errorf("expected CONFIRMED event, got %+v", wsEvtConfirmed)
	}

	// -------------------------------------------------------------------------
	// ADMIN FLOW: Open Dashboard -> Verify Order -> Lifecycle State Transitions
	// -------------------------------------------------------------------------
	// A. Admin opens dashboard (GET /api/v1/orders)
	reqAdminList, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/orders", nil)
	reqAdminList.Header.Set("Authorization", "Bearer "+adminToken)
	respAdminList, err := client.Do(reqAdminList)
	if err != nil || respAdminList.StatusCode != http.StatusOK {
		t.Fatalf("admin list orders failed: %v, status: %d", err, respAdminList.StatusCode)
	}
	var adminListResp struct {
		Orders []models.OrderResponse `json:"orders"`
	}
	_ = json.NewDecoder(respAdminList.Body).Decode(&adminListResp)
	respAdminList.Body.Close()

	foundInAdminDashboard := false
	for _, o := range adminListResp.Orders {
		if o.ID == createdOrder.ID {
			foundInAdminDashboard = true
			if o.Status != models.OrderStatusConfirmed {
				t.Errorf("expected order status CONFIRMED in dashboard, got %s", o.Status)
			}
			break
		}
	}
	if !foundInAdminDashboard {
		t.Errorf("created order %s not found in admin dashboard order list", createdOrder.ID)
	}

	// B. Admin transitions order: CONFIRMED → PREPARING → READY → COMPLETED
	transitions := []string{
		models.OrderStatusPreparing,
		models.OrderStatusReady,
		models.OrderStatusCompleted,
	}

	for _, nextStatus := range transitions {
		patchPayload, _ := json.Marshal(map[string]string{"status": nextStatus})
		reqPatch, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/orders/%s/status", server.URL, createdOrder.ID), bytes.NewBuffer(patchPayload))
		reqPatch.Header.Set("Authorization", "Bearer "+adminToken)
		reqPatch.Header.Set("Content-Type", "application/json")

		respPatch, err := client.Do(reqPatch)
		if err != nil || respPatch.StatusCode != http.StatusOK {
			t.Fatalf("admin patch status to %s failed: %v, status: %d", nextStatus, err, respPatch.StatusCode)
		}
		respPatch.Body.Close()

		// Verify student receives real-time WebSocket event for this transition
		_ = connStudent.SetReadDeadline(time.Now().Add(2 * time.Second))
		var evt ws.Event
		err = connStudent.ReadJSON(&evt)
		if err != nil {
			t.Fatalf("student failed to receive %s event over websocket: %v", nextStatus, err)
		}
		if evt.Type != ws.EventOrderStatusUpdated || evt.Status != nextStatus || evt.OrderID != createdOrder.ID {
			t.Errorf("expected event for %s, got %+v", nextStatus, evt)
		}
	}

	// -------------------------------------------------------------------------
	// NOTIFICATIONS & AUTHORITATIVE PERSISTENCE
	// -------------------------------------------------------------------------
	// 1. Verify notification records exist for student in PostgreSQL
	notifsResp, err := notifRepo.ListByUser(context.Background(), student.ID, 20, 0)
	if err != nil || notifsResp == nil {
		t.Fatalf("failed to query notifications: %v", err)
	}
	if len(notifsResp.Notifications) < 4 { // CONFIRMED, PREPARING, READY, COMPLETED
		t.Errorf("expected at least 4 notifications for order lifecycle, got %d", len(notifsResp.Notifications))
	}

	// 2. Mark single notification as read (PATCH /api/v1/notifications/:id/read)
	firstNotif := notifsResp.Notifications[0]
	reqReadOne, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/notifications/%s/read", server.URL, firstNotif.ID), nil)
	reqReadOne.Header.Set("Authorization", "Bearer "+studentToken)
	respReadOne, err := client.Do(reqReadOne)
	if err != nil || respReadOne.StatusCode != http.StatusOK {
		t.Errorf("failed to mark notification as read: %v, status: %d", err, respReadOne.StatusCode)
	}
	respReadOne.Body.Close()

	// 3. Mark all notifications as read (POST /api/v1/notifications/read-all)
	reqReadAll, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/notifications/read-all", nil)
	reqReadAll.Header.Set("Authorization", "Bearer "+studentToken)
	respReadAll, err := client.Do(reqReadAll)
	if err != nil || respReadAll.StatusCode != http.StatusOK {
		t.Errorf("failed to mark all notifications as read: %v, status: %d", err, respReadAll.StatusCode)
	}
	respReadAll.Body.Close()

	afterReadResp, _ := notifRepo.ListByUser(context.Background(), student.ID, 20, 0)
	if afterReadResp.UnreadCount != 0 {
		t.Errorf("expected 0 unread notifications after read-all, got %d", afterReadResp.UnreadCount)
	}

	// 4. Verify PostgreSQL authoritative state on reconnect/refresh
	finalOrder, err := orderRepo.GetByID(context.Background(), createdOrder.ID)
	if err != nil {
		t.Fatalf("failed to retrieve final order: %v", err)
	}
	if finalOrder.Status != models.OrderStatusCompleted {
		t.Errorf("expected final authoritative order status COMPLETED, got %s", finalOrder.Status)
	}

	// -------------------------------------------------------------------------
	// AUTHORIZATION & SECURITY CONTROLS
	// -------------------------------------------------------------------------
	// A. Student cannot access admin order status endpoint -> 403 Forbidden
	reqStudentPatch, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/orders/%s/status", server.URL, createdOrder.ID), bytes.NewBuffer([]byte(`{"status":"CANCELLED"}`)))
	reqStudentPatch.Header.Set("Authorization", "Bearer "+studentToken)
	reqStudentPatch.Header.Set("Content-Type", "application/json")
	respStudentPatch, err := client.Do(reqStudentPatch)
	if err != nil || respStudentPatch.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for student updating order status, got %d", respStudentPatch.StatusCode)
	}
	respStudentPatch.Body.Close()

	// B. Student cannot access admin analytics endpoint -> 403 Forbidden
	reqAnalytics, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/analytics/trends", nil)
	reqAnalytics.Header.Set("Authorization", "Bearer "+studentToken)
	respAnalytics, err := client.Do(reqAnalytics)
	if err != nil || respAnalytics.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for student accessing analytics, got %d", respAnalytics.StatusCode)
	}
	respAnalytics.Body.Close()

	// C. Student cannot view another user's order details -> 403 Forbidden
	reqOtherView, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/orders/%s", server.URL, createdOrder.ID), nil)
	reqOtherView.Header.Set("Authorization", "Bearer "+otherToken)
	respOtherView, err := client.Do(reqOtherView)
	if err != nil || respOtherView.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for viewing another user's order, got %d", respOtherView.StatusCode)
	}
	respOtherView.Body.Close()
}
