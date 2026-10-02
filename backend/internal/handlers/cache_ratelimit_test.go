package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"campusbite/internal/auth"
	"campusbite/internal/cache"
	"campusbite/internal/database"
	"campusbite/internal/models"
	"campusbite/internal/repository"
	"campusbite/internal/routes"
	"campusbite/internal/service"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func setupTestAppWithRedis(t *testing.T) (*gin.Engine, *database.DB, *auth.TokenService, *cache.RedisCache, *miniredis.Miniredis) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	redisCache := cache.NewRedisCacheFromClient(client)

	_, db, tokenService := setupIntegrationApp(t)
	rzpService := service.NewRazorpayService("rzp_test_campusbite_mock_key", "rzp_test_campusbite_mock_secret", "rzp_test_campusbite_mock_webhook_secret")

	router := routes.SetupRouter(db, tokenService, rzpService, redisCache)
	return router, db, tokenService, redisCache, s
}

func TestMenu_CacheAsideAndInvalidation(t *testing.T) {
	router, db, tokenService, redisCache, s := setupTestAppWithRedis(t)
	defer s.Close()
	defer db.Close()
	defer redisCache.Close()

	_, adminToken := createTestTokens(t, tokenService)

	// Create initial item in PostgreSQL
	menuRepo := repository.NewMenuRepository(db)
	item1, err := menuRepo.Create(context.Background(), fmt.Sprintf("CachedBurger_%d", time.Now().UnixNano()), "Burger", 120.0, "", true, 20)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}

	// 1. Initial Request -> Cache MISS, populates Redis
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/menu", nil)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on first menu get, got %d", w1.Code)
	}
	if cacheHeader := w1.Header().Get("X-Cache"); cacheHeader != "MISS" {
		t.Errorf("expected X-Cache: MISS on initial request, got '%s'", cacheHeader)
	}

	// Verify Redis has key stored
	cachedVal, err := redisCache.Get(context.Background(), cache.KeyMenuAvailable)
	if err != nil || cachedVal == "" {
		t.Fatalf("expected Redis to contain key '%s', got err: %v", cache.KeyMenuAvailable, err)
	}

	// 2. Second Request -> Cache HIT
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/menu", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on second menu get, got %d", w2.Code)
	}
	if cacheHeader := w2.Header().Get("X-Cache"); cacheHeader != "HIT" {
		t.Errorf("expected X-Cache: HIT on second request, got '%s'", cacheHeader)
	}

	// 3. Admin creates new item via POST /api/v1/menu -> Invalidates Cache
	validPrice := 80.0
	initQty := 15
	isAvail := true
	createPayload := models.CreateMenuItemRequest{
		Name:            fmt.Sprintf("CachedFries_%d", time.Now().UnixNano()),
		Description:     "Crispy Fries",
		Price:           &validPrice,
		IsAvailable:     &isAvail,
		InitialQuantity: &initQty,
	}
	createJSON, _ := json.Marshal(createPayload)
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/v1/menu", bytes.NewBuffer(createJSON))
	reqCreate.Header.Set("Authorization", "Bearer "+adminToken)
	reqCreate.Header.Set("Content-Type", "application/json")
	wCreate := httptest.NewRecorder()
	router.ServeHTTP(wCreate, reqCreate)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on item create, got %d: %s", wCreate.Code, wCreate.Body.String())
	}

	// Verify cache key was deleted
	_, err = redisCache.Get(context.Background(), cache.KeyMenuAvailable)
	if err != cache.ErrCacheMiss {
		t.Errorf("expected cache key to be deleted after create, got err: %v", err)
	}

	// 4. Next GET -> Cache MISS and repopulates
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/menu", nil)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	if w3.Header().Get("X-Cache") != "MISS" {
		t.Errorf("expected X-Cache: MISS after invalidation, got '%s'", w3.Header().Get("X-Cache"))
	}

	// 5. Admin updates item via PATCH /api/v1/menu/:id -> Invalidates Cache
	newPrice := 140.0
	updatePayload := models.UpdateMenuItemRequest{
		Price: &newPrice,
	}
	updateJSON, _ := json.Marshal(updatePayload)
	reqUpdate := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/v1/menu/%s", item1.ID), bytes.NewBuffer(updateJSON))
	reqUpdate.Header.Set("Authorization", "Bearer "+adminToken)
	reqUpdate.Header.Set("Content-Type", "application/json")
	wUpdate := httptest.NewRecorder()
	router.ServeHTTP(wUpdate, reqUpdate)
	if wUpdate.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on update, got %d: %s", wUpdate.Code, wUpdate.Body.String())
	}

	// Verify cache key was deleted
	_, err = redisCache.Get(context.Background(), cache.KeyMenuAvailable)
	if err != cache.ErrCacheMiss {
		t.Errorf("expected cache key to be deleted after update, got err: %v", err)
	}

	// 6. Admin updates inventory via PATCH /api/v1/inventory/:menuItemId -> Invalidates Cache
	// First populate cache again
	wPopulate := httptest.NewRecorder()
	router.ServeHTTP(wPopulate, httptest.NewRequest(http.MethodGet, "/api/v1/menu", nil))

	newQty := 50
	invPayload := models.UpdateInventoryRequest{
		Quantity: &newQty,
	}
	invJSON, _ := json.Marshal(invPayload)
	reqInv := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/v1/inventory/%s", item1.ID), bytes.NewBuffer(invJSON))
	reqInv.Header.Set("Authorization", "Bearer "+adminToken)
	reqInv.Header.Set("Content-Type", "application/json")
	wInv := httptest.NewRecorder()
	router.ServeHTTP(wInv, reqInv)
	if wInv.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on inventory update, got %d: %s", wInv.Code, wInv.Body.String())
	}

	// Verify cache key was deleted
	_, err = redisCache.Get(context.Background(), cache.KeyMenuAvailable)
	if err != cache.ErrCacheMiss {
		t.Errorf("expected cache key to be deleted after inventory update, got err: %v", err)
	}
}

