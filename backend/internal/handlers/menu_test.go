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
	"campusbite/internal/models"
	"campusbite/internal/repository"
)

func createTestTokens(t *testing.T, tokenService *auth.TokenService) (studentToken string, adminToken string) {
	sToken, err := tokenService.GenerateToken("550e8400-e29b-41d4-a716-446655440001", models.RoleStudent)
	if err != nil {
		t.Fatalf("failed to generate student token: %v", err)
	}

	aToken, err := tokenService.GenerateToken("550e8400-e29b-41d4-a716-446655440002", models.RoleAdmin)
	if err != nil {
		t.Fatalf("failed to generate admin token: %v", err)
	}

	return sToken, aToken
}

func TestMenu_Create(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	studentToken, adminToken := createTestTokens(t, tokenService)

	validPrice := 120.50
	initQty := 25
	isAvail := true

	payload := models.CreateMenuItemRequest{
		Name:            fmt.Sprintf("Burger_%d", time.Now().UnixNano()),
		Description:     "Delicious grilled chicken burger",
		Price:           &validPrice,
		IsAvailable:     &isAvail,
		InitialQuantity: &initQty,
	}
	body, _ := json.Marshal(payload)

	// 1. ADMIN creates menu item -> 201 Created
	req := httptest.NewRequest(http.MethodPost, "/api/v1/menu", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for ADMIN, got %d: %s", w.Code, w.Body.String())
	}

	var created models.MenuItem
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to unmarshal created item: %v", err)
	}
	if created.Name != payload.Name {
		t.Errorf("expected name %s, got %s", payload.Name, created.Name)
	}
	if created.Price != validPrice {
		t.Errorf("expected price %f, got %f", validPrice, created.Price)
	}

	// 2. STUDENT creates menu item -> 403 Forbidden
	sReq := httptest.NewRequest(http.MethodPost, "/api/v1/menu", bytes.NewBuffer(body))
	sReq.Header.Set("Authorization", "Bearer "+studentToken)
	sReq.Header.Set("Content-Type", "application/json")
	sW := httptest.NewRecorder()
	router.ServeHTTP(sW, sReq)

	if sW.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for STUDENT, got %d", sW.Code)
	}

	// 3. Unauthenticated request -> 401 Unauthorized
	unauthReq := httptest.NewRequest(http.MethodPost, "/api/v1/menu", bytes.NewBuffer(body))
	unauthReq.Header.Set("Content-Type", "application/json")
	unauthW := httptest.NewRecorder()
	router.ServeHTTP(unauthW, unauthReq)

	if unauthW.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized without token, got %d", unauthW.Code)
	}

	// 4. Invalid creation inputs
	negativePrice := -10.0
	badInputs := []struct {
		name    string
		payload models.CreateMenuItemRequest
	}{
		{"empty name", models.CreateMenuItemRequest{Name: "", Price: &validPrice}},
		{"nil price", models.CreateMenuItemRequest{Name: "Item", Price: nil}},
		{"negative price", models.CreateMenuItemRequest{Name: "Item", Price: &negativePrice}},
	}

	for _, tc := range badInputs {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(tc.payload)
			badReq := httptest.NewRequest(http.MethodPost, "/api/v1/menu", bytes.NewBuffer(b))
			badReq.Header.Set("Authorization", "Bearer "+adminToken)
			badReq.Header.Set("Content-Type", "application/json")
			badW := httptest.NewRecorder()
			router.ServeHTTP(badW, badReq)

			if badW.Code != http.StatusBadRequest {
				t.Errorf("expected 400 Bad Request for %s, got %d: %s", tc.name, badW.Code, badW.Body.String())
			}
		})
	}
}

