package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"campusbite/internal/auth"
	"campusbite/internal/database"
	"campusbite/internal/models"
	"campusbite/internal/repository"
)

var userSeq uint64

func createRealTestUser(t *testing.T, db *database.DB, tokenService *auth.TokenService, role string) (*models.User, string) {
	seq := atomic.AddUint64(&userSeq, 1)
	userRepo := repository.NewUserRepository(db)
	user, err := userRepo.Create(
		context.Background(),
		"Test User",
		fmt.Sprintf("user_%d_%d_%d@campusbite.internal", time.Now().UnixNano(), seq, rand.Intn(100000)),
		"hashed_pass_placeholder",
		role,
	)
	if err != nil {
		t.Fatalf("failed to create real test user: %v", err)
	}
	token, err := tokenService.GenerateToken(user.ID, user.Role)
	if err != nil {
		t.Fatalf("failed to generate token for test user: %v", err)
	}
	return user, token
}

func TestCart_AddAndGet(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	_, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	menuRepo := repository.NewMenuRepository(db)
	item1, err := menuRepo.Create(context.Background(), fmt.Sprintf("CartItem1_%d", time.Now().UnixNano()), "Item 1", 50.0, "", true, 10)
	if err != nil {
		t.Fatalf("failed to seed menu item: %v", err)
	}

	item2, err := menuRepo.Create(context.Background(), fmt.Sprintf("CartItem2_%d", time.Now().UnixNano()), "Item 2", 75.0, "", true, 10)
	if err != nil {
		t.Fatalf("failed to seed menu item: %v", err)
	}

	// 1. Initial cart should be empty
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cart", nil)
	req.Header.Set("Authorization", "Bearer "+studentToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for GET cart, got %d: %s", w.Code, w.Body.String())
	}

	var cart models.CartResponse
	if err := json.Unmarshal(w.Body.Bytes(), &cart); err != nil {
		t.Fatalf("failed to decode cart response: %v", err)
	}
	if len(cart.Items) != 0 {
		t.Errorf("expected empty initial cart, got %d items", len(cart.Items))
	}

	// 2. Add item1 with quantity 2
	addPayload := models.AddToCartRequest{
		MenuItemID: item1.ID,
		Quantity:   2,
	}
	body, _ := json.Marshal(addPayload)
	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(body))
	addReq.Header.Set("Authorization", "Bearer "+studentToken)
	addReq.Header.Set("Content-Type", "application/json")
	addW := httptest.NewRecorder()
	router.ServeHTTP(addW, addReq)

	if addW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on add item to cart, got %d: %s", addW.Code, addW.Body.String())
	}

	var updatedCart models.CartResponse
	if err := json.Unmarshal(addW.Body.Bytes(), &updatedCart); err != nil {
		t.Fatalf("failed to unmarshal cart: %v", err)
	}

	if len(updatedCart.Items) != 1 {
		t.Fatalf("expected 1 item in cart, got %d", len(updatedCart.Items))
	}
	if updatedCart.Items[0].Quantity != 2 {
		t.Errorf("expected quantity 2, got %d", updatedCart.Items[0].Quantity)
	}
	if updatedCart.Items[0].Subtotal != 100.0 {
		t.Errorf("expected subtotal 100.0, got %f", updatedCart.Items[0].Subtotal)
	}
	if updatedCart.TotalAmount != 100.0 {
		t.Errorf("expected total amount 100.0, got %f", updatedCart.TotalAmount)
	}

	// 3. Add same item1 again with quantity 1 -> should increment to 3
	addPayload2 := models.AddToCartRequest{
		MenuItemID: item1.ID,
		Quantity:   1,
	}
	body2, _ := json.Marshal(addPayload2)
	addReq2 := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(body2))
	addReq2.Header.Set("Authorization", "Bearer "+studentToken)
	addReq2.Header.Set("Content-Type", "application/json")
	addW2 := httptest.NewRecorder()
	router.ServeHTTP(addW2, addReq2)

	if addW2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on increment cart item, got %d: %s", addW2.Code, addW2.Body.String())
	}

	_ = json.Unmarshal(addW2.Body.Bytes(), &updatedCart)
	if updatedCart.Items[0].Quantity != 3 {
		t.Errorf("expected quantity 3 after increment, got %d", updatedCart.Items[0].Quantity)
	}
	if updatedCart.TotalAmount != 150.0 {
		t.Errorf("expected total amount 150.0, got %f", updatedCart.TotalAmount)
	}

	// 4. Add item2 with quantity 1
	addPayload3 := models.AddToCartRequest{
		MenuItemID: item2.ID,
		Quantity:   1,
	}
	body3, _ := json.Marshal(addPayload3)
	addReq3 := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(body3))
	addReq3.Header.Set("Authorization", "Bearer "+studentToken)
	addReq3.Header.Set("Content-Type", "application/json")
	addW3 := httptest.NewRecorder()
	router.ServeHTTP(addW3, addReq3)

	if addW3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", addW3.Code)
	}

	_ = json.Unmarshal(addW3.Body.Bytes(), &updatedCart)
	if len(updatedCart.Items) != 2 {
		t.Fatalf("expected 2 items in cart, got %d", len(updatedCart.Items))
	}
	if updatedCart.TotalAmount != 225.0 {
		t.Errorf("expected total amount 225.0 (150 + 75), got %f", updatedCart.TotalAmount)
	}

	// 5. Unauthenticated request -> 401
	unauthReq := httptest.NewRequest(http.MethodGet, "/api/v1/cart", nil)
	unauthW := httptest.NewRecorder()
	router.ServeHTTP(unauthW, unauthReq)
	if unauthW.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for unauthenticated GET cart, got %d", unauthW.Code)
	}

	// 6. Add non-existent item -> 404
	missingPayload := models.AddToCartRequest{
		MenuItemID: "00000000-0000-0000-0000-000000000000",
		Quantity:   1,
	}
	missingBody, _ := json.Marshal(missingPayload)
	missingReq := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(missingBody))
	missingReq.Header.Set("Authorization", "Bearer "+studentToken)
	missingReq.Header.Set("Content-Type", "application/json")
	missingW := httptest.NewRecorder()
	router.ServeHTTP(missingW, missingReq)
	if missingW.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent menu item, got %d", missingW.Code)
	}

	// 7. Add with quantity <= 0 -> 400
	badQtyPayload := models.AddToCartRequest{
		MenuItemID: item1.ID,
		Quantity:   0,
	}
	badQtyBody, _ := json.Marshal(badQtyPayload)
	badQtyReq := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(badQtyBody))
	badQtyReq.Header.Set("Authorization", "Bearer "+studentToken)
	badQtyReq.Header.Set("Content-Type", "application/json")
	badQtyW := httptest.NewRecorder()
	router.ServeHTTP(badQtyW, badQtyReq)
	if badQtyW.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-positive quantity, got %d", badQtyW.Code)
	}
}