// Mock cache that simulates Redis network failures
type failingMockCache struct {
	failGet bool
	failSet bool
	failDel bool
}

func (f *failingMockCache) Get(ctx context.Context, key string) (string, error) {
	if f.failGet {
		return "", errors.New("simulated redis connection timeout on GET")
	}
	return "", cache.ErrCacheMiss
}

func (f *failingMockCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if f.failSet {
		return errors.New("simulated redis write failure on SET")
	}
	return nil
}

func (f *failingMockCache) Del(ctx context.Context, keys ...string) error {
	if f.failDel {
		return errors.New("simulated redis connection error on DEL")
	}
	return nil
}

func (f *failingMockCache) Ping(ctx context.Context) error {
	return nil
}

func (f *failingMockCache) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Duration, error) {
	return true, limit, 0, nil
}

func (f *failingMockCache) Close() error {
	return nil
}

func TestMenu_RedisFailureFallback(t *testing.T) {
	_, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	// 1. Redis GET fails -> Request still succeeds by falling back to PostgreSQL
	failCache := &failingMockCache{failGet: true, failSet: true, failDel: true}
	rzpService := service.NewRazorpayService("rzp_test_campusbite_mock_key", "rzp_test_campusbite_mock_secret", "rzp_test_campusbite_mock_webhook_secret")
	router := routes.SetupRouter(db, tokenService, rzpService, failCache)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/menu", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK on Redis GET failure fallback to PostgreSQL, got %d", w.Code)
	}
	if w.Header().Get("X-Cache") != "MISS" {
		t.Errorf("expected X-Cache: MISS on failed cache, got '%s'", w.Header().Get("X-Cache"))
	}

	// 2. Redis DEL fails -> Admin business operation still succeeds
	_, adminToken := createTestTokens(t, tokenService)
	validPrice := 50.0
	createPayload := models.CreateMenuItemRequest{
		Name:  fmt.Sprintf("FallbackItem_%d", time.Now().UnixNano()),
		Price: &validPrice,
	}
	createJSON, _ := json.Marshal(createPayload)
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/v1/menu", bytes.NewBuffer(createJSON))
	reqCreate.Header.Set("Authorization", "Bearer "+adminToken)
	reqCreate.Header.Set("Content-Type", "application/json")
	wCreate := httptest.NewRecorder()
	router.ServeHTTP(wCreate, reqCreate)
	if wCreate.Code != http.StatusCreated {
		t.Errorf("expected 201 Created on item create even if Redis DEL fails, got %d: %s", wCreate.Code, wCreate.Body.String())
	}
}

