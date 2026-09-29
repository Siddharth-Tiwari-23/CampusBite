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

	"campusbite/internal/models"
	"campusbite/internal/repository"
)

func TestOrder_CreateAndGet(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	_, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_, adminToken := createRealTestUser(t, db, tokenService, models.RoleAdmin)
	_, otherStudentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	// 1. Attempt to create order from empty cart -> 400 Bad Request
	emptyOrderReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	emptyOrderReq.Header.Set("Authorization", "Bearer "+studentToken)
	emptyOrderW := httptest.NewRecorder()
	router.ServeHTTP(emptyOrderW, emptyOrderReq)

	if emptyOrderW.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request when creating order from empty cart, got %d: %s", emptyOrderW.Code, emptyOrderW.Body.String())
	}

	// 2. Seed menu items and populate cart
	menuRepo := repository.NewMenuRepository(db)
	item1, err := menuRepo.Create(context.Background(), fmt.Sprintf("OrderPizza_%d", time.Now().UnixNano()), "Fresh Pizza", 150.0, true, 20)
	if err != nil {
		t.Fatalf("failed to seed menu item 1: %v", err)
	}
	item2, err := menuRepo.Create(context.Background(), fmt.Sprintf("OrderCoke_%d", time.Now().UnixNano()), "Cold Drink", 40.0, true, 50)
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

	// 3. Create order -> 201 Created
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	createReq.Header.Set("Authorization", "Bearer "+studentToken)
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

	// 4. Verify cart was cleared after order placement
	getCartReq := httptest.NewRequest(http.MethodGet, "/api/v1/cart", nil)
	getCartReq.Header.Set("Authorization", "Bearer "+studentToken)
	getCartW := httptest.NewRecorder()
	router.ServeHTTP(getCartW, getCartReq)

	var cartAfterOrder models.CartResponse
	_ = json.Unmarshal(getCartW.Body.Bytes(), &cartAfterOrder)
	if len(cartAfterOrder.Items) != 0 {
		t.Errorf("expected 0 items in cart after order creation, got %d", len(cartAfterOrder.Items))
	}

	// 5. Get order by ID - Owner Student -> 200 OK
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

	// 6. Get order by ID - Other Student -> 403 Forbidden
	forbiddenReq := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+createdOrder.ID, nil)
	forbiddenReq.Header.Set("Authorization", "Bearer "+otherStudentToken)
	forbiddenW := httptest.NewRecorder()
	router.ServeHTTP(forbiddenW, forbiddenReq)

	if forbiddenW.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for non-owner student, got %d", forbiddenW.Code)
	}

	// 7. Get order by ID - Admin -> 200 OK
	adminGetReq := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+createdOrder.ID, nil)
	adminGetReq.Header.Set("Authorization", "Bearer "+adminToken)
	adminGetW := httptest.NewRecorder()
	router.ServeHTTP(adminGetW, adminGetReq)

	if adminGetW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for Admin viewing order, got %d: %s", adminGetW.Code, adminGetW.Body.String())
	}

	// 8. Non-existent order -> 404
	missingReq := httptest.NewRequest(http.MethodGet, "/api/v1/orders/00000000-0000-0000-0000-000000000000", nil)
	missingReq.Header.Set("Authorization", "Bearer "+studentToken)
	missingW := httptest.NewRecorder()
	router.ServeHTTP(missingW, missingReq)

	if missingW.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for nonexistent order, got %d", missingW.Code)
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
