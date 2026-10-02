package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

	"github.com/gorilla/websocket"
)

func setupWSTestServer(t *testing.T) (*httptest.Server, *database.DB, *auth.TokenService, *ws.Hub) {
	_, db, tokenService := setupIntegrationApp(t)
	rzpService := service.NewRazorpayService("rzp_test_campusbite_mock_key", "rzp_test_campusbite_mock_secret", "rzp_test_campusbite_mock_webhook_secret")
	noOpCache := cache.NewNoOpCache()
	wsHub := ws.NewHub()

	router := routes.SetupRouter(db, tokenService, rzpService, noOpCache, wsHub)
	server := httptest.NewServer(router)

	return server, db, tokenService, wsHub
}

func dialWS(t *testing.T, serverURL, token string, useHeader bool) (*websocket.Conn, *http.Response, error) {
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/api/v1/ws"

	var header http.Header
	if useHeader {
		header = http.Header{}
		header.Set("Authorization", "Bearer "+token)
	} else if token != "" {
		wsURL += "?token=" + token
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 3 * time.Second,
	}

	conn, resp, err := dialer.Dial(wsURL, header)
	return conn, resp, err
}

func TestWS_Authentication(t *testing.T) {
	server, db, tokenService, _ := setupWSTestServer(t)
	defer server.Close()
	defer db.Close()

	_, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	// 1. Valid token via Query Parameter -> 101 Switching Protocols
	conn1, resp1, err := dialWS(t, server.URL, studentToken, false)
	if err != nil {
		t.Fatalf("expected successful WS connection with query token, got error: %v", err)
	}
	if resp1.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("expected 101 Switching Protocols, got %d", resp1.StatusCode)
	}
	_ = conn1.Close()

	// 2. Valid token via Authorization Header -> 101 Switching Protocols
	conn2, resp2, err := dialWS(t, server.URL, studentToken, true)
	if err != nil {
		t.Fatalf("expected successful WS connection with auth header, got error: %v", err)
	}
	if resp2.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("expected 101 Switching Protocols, got %d", resp2.StatusCode)
	}
	_ = conn2.Close()

	// 3. Missing token -> 401 Unauthorized
	conn3, resp3, err := dialWS(t, server.URL, "", false)
	if err == nil {
		_ = conn3.Close()
		t.Fatalf("expected error on missing token, but connection succeeded")
	}
	if resp3 != nil && resp3.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for missing token, got %d", resp3.StatusCode)
	}

	// 4. Invalid / tampered token -> 401 Unauthorized
	conn4, resp4, err := dialWS(t, server.URL, "invalid_jwt_token_payload", false)
	if err == nil {
		_ = conn4.Close()
		t.Fatalf("expected error on invalid token, but connection succeeded")
	}
	if resp4 != nil && resp4.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for invalid token, got %d", resp4.StatusCode)
	}
}