func TestRateLimit_EndpointsAnd429(t *testing.T) {
	router, db, _, redisCache, s := setupTestAppWithRedis(t)
	defer s.Close()
	defer db.Close()
	defer redisCache.Close()

	// Login endpoint rate limit: 10 requests per minute
	loginPayload := models.LoginRequest{
		Email:    "rate_test@campusbite.internal",
		Password: "WrongPassword123!",
	}
	loginJSON, _ := json.Marshal(loginPayload)

	for i := 1; i <= 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBuffer(loginJSON))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.168.1.50:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was prematurely rate limited", i)
		}
	}

	// 11th request -> 429 Too Many Requests
	req11 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBuffer(loginJSON))
	req11.Header.Set("Content-Type", "application/json")
	req11.RemoteAddr = "192.168.1.50:12345"
	w11 := httptest.NewRecorder()
	router.ServeHTTP(w11, req11)

	if w11.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 Too Many Requests on 11th login attempt, got %d: %s", w11.Code, w11.Body.String())
	}

	retryAfter := w11.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Errorf("expected Retry-After header in 429 response")
	}

	// Fast forward miniredis past window (61 seconds)
	s.FastForward(61 * time.Second)

	// 12th request after reset -> Allowed through
	req12 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBuffer(loginJSON))
	req12.Header.Set("Content-Type", "application/json")
	req12.RemoteAddr = "192.168.1.50:12345"
	w12 := httptest.NewRecorder()
	router.ServeHTTP(w12, req12)

	if w12.Code == http.StatusTooManyRequests {
		t.Errorf("expected request to be allowed after rate limit window reset, got 429")
	}
}

func TestRateLimit_Checkout_PerUser(t *testing.T) {
	router, db, tokenService, redisCache, s := setupTestAppWithRedis(t)
	defer s.Close()
	defer db.Close()
	defer redisCache.Close()

	_, studentToken1 := createRealTestUser(t, db, tokenService, models.RoleStudent)
	_, studentToken2 := createRealTestUser(t, db, tokenService, models.RoleStudent)

	// Checkout limit is 15 requests per minute
	for i := 1; i <= 15; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
		req.Header.Set("Authorization", "Bearer "+studentToken1)
		req.Header.Set("Idempotency-Key", fmt.Sprintf("key-u1-%d", i))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("user 1 request %d was unexpectedly rate limited", i)
		}
	}

	// 16th checkout request for user 1 -> 429
	req16 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	req16.Header.Set("Authorization", "Bearer "+studentToken1)
	req16.Header.Set("Idempotency-Key", "key-u1-16")
	w16 := httptest.NewRecorder()
	router.ServeHTTP(w16, req16)

	if w16.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 for user 1 on 16th checkout, got %d", w16.Code)
	}

	// User 2 is independent and must NOT be blocked
	reqU2 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	reqU2.Header.Set("Authorization", "Bearer "+studentToken2)
	reqU2.Header.Set("Idempotency-Key", "key-u2-1")
	wU2 := httptest.NewRecorder()
	router.ServeHTTP(wU2, reqU2)

	if wU2.Code == http.StatusTooManyRequests {
		t.Errorf("user 2 should not be blocked by user 1's rate limit")
	}
}

func TestRateLimit_FailOpenOnRedisDown(t *testing.T) {
	_, db, tokenService := setupIntegrationApp(t)
	defer db.Close()

	// Router initialized with NoOpCache (simulating unavailable Redis)
	noOpCache := cache.NewNoOpCache()
	rzpService := service.NewRazorpayService("rzp_test_campusbite_mock_key", "rzp_test_campusbite_mock_secret", "rzp_test_campusbite_mock_webhook_secret")
	router := routes.SetupRouter(db, tokenService, rzpService, noOpCache)

	// Even with many requests, NoOpCache allows all requests through (fail-open)
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBuffer([]byte(`{"email":"failopen@test.com","password":"test"}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code == http.StatusTooManyRequests {
			t.Errorf("expected fail-open behavior when Redis is down, got 429 on request %d", i)
		}
	}
}

func TestRateLimit_ConcurrentRequestsNoBypass(t *testing.T) {
	router, db, tokenService, redisCache, s := setupTestAppWithRedis(t)
	defer s.Close()
	defer db.Close()
	defer redisCache.Close()

	_, studentToken := createRealTestUser(t, db, tokenService, models.RoleStudent)

	limit := 15
	totalRequests := 30
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowedCount := 0
	rateLimitedCount := 0

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
			req.Header.Set("Authorization", "Bearer "+studentToken)
			req.Header.Set("Idempotency-Key", fmt.Sprintf("race-key-%d", idx))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			mu.Lock()
			if w.Code == http.StatusTooManyRequests {
				rateLimitedCount++
			} else {
				allowedCount++
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	if allowedCount != limit {
		t.Errorf("expected exactly %d allowed requests under concurrent load, got %d", limit, allowedCount)
	}
	if rateLimitedCount != totalRequests-limit {
		t.Errorf("expected %d rate-limited requests (429), got %d", totalRequests-limit, rateLimitedCount)
	}
}
