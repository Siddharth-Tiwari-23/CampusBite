package ws_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"campusbite/internal/ws"

	"github.com/gorilla/websocket"
)

func createTestClientConn(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	var serverConn *websocket.Conn
	var serverErr error
	serverReady := make(chan struct{})

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverConn, serverErr = upgrader.Upgrade(w, r, nil)
		close(serverReady)
	}))
	defer s.Close()

	wsURL := "ws" + strings.TrimPrefix(s.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial test ws server: %v", err)
	}

	<-serverReady
	if serverErr != nil {
		t.Fatalf("failed to upgrade test server connection: %v", serverErr)
	}

	return serverConn, clientConn
}

func TestHub_RegisterUnregisterAndCounts(t *testing.T) {
	hub := ws.NewHub()

	serverConn1, clientConn1 := createTestClientConn(t)
	defer serverConn1.Close()
	defer clientConn1.Close()

	serverConn2, clientConn2 := createTestClientConn(t)
	defer serverConn2.Close()
	defer clientConn2.Close()

	client1 := ws.NewClient(hub, serverConn1, "user-1", "STUDENT")
	client2 := ws.NewClient(hub, serverConn2, "user-2", "ADMIN")

	// 1. Initial state
	if hub.GetActiveUserCount() != 0 || hub.GetAdminCount() != 0 {
		t.Errorf("expected 0 users and 0 admins initially")
	}

	// 2. Register student
	hub.Register(client1)
	if !hub.IsUserConnected("user-1") || hub.GetActiveUserCount() != 1 || hub.GetAdminCount() != 0 {
		t.Errorf("expected user-1 connected, 1 user count, 0 admin count")
	}

	// 3. Register admin
	hub.Register(client2)
	if !hub.IsUserConnected("user-2") || hub.GetActiveUserCount() != 2 || hub.GetAdminCount() != 1 {
		t.Errorf("expected user-2 connected, 2 users, 1 admin")
	}

	// 4. Unregister student
	hub.Unregister(client1)
	if hub.IsUserConnected("user-1") || hub.GetActiveUserCount() != 1 {
		t.Errorf("expected user-1 unregistered, 1 user count")
	}

	// 5. Unregister admin
	hub.Unregister(client2)
	if hub.IsUserConnected("user-2") || hub.GetActiveUserCount() != 0 || hub.GetAdminCount() != 0 {
		t.Errorf("expected all unregistered")
	}
}

func TestHub_SendToUser_Isolation(t *testing.T) {
	hub := ws.NewHub()

	serverConn1, clientConn1 := createTestClientConn(t)
	defer serverConn1.Close()
	defer clientConn1.Close()

	serverConn2, clientConn2 := createTestClientConn(t)
	defer serverConn2.Close()
	defer clientConn2.Close()

	client1 := ws.NewClient(hub, serverConn1, "student-a", "STUDENT")
	client2 := ws.NewClient(hub, serverConn2, "student-b", "STUDENT")

	hub.Register(client1)
	hub.Register(client2)

	go client1.WritePump()
	go client2.WritePump()

	// Send event strictly to student A
	testEvent := ws.NewOrderStatusUpdatedEvent("order-101", "READY")
	hub.SendToUser("student-a", testEvent)

	// Student A should receive message
	_ = clientConn1.SetReadDeadline(time.Now().Add(2 * time.Second))
	var receivedEvent ws.Event
	err := clientConn1.ReadJSON(&receivedEvent)
	if err != nil {
		t.Fatalf("student A failed to receive event: %v", err)
	}

	if receivedEvent.Type != ws.EventOrderStatusUpdated || receivedEvent.OrderID != "order-101" || receivedEvent.Status != "READY" {
		t.Errorf("student A received unexpected event: %+v", receivedEvent)
	}

	// Student B should NOT receive anything
	_ = clientConn2.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	var eventB ws.Event
	err = clientConn2.ReadJSON(&eventB)
	if err == nil {
		t.Errorf("student B should not have received event, but received: %+v", eventB)
	}
}

func TestHub_BroadcastToAdmins(t *testing.T) {
	hub := ws.NewHub()

	serverConn1, clientConn1 := createTestClientConn(t)
	defer serverConn1.Close()
	defer clientConn1.Close()

	serverConn2, clientConn2 := createTestClientConn(t)
	defer serverConn2.Close()
	defer clientConn2.Close()

	adminClient := ws.NewClient(hub, serverConn1, "admin-user", "ADMIN")
	studentClient := ws.NewClient(hub, serverConn2, "student-user", "STUDENT")

	hub.Register(adminClient)
	hub.Register(studentClient)

	go adminClient.WritePump()
	go studentClient.WritePump()

	adminEvent := ws.NewOrderCreatedEvent("order-500", "student-user", 350.0)
	hub.BroadcastToAdmins(adminEvent)

	// Admin receives event
	_ = clientConn1.SetReadDeadline(time.Now().Add(2 * time.Second))
	var receivedAdminEvent ws.Event
	err := clientConn1.ReadJSON(&receivedAdminEvent)
	if err != nil {
		t.Fatalf("admin failed to receive admin broadcast: %v", err)
	}
	if receivedAdminEvent.Type != ws.EventNewOrder || receivedAdminEvent.OrderID != "order-500" {
		t.Errorf("admin received unexpected event: %+v", receivedAdminEvent)
	}

	// Student does NOT receive admin event
	_ = clientConn2.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	var receivedStudentEvent ws.Event
	err = clientConn2.ReadJSON(&receivedStudentEvent)
	if err == nil {
		t.Errorf("student should not have received admin event, but got: %+v", receivedStudentEvent)
	}
}

func TestHub_ConcurrentOperations_RaceSafe(t *testing.T) {
	hub := ws.NewHub()
	numUsers := 20
	var wg sync.WaitGroup

	// Concurrently register clients
	for i := 0; i < numUsers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			serverConn, clientConn := createTestClientConn(t)
			defer serverConn.Close()
			defer clientConn.Close()

			role := "STUDENT"
			if idx%3 == 0 {
				role = "ADMIN"
			}
			client := ws.NewClient(hub, serverConn, fmt.Sprintf("concurrent-user-%d", idx), role)
			hub.Register(client)

			// Publish events concurrently
			hub.SendToUser(fmt.Sprintf("concurrent-user-%d", idx), ws.NewOrderStatusUpdatedEvent("ord-1", "PREPARING"))
			hub.BroadcastToAdmins(ws.NewOrderCreatedEvent("ord-2", fmt.Sprintf("concurrent-user-%d", idx), 100.0))
			hub.Broadcast(ws.NewNotificationEvent("hello", nil))

			hub.Unregister(client)
		}(i)
	}

	wg.Wait()
}