func TestCart_UpdateAndDelete(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	_, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	menuRepo := repository.NewMenuRepository(db)
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("UpdateItem_%d", time.Now().UnixNano()), "Test Item", 40.0, "", true, 10)
	if err != nil {
		t.Fatalf("failed to seed menu item: %v", err)
	}

	// 1. Add item
	addPayload := models.AddToCartRequest{MenuItemID: item.ID, Quantity: 2}
	b, _ := json.Marshal(addPayload)
	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(b))
	addReq.Header.Set("Authorization", "Bearer "+studentToken)
	addReq.Header.Set("Content-Type", "application/json")
	addW := httptest.NewRecorder()
	router.ServeHTTP(addW, addReq)
	if addW.Code != http.StatusOK {
		t.Fatalf("failed to add item: %s", addW.Body.String())
	}

	// 2. Update quantity to 5
	updatePayload := models.UpdateCartItemRequest{Quantity: 5}
	ub, _ := json.Marshal(updatePayload)
	updateReq := httptest.NewRequest(http.MethodPatch, "/api/v1/cart/items/"+item.ID, bytes.NewBuffer(ub))
	updateReq.Header.Set("Authorization", "Bearer "+studentToken)
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	router.ServeHTTP(updateW, updateReq)

	if updateW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on update item, got %d: %s", updateW.Code, updateW.Body.String())
	}

	var updatedCart models.CartResponse
	_ = json.Unmarshal(updateW.Body.Bytes(), &updatedCart)
	if len(updatedCart.Items) != 1 || updatedCart.Items[0].Quantity != 5 {
		t.Errorf("expected item quantity 5, got %d", updatedCart.Items[0].Quantity)
	}
	if updatedCart.TotalAmount != 200.0 {
		t.Errorf("expected total amount 200.0 (5 * 40), got %f", updatedCart.TotalAmount)
	}

	// 3. Update quantity with bad value <= 0 -> 400
	badUpdate := models.UpdateCartItemRequest{Quantity: -1}
	bub, _ := json.Marshal(badUpdate)
	badUpdateReq := httptest.NewRequest(http.MethodPatch, "/api/v1/cart/items/"+item.ID, bytes.NewBuffer(bub))
	badUpdateReq.Header.Set("Authorization", "Bearer "+studentToken)
	badUpdateReq.Header.Set("Content-Type", "application/json")
	badUpdateW := httptest.NewRecorder()
	router.ServeHTTP(badUpdateW, badUpdateReq)
	if badUpdateW.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative quantity update, got %d", badUpdateW.Code)
	}

	// 4. Delete single item from cart -> 200 OK
	delItemReq := httptest.NewRequest(http.MethodDelete, "/api/v1/cart/items/"+item.ID, nil)
	delItemReq.Header.Set("Authorization", "Bearer "+studentToken)
	delItemW := httptest.NewRecorder()
	router.ServeHTTP(delItemW, delItemReq)

	if delItemW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on delete cart item, got %d: %s", delItemW.Code, delItemW.Body.String())
	}

	// 5. Verify cart is empty
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/cart", nil)
	getReq.Header.Set("Authorization", "Bearer "+studentToken)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)

	var emptyCart models.CartResponse
	_ = json.Unmarshal(getW.Body.Bytes(), &emptyCart)
	if len(emptyCart.Items) != 0 {
		t.Errorf("expected 0 items in cart, got %d", len(emptyCart.Items))
	}
	if emptyCart.TotalAmount != 0 {
		t.Errorf("expected total amount 0, got %f", emptyCart.TotalAmount)
	}
}

