package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"campusbite/internal/cache"
	"campusbite/internal/models"
	"campusbite/internal/routes"
	"campusbite/internal/service"
)

func TestE2E_MenuAndAddToCartVerification(t *testing.T) {
	_, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	rzpService := service.NewRazorpayService("rzp_test_campusbite_mock_key", "rzp_test_campusbite_mock_secret", "rzp_test_campusbite_mock_webhook_secret")
	appRouter := routes.SetupRouter(db, tokenService, rzpService, cache.NewNoOpCache())

	// 1. Verify GET /api/v1/menu returns valid UUID IDs and active items
	reqMenu := httptest.NewRequest(http.MethodGet, "/api/v1/menu", nil)
	wMenu := httptest.NewRecorder()
	appRouter.ServeHTTP(wMenu, reqMenu)

	if wMenu.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for GET /api/v1/menu, got %d: %s", wMenu.Code, wMenu.Body.String())
	}

	var menuResp struct {
		Items []models.MenuItem `json:"items"`
	}
	if err := json.Unmarshal(wMenu.Body.Bytes(), &menuResp); err != nil {
		t.Fatalf("failed to parse menu items: %v", err)
	}

	menuItems := menuResp.Items
	if len(menuItems) < 3 {
		t.Fatalf("expected at least 3 menu items, got %d", len(menuItems))
	}

	for _, item := range menuItems {
		if !isValidUUIDString(item.ID) {
			t.Fatalf("menu item '%s' has invalid UUID ID: '%s'", item.Name, item.ID)
		}
		// Production catalog items must have verified local image assets
		if item.ImageURL != "" && !strings.HasPrefix(item.ImageURL, "/images/menu/") {
			t.Errorf("menu item '%s' has unexpected image_url format: %s", item.Name, item.ImageURL)
		}
	}
	t.Logf("✓ Verified %d menu items from GET /api/v1/menu; all possess valid UUID IDs.", len(menuItems))

	// 2. Authenticate as a student user
	_, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	// 3. Clear cart first
	reqClear := httptest.NewRequest(http.MethodDelete, "/api/v1/cart", nil)
	reqClear.Header.Set("Authorization", "Bearer "+studentToken)
	wClear := httptest.NewRecorder()
	appRouter.ServeHTTP(wClear, reqClear)
	if wClear.Code != http.StatusOK {
		t.Fatalf("failed to clear cart: %d", wClear.Code)
	}

	// 4. Add at least 3 distinct products to the tray (simulating frontend payload)
	selectedItems := menuItems[:3]
	quantities := []int{2, 1, 3}

	for i, item := range selectedItems {
		qty := quantities[i]
		// Frontend cartService sends menu_item_id and item_id
		addBody, _ := json.Marshal(map[string]interface{}{
			"menu_item_id": item.ID,
			"item_id":      item.ID,
			"quantity":     qty,
		})

		reqAdd := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", bytes.NewBuffer(addBody))
		reqAdd.Header.Set("Authorization", "Bearer "+studentToken)
		reqAdd.Header.Set("Content-Type", "application/json")
		wAdd := httptest.NewRecorder()
		appRouter.ServeHTTP(wAdd, reqAdd)

		if wAdd.Code != http.StatusOK {
			t.Fatalf("failed to add product %s to tray: status %d, body: %s", item.Name, wAdd.Code, wAdd.Body.String())
		}
		t.Logf("✓ Successfully added to tray: '%s' (Qty: %d, ID: %s)", item.Name, qty, item.ID)
	}

	// 5. Verify GET /api/v1/cart reflects the selected items and correct prices/quantities
	reqCart := httptest.NewRequest(http.MethodGet, "/api/v1/cart", nil)
	reqCart.Header.Set("Authorization", "Bearer "+studentToken)
	wCart := httptest.NewRecorder()
	appRouter.ServeHTTP(wCart, reqCart)

	if wCart.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for GET /api/v1/cart, got %d: %s", wCart.Code, wCart.Body.String())
	}

	var cart models.CartResponse
	if err := json.Unmarshal(wCart.Body.Bytes(), &cart); err != nil {
		t.Fatalf("failed to parse cart response: %v", err)
	}

	if len(cart.Items) != 3 {
		t.Fatalf("expected 3 items in cart, got %d", len(cart.Items))
	}

	var expectedTotal float64
	for i, item := range selectedItems {
		qty := quantities[i]
		found := false
		for _, cartItem := range cart.Items {
			if cartItem.MenuItemID == item.ID {
				found = true
				if cartItem.Quantity != qty {
					t.Errorf("item '%s': expected quantity %d, got %d", item.Name, qty, cartItem.Quantity)
				}
				if cartItem.Price != item.Price {
					t.Errorf("item '%s': expected price %.2f, got %.2f", item.Name, item.Price, cartItem.Price)
				}
				expectedSubtotal := float64(qty) * item.Price
				if cartItem.Subtotal != expectedSubtotal {
					t.Errorf("item '%s': expected subtotal %.2f, got %.2f", item.Name, expectedSubtotal, cartItem.Subtotal)
				}
				expectedTotal += expectedSubtotal
				break
			}
		}
		if !found {
			t.Errorf("expected product '%s' in cart, but was not found", item.Name)
		}
	}

	if cart.TotalAmount != expectedTotal {
		t.Errorf("expected total cart amount %.2f, got %.2f", expectedTotal, cart.TotalAmount)
	}

	t.Logf("✓ Cart verified: 3 items present with exact quantities, prices, and total %.2f", cart.TotalAmount)
	_ = context.Background()
}

func isValidUUIDString(u string) bool {
	if len(u) != 36 {
		return false
	}
	for i, c := range u {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}
