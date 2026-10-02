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

func TestInventory_ReadAndList(t *testing.T) {
	router, db, _ := setupIntegrationApp(t)
	defer db.Close()

	menuRepo := repository.NewMenuRepository(db)
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("Samosa_%d", time.Now().UnixNano()), "Crispy potato samosa", 15.0, "", true, 40)
	if err != nil {
		t.Fatalf("failed to seed menu item: %v", err)
	}

	// 1. Public GET /api/v1/inventory
	req := httptest.NewRequest(http.MethodGet, "/api/v1/inventory", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for public inventory list, got %d: %s", w.Code, w.Body.String())
	}

	var listResp struct {
		Inventory []models.InventoryItem `json:"inventory"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to decode inventory list: %v", err)
	}
	if len(listResp.Inventory) == 0 {
		t.Fatal("expected at least one inventory record")
	}

	// 2. Public GET /api/v1/inventory/:menuItemId
	itemReq := httptest.NewRequest(http.MethodGet, "/api/v1/inventory/"+item.ID, nil)
	itemW := httptest.NewRecorder()
	router.ServeHTTP(itemW, itemReq)

	if itemW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for single inventory item, got %d: %s", itemW.Code, itemW.Body.String())
	}

	var invItem models.InventoryItem
	_ = json.Unmarshal(itemW.Body.Bytes(), &invItem)
	if invItem.MenuItemID != item.ID {
		t.Errorf("expected menu_item_id %s, got %s", item.ID, invItem.MenuItemID)
	}
	if invItem.Quantity != 40 {
		t.Errorf("expected quantity 40, got %d", invItem.Quantity)
	}

	// 3. Nonexistent menu item inventory -> 404
	missingReq := httptest.NewRequest(http.MethodGet, "/api/v1/inventory/00000000-0000-0000-0000-000000000000", nil)
	missingW := httptest.NewRecorder()
	router.ServeHTTP(missingW, missingReq)

	if missingW.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for missing item inventory, got %d", missingW.Code)
	}
}

func TestInventory_Update(t *testing.T) {
	router, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	studentToken, adminToken := createTestTokens(t, tokenService)

	menuRepo := repository.NewMenuRepository(db)
	item, err := menuRepo.Create(context.Background(), fmt.Sprintf("Chai_%d", time.Now().UnixNano()), "Hot masala tea", 10.0, "", true, 20)
	if err != nil {
		t.Fatalf("failed to seed menu item: %v", err)
	}

	newQty := 50
	updatePayload := models.UpdateInventoryRequest{
		Quantity: &newQty,
	}
	body, _ := json.Marshal(updatePayload)

	// 1. ADMIN updates inventory -> 200 OK
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/inventory/"+item.ID, bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for ADMIN inventory update, got %d: %s", w.Code, w.Body.String())
	}

	var updated models.InventoryItem
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if updated.Quantity != newQty {
		t.Errorf("expected updated quantity %d, got %d", newQty, updated.Quantity)
	}

	// 2. STUDENT updates inventory -> 403 Forbidden
	sReq := httptest.NewRequest(http.MethodPatch, "/api/v1/inventory/"+item.ID, bytes.NewBuffer(body))
	sReq.Header.Set("Authorization", "Bearer "+studentToken)
	sReq.Header.Set("Content-Type", "application/json")
	sW := httptest.NewRecorder()
	router.ServeHTTP(sW, sReq)

	if sW.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for STUDENT inventory update, got %d", sW.Code)
	}

	// 3. Negative quantity rejected -> 400 Bad Request
	negativeQty := -5
	negBody, _ := json.Marshal(models.UpdateInventoryRequest{Quantity: &negativeQty})
	negReq := httptest.NewRequest(http.MethodPatch, "/api/v1/inventory/"+item.ID, bytes.NewBuffer(negBody))
	negReq.Header.Set("Authorization", "Bearer "+adminToken)
	negReq.Header.Set("Content-Type", "application/json")
	negW := httptest.NewRecorder()
	router.ServeHTTP(negW, negReq)

	if negW.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for negative quantity, got %d", negW.Code)
	}

	// 4. Nonexistent menu item update -> 404 Not Found
	missingReq := httptest.NewRequest(http.MethodPatch, "/api/v1/inventory/00000000-0000-0000-0000-000000000000", bytes.NewBuffer(body))
	missingReq.Header.Set("Authorization", "Bearer "+adminToken)
	missingReq.Header.Set("Content-Type", "application/json")
	missingW := httptest.NewRecorder()
	router.ServeHTTP(missingW, missingReq)

	if missingW.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for nonexistent item update, got %d", missingW.Code)
	}
}