func TestMenu_GetAndList(t *testing.T) {
	router, db, _ := setupIntegrationApp(t)
	defer db.Close()

	menuRepo := repository.NewMenuRepository(db)
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("Pizza_%d", time.Now().UnixNano()), "Cheesy pizza", 250.0, "", true, 10)
	if err != nil {
		t.Fatalf("failed to seed menu item: %v", err)
	}

	// 1. Public GET /api/v1/menu
	req := httptest.NewRequest(http.MethodGet, "/api/v1/menu", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for public menu list, got %d: %s", w.Code, w.Body.String())
	}

	var listResp struct {
		Items []models.MenuItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to decode menu list: %v", err)
	}
	if len(listResp.Items) == 0 {
		t.Fatal("expected at least one menu item in list")
	}

	// 2. Public GET /api/v1/menu/:id
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/menu/"+item.ID, nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for get item by ID, got %d", getW.Code)
	}

	var fetched models.MenuItem
	_ = json.Unmarshal(getW.Body.Bytes(), &fetched)
	if fetched.ID != item.ID {
		t.Errorf("expected ID %s, got %s", item.ID, fetched.ID)
	}

	// 3. Nonexistent menu item -> 404
	missingReq := httptest.NewRequest(http.MethodGet, "/api/v1/menu/00000000-0000-0000-0000-000000000000", nil)
	missingW := httptest.NewRecorder()
	router.ServeHTTP(missingW, missingReq)

	if missingW.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for nonexistent ID, got %d", missingW.Code)
	}

	// 4. Invalid UUID format -> 400
	invalidReq := httptest.NewRequest(http.MethodGet, "/api/v1/menu/invalid-uuid", nil)
	invalidW := httptest.NewRecorder()
	router.ServeHTTP(invalidW, invalidReq)

	if invalidW.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid UUID format, got %d", invalidW.Code)
	}
}

func TestMenu_UpdateAndDelete(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	studentToken, adminToken := createTestTokens(t, tokenService)

	menuRepo := repository.NewMenuRepository(db)
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("Roll_%d", time.Now().UnixNano()), "Egg roll", 60.0, "", true, 5)
	if err != nil {
		t.Fatalf("failed to seed menu item: %v", err)
	}

	// 1. ADMIN updates menu item -> 200 OK
	updatedName := "Egg Cheese Roll"
	updatedPrice := 75.0
	isAvail := false

	updatePayload := models.UpdateMenuItemRequest{
		Name:        &updatedName,
		Price:       &updatedPrice,
		IsAvailable: &isAvail,
	}
	body, _ := json.Marshal(updatePayload)

	updateReq := httptest.NewRequest(http.MethodPatch, "/api/v1/menu/"+item.ID, bytes.NewBuffer(body))
	updateReq.Header.Set("Authorization", "Bearer "+adminToken)
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	router.ServeHTTP(updateW, updateReq)

	if updateW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for ADMIN update, got %d: %s", updateW.Code, updateW.Body.String())
	}

	var updatedItem models.MenuItem
	_ = json.Unmarshal(updateW.Body.Bytes(), &updatedItem)
	if updatedItem.Name != updatedName {
		t.Errorf("expected name %s, got %s", updatedName, updatedItem.Name)
	}
	if updatedItem.Price != updatedPrice {
		t.Errorf("expected price %f, got %f", updatedPrice, updatedItem.Price)
	}
	if updatedItem.IsAvailable != isAvail {
		t.Errorf("expected is_available %v, got %v", isAvail, updatedItem.IsAvailable)
	}

	// 2. STUDENT updates menu item -> 403 Forbidden
	sUpdateReq := httptest.NewRequest(http.MethodPatch, "/api/v1/menu/"+item.ID, bytes.NewBuffer(body))
	sUpdateReq.Header.Set("Authorization", "Bearer "+studentToken)
	sUpdateReq.Header.Set("Content-Type", "application/json")
	sUpdateW := httptest.NewRecorder()
	router.ServeHTTP(sUpdateW, sUpdateReq)

	if sUpdateW.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for STUDENT update, got %d", sUpdateW.Code)
	}

	// 3. STUDENT deletes menu item -> 403 Forbidden
	sDelReq := httptest.NewRequest(http.MethodDelete, "/api/v1/menu/"+item.ID, nil)
	sDelReq.Header.Set("Authorization", "Bearer "+studentToken)
	sDelW := httptest.NewRecorder()
	router.ServeHTTP(sDelW, sDelReq)

	if sDelW.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for STUDENT delete, got %d", sDelW.Code)
	}

	// 4. ADMIN deletes menu item -> 200 OK
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/menu/"+item.ID, nil)
	delReq.Header.Set("Authorization", "Bearer "+adminToken)
	delW := httptest.NewRecorder()
	router.ServeHTTP(delW, delReq)

	if delW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for ADMIN delete, got %d: %s", delW.Code, delW.Body.String())
	}

	// 5. Verify deleted item is not found -> 404
	checkReq := httptest.NewRequest(http.MethodGet, "/api/v1/menu/"+item.ID, nil)
	checkW := httptest.NewRecorder()
	router.ServeHTTP(checkW, checkReq)

	if checkW.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found after deletion, got %d", checkW.Code)
	}
}