func TestWS_OrderLifecycleAndAdminEvents(t *testing.T) {
	server, db, tokenService, wsHub := setupWSTestServer(t)
	defer server.Close()
	defer db.Close()

	userA, tokenA := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_, tokenB := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_, adminToken := createRealTestUser(t, db, tokenService, models.RoleAdmin)

	// Connect Student A, Student B, and Admin over WebSocket
	connA, _, err := dialWS(t, server.URL, tokenA, false)
	if err != nil {
		t.Fatalf("failed to connect student A: %v", err)
	}
	defer connA.Close()

	connB, _, err := dialWS(t, server.URL, tokenB, false)
	if err != nil {
		t.Fatalf("failed to connect student B: %v", err)
	}
	defer connB.Close()

	connAdmin, _, err := dialWS(t, server.URL, adminToken, false)
	if err != nil {
		t.Fatalf("failed to connect admin: %v", err)
	}
	defer connAdmin.Close()

	// Wait briefly for registration
	time.Sleep(50 * time.Millisecond)

	if !wsHub.IsUserConnected(userA.ID) {
		t.Errorf("expected user A to be connected in hub")
	}

	// 1. Student A adds items to cart and places an order
	menuRepo := repository.NewMenuRepository(db)
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("WSPizza_%d", time.Now().UnixNano()), "Pizza", 200.0, "", true, 10)
	if err != nil {
		t.Fatalf("failed to seed menu item: %v", err)
	}

	cartRepo := repository.NewCartRepository(db)
	_, _ = cartRepo.AddItem(context.Background(), userA.ID, item.ID, 1)

	// Checkout order
	client := server.Client()
	checkoutReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/orders", nil)
	checkoutReq.Header.Set("Authorization", "Bearer "+tokenA)
	checkoutReq.Header.Set("Idempotency-Key", fmt.Sprintf("ws-checkout-%d", time.Now().UnixNano()))
	respCheckout, err := client.Do(checkoutReq)
	if err != nil || respCheckout.StatusCode != http.StatusCreated {
		t.Fatalf("failed to checkout order: %v, status: %d", err, respCheckout.StatusCode)
	}

	var createdOrder models.OrderResponse
	_ = json.NewDecoder(respCheckout.Body).Decode(&createdOrder)
	respCheckout.Body.Close()

	// 2. Admin should receive NEW_ORDER event
	_ = connAdmin.SetReadDeadline(time.Now().Add(2 * time.Second))
	var adminEvent ws.Event
	err = connAdmin.ReadJSON(&adminEvent)
	if err != nil {
		t.Fatalf("admin failed to receive NEW_ORDER event: %v", err)
	}
	if adminEvent.Type != ws.EventNewOrder || adminEvent.OrderID != createdOrder.ID {
		t.Errorf("expected NEW_ORDER for order %s, got %+v", createdOrder.ID, adminEvent)
	}

	// 3. Admin updates order status to PREPARING (PATCH /api/v1/orders/:id/status)
	statusPayload, _ := json.Marshal(map[string]string{"status": "PREPARING"})
	patchReq, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/orders/%s/status", server.URL, createdOrder.ID), bytes.NewBuffer(statusPayload))
	patchReq.Header.Set("Authorization", "Bearer "+adminToken)
	patchReq.Header.Set("Content-Type", "application/json")
	respPatch, err := client.Do(patchReq)
	if err != nil || respPatch.StatusCode != http.StatusOK {
		t.Fatalf("failed to patch order status: %v, status: %d", err, respPatch.StatusCode)
	}
	respPatch.Body.Close()

	// 4. Student A should receive ORDER_STATUS_UPDATED (status: PREPARING)
	_ = connA.SetReadDeadline(time.Now().Add(2 * time.Second))
	var eventA ws.Event
	err = connA.ReadJSON(&eventA)
	if err != nil {
		t.Fatalf("student A failed to receive ORDER_STATUS_UPDATED event: %v", err)
	}
	if eventA.Type != ws.EventOrderStatusUpdated || eventA.OrderID != createdOrder.ID || eventA.Status != "PREPARING" {
		t.Errorf("student A received unexpected event: %+v", eventA)
	}

	// 5. Student B must NOT receive Student A's order event
	_ = connB.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	var eventB ws.Event
	err = connB.ReadJSON(&eventB)
	if err == nil {
		t.Errorf("student B should not receive student A's event, but received: %+v", eventB)
	}

	// 6. Admin updates order status to READY
	statusReadyPayload, _ := json.Marshal(map[string]string{"status": "READY"})
	patchReadyReq, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/orders/%s/status", server.URL, createdOrder.ID), bytes.NewBuffer(statusReadyPayload))
	patchReadyReq.Header.Set("Authorization", "Bearer "+adminToken)
	patchReadyReq.Header.Set("Content-Type", "application/json")
	respReady, err := client.Do(patchReadyReq)
	if err != nil || respReady.StatusCode != http.StatusOK {
		t.Fatalf("failed to patch ready status: %v", err)
	}
	respReady.Body.Close()

	// Student A receives READY event
	_ = connA.SetReadDeadline(time.Now().Add(2 * time.Second))
	var readyEvent ws.Event
	err = connA.ReadJSON(&readyEvent)
	if err != nil {
		t.Fatalf("student A failed to receive READY event: %v", err)
	}
	if readyEvent.Status != "READY" {
		t.Errorf("expected status READY, got %s", readyEvent.Status)
	}
}

func TestWS_DisconnectAndReconnect(t *testing.T) {
	server, db, tokenService, wsHub := setupWSTestServer(t)
	defer server.Close()
	defer db.Close()

	user, token := createRealTestUser(t, db, tokenService, models.RoleStudent)

	// 1. Initial Connection
	conn1, _, err := dialWS(t, server.URL, token, false)
	if err != nil {
		t.Fatalf("failed initial connection: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if !wsHub.IsUserConnected(user.ID) {
		t.Errorf("expected user to be connected")
	}

	// 2. Disconnect
	_ = conn1.Close()
	time.Sleep(100 * time.Millisecond)

	if wsHub.IsUserConnected(user.ID) {
		t.Errorf("expected user to be unregistered after disconnect")
	}

	// 3. Reconnect
	conn2, _, err := dialWS(t, server.URL, token, false)
	if err != nil {
		t.Fatalf("failed reconnection: %v", err)
	}
	defer conn2.Close()

	time.Sleep(50 * time.Millisecond)
	if !wsHub.IsUserConnected(user.ID) {
		t.Errorf("expected user to be re-registered after reconnect")
	}

	// 4. Send event to reconnected client
	wsHub.SendToUser(user.ID, ws.NewOrderStatusUpdatedEvent("order-rec-1", "CONFIRMED"))

	_ = conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	var event ws.Event
	err = conn2.ReadJSON(&event)
	if err != nil {
		t.Fatalf("reconnected client failed to receive event: %v", err)
	}
	if event.Status != "CONFIRMED" || event.OrderID != "order-rec-1" {
		t.Errorf("unexpected event received: %+v", event)
	}
}