func TestCart_UUIDValidationAndFieldCompatibility(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	_, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	menuRepo := repository.NewMenuRepository(db)
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("UUIDTest_%d", time.Now().UnixNano()), "Test Item", 50.0, "/images/menu/veg_momos.jpg", true, 20)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}

	// 1. Invalid UUID in menu_item_id -> 400 "invalid menu item ID format"
	invalidBody := []byte(`{"menu_item_id": "invalid-uuid-string", "quantity": 1}`)
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(invalidBody))
	req1.Header.Set("Authorization", "Bearer "+studentToken)
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid menu_item_id UUID, got %d", w1.Code)
	}
	if !strings.Contains(w1.Body.String(), "invalid menu item ID format") {
		t.Errorf("expected 'invalid menu item ID format' error, got %s", w1.Body.String())
	}

	// 2. Invalid UUID in item_id fallback -> 400 "invalid menu item ID format"
	invalidBody2 := []byte(`{"item_id": "12345", "quantity": 1}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(invalidBody2))
	req2.Header.Set("Authorization", "Bearer "+studentToken)
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid item_id UUID, got %d", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), "invalid menu item ID format") {
		t.Errorf("expected 'invalid menu item ID format' error, got %s", w2.Body.String())
	}

	// 3. Valid menu_item_id -> 200 OK with ImageURL populated
	validBody := []byte(fmt.Sprintf(`{"menu_item_id": "%s", "quantity": 2}`, item.ID))
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(validBody))
	req3.Header.Set("Authorization", "Bearer "+studentToken)
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid menu_item_id, got %d: %s", w3.Code, w3.Body.String())
	}
	var cartResp models.CartResponse
	if err := json.Unmarshal(w3.Body.Bytes(), &cartResp); err != nil {
		t.Fatalf("failed to parse cart response: %v", err)
	}
	if len(cartResp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(cartResp.Items))
	}
	if cartResp.Items[0].ImageURL != "/images/menu/veg_momos.jpg" {
		t.Errorf("expected image_url '/images/menu/veg_momos.jpg', got '%s'", cartResp.Items[0].ImageURL)
	}

	// 4. Valid item_id fallback -> 200 OK
	validBodyFallback := []byte(fmt.Sprintf(`{"item_id": "%s", "quantity": 1}`, item.ID))
	req4 := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(validBodyFallback))
	req4.Header.Set("Authorization", "Bearer "+studentToken)
	req4.Header.Set("Content-Type", "application/json")
	w4 := httptest.NewRecorder()
	router.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid item_id fallback, got %d: %s", w4.Code, w4.Body.String())
	}

	// 5. Update via PUT route -> 200 OK
	updateBody := []byte(`{"quantity": 5}`)
	req5 := httptest.NewRequest(http.MethodPut, "/api/v1/cart/items/"+item.ID, bytes.NewBuffer(updateBody))
	req5.Header.Set("Authorization", "Bearer "+studentToken)
	req5.Header.Set("Content-Type", "application/json")
	w5 := httptest.NewRecorder()
	router.ServeHTTP(w5, req5)
	if w5.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for PUT update, got %d: %s", w5.Code, w5.Body.String())
	}
}
