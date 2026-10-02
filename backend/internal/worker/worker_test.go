package worker_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"campusbite/internal/auth"
	"campusbite/internal/config"
	"campusbite/internal/database"
	"campusbite/internal/models"
	"campusbite/internal/repository"
	"campusbite/internal/worker"
)

func setupTestDB(t *testing.T) (*database.DB, *config.Config) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("skipping worker tests: failed to load config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Skipf("skipping worker tests: failed to connect to database: %v", err)
	}

	return db, cfg
}

func createTestUserAndOrder(t *testing.T, db *database.DB, initialStock, orderQty int) (*models.User, *models.MenuItem, *models.OrderResponse) {
	t.Helper()
	ctx := context.Background()

	userRepo := repository.NewUserRepository(db)
	menuRepo := repository.NewMenuRepository(db)
	cartRepo := repository.NewCartRepository(db)
	orderRepo := repository.NewOrderRepository(db)

	// Create unique user
	user, err := userRepo.Create(
		ctx,
		"Worker Test User",
		fmt.Sprintf("worker_user_%d@campusbite.internal", time.Now().UnixNano()),
		"hashed_password",
		models.RoleStudent,
	)
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}

	// Create menu item with initial inventory
	item, err := menuRepo.Create(
		ctx,
		fmt.Sprintf("Worker Burger %d", time.Now().UnixNano()),
		"Worker Burger Description",
		120.00,
		"",
		true,
		initialStock,
	)
	if err != nil {
		t.Fatalf("failed to create menu item: %v", err)
	}

	// Add to cart
	_, err = cartRepo.AddItem(ctx, user.ID, item.ID, orderQty)
	if err != nil {
		t.Fatalf("failed to add item to cart: %v", err)
	}

	// Checkout order
	idempKey := fmt.Sprintf("idemp_worker_test_%d", time.Now().UnixNano())
	order, err := orderRepo.CreateFromCart(ctx, user.ID, idempKey, "")
	if err != nil {
		t.Fatalf("failed to create order from cart: %v", err)
	}

	return user, item, order
}

func TestWorker_ReservationExpiry_RestoresInventoryAndExpires(t *testing.T) {
	db, cfg := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	initialStock := 10
	orderQty := 3

	_, item, order := createTestUserAndOrder(t, db, initialStock, orderQty)

	// Verify stock was reduced from 10 to 7 upon checkout
	var stockAfterCheckout int
	err := db.Pool.QueryRow(ctx, "SELECT quantity FROM inventory WHERE menu_item_id = $1", item.ID).Scan(&stockAfterCheckout)
	if err != nil {
		t.Fatalf("failed to query inventory: %v", err)
	}
	if stockAfterCheckout != initialStock-orderQty {
		t.Fatalf("expected stock %d, got %d", initialStock-orderQty, stockAfterCheckout)
	}

	// 1. Manually backdate the reservation expires_at to simulate expiration
	_, err = db.Pool.Exec(ctx, `
		UPDATE inventory_reservations
		SET expires_at = NOW() - INTERVAL '1 minute'
		WHERE order_id = $1
	`, order.ID)
	if err != nil {
		t.Fatalf("failed to backdate reservation: %v", err)
	}

	// 2. Run reservation worker processing
	resWorker := worker.NewReservationWorker(db, cfg.ReservationWorkerInterval)
	processed, err := resWorker.ProcessExpiredReservations(ctx)
	if err != nil {
		t.Fatalf("ProcessExpiredReservations failed: %v", err)
	}
	if processed < 1 {
		t.Fatalf("expected at least 1 reservation processed, got %d", processed)
	}

	// 3. Verify reservation status is now EXPIRED
	var resStatus string
	err = db.Pool.QueryRow(ctx, "SELECT status FROM inventory_reservations WHERE order_id = $1", order.ID).Scan(&resStatus)
	if err != nil {
		t.Fatalf("failed to query reservation status: %v", err)
	}
	if resStatus != models.ReservationStatusExpired {
		t.Errorf("expected reservation status %s, got %s", models.ReservationStatusExpired, resStatus)
	}

	// 4. Verify inventory quantity was restored back to 10
	var restoredStock int
	err = db.Pool.QueryRow(ctx, "SELECT quantity FROM inventory WHERE menu_item_id = $1", item.ID).Scan(&restoredStock)
	if err != nil {
		t.Fatalf("failed to query restored inventory: %v", err)
	}
	if restoredStock != initialStock {
		t.Errorf("expected inventory restored to %d, got %d", initialStock, restoredStock)
	}

	// 5. Verify order status transitioned to CANCELLED
	var orderStatus string
	err = db.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", order.ID).Scan(&orderStatus)
	if err != nil {
		t.Fatalf("failed to query order status: %v", err)
	}
	if orderStatus != models.OrderStatusCancelled {
		t.Errorf("expected order status %s, got %s", models.OrderStatusCancelled, orderStatus)
	}

	// 6. Running processing a second time should NOT restore inventory again (idempotent maintenance)
	processedAgain, err := resWorker.ProcessExpiredReservations(ctx)
	if err != nil {
		t.Fatalf("second ProcessExpiredReservations failed: %v", err)
	}

	var finalStock int
	err = db.Pool.QueryRow(ctx, "SELECT quantity FROM inventory WHERE menu_item_id = $1", item.ID).Scan(&finalStock)
	if err != nil {
		t.Fatalf("failed to query final inventory: %v", err)
	}
	if finalStock != initialStock {
		t.Errorf("expected inventory to remain %d, got %d (double restoration occurred!)", initialStock, finalStock)
	}
	_ = processedAgain
}

func TestWorker_ReservationNotExpired_RemainsActive(t *testing.T) {
	db, cfg := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	initialStock := 10
	orderQty := 2

	_, item, order := createTestUserAndOrder(t, db, initialStock, orderQty)

	// Reservation has expires_at 15 minutes in the future (default)
	resWorker := worker.NewReservationWorker(db, cfg.ReservationWorkerInterval)
	_, err := resWorker.ProcessExpiredReservations(ctx)
	if err != nil {
		t.Fatalf("ProcessExpiredReservations failed: %v", err)
	}

	// Verify reservation is still ACTIVE
	var resStatus string
	err = db.Pool.QueryRow(ctx, "SELECT status FROM inventory_reservations WHERE order_id = $1", order.ID).Scan(&resStatus)
	if err != nil {
		t.Fatalf("failed to query reservation status: %v", err)
	}
	if resStatus != models.ReservationStatusActive {
		t.Errorf("expected reservation status to remain ACTIVE, got %s", resStatus)
	}

	// Verify inventory is still 8
	var currentStock int
	err = db.Pool.QueryRow(ctx, "SELECT quantity FROM inventory WHERE menu_item_id = $1", item.ID).Scan(&currentStock)
	if err != nil {
		t.Fatalf("failed to query inventory: %v", err)
	}
	if currentStock != initialStock-orderQty {
		t.Errorf("expected inventory to remain %d, got %d", initialStock-orderQty, currentStock)
	}

	// Verify order status is still PENDING
	var orderStatus string
	err = db.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", order.ID).Scan(&orderStatus)
	if err != nil {
		t.Fatalf("failed to query order status: %v", err)
	}
	if orderStatus != models.OrderStatusPending {
		t.Errorf("expected order status to remain PENDING, got %s", orderStatus)
	}
}

func TestWorker_ConcurrentExpiry_NoDoubleRestoration(t *testing.T) {
	db, cfg := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	initialStock := 20
	orderQty := 5

	_, item, order := createTestUserAndOrder(t, db, initialStock, orderQty)

	// Fetch the reservation ID
	var resID string
	err := db.Pool.QueryRow(ctx, "SELECT id FROM inventory_reservations WHERE order_id = $1", order.ID).Scan(&resID)
	if err != nil {
		t.Fatalf("failed to query reservation id: %v", err)
	}

	// Backdate the reservation to past
	_, err = db.Pool.Exec(ctx, `
		UPDATE inventory_reservations
		SET expires_at = NOW() - INTERVAL '5 minutes'
		WHERE id = $1
	`, resID)
	if err != nil {
		t.Fatalf("failed to backdate reservation: %v", err)
	}

	// Concurrently attempt to expire the exact same reservation across 10 goroutines
	concurrency := 10
	var wg sync.WaitGroup
	wg.Add(concurrency)

	var handledCount int64
	var handledMutex sync.Mutex

	resWorker := worker.NewReservationWorker(db, cfg.ReservationWorkerInterval)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			handled, expErr := resWorker.ExpireSingleReservation(ctx, resID)
			if expErr == nil && handled {
				handledMutex.Lock()
				handledCount++
				handledMutex.Unlock()
			}
		}()
	}

	wg.Wait()

	// Exactly 1 goroutine should succeed in claiming and expiring the reservation
	if handledCount != 1 {
		t.Errorf("expected exactly 1 goroutine to handle reservation expiration, got %d", handledCount)
	}

	// Inventory must be exactly 20, never double-restored
	var finalStock int
	err = db.Pool.QueryRow(ctx, "SELECT quantity FROM inventory WHERE menu_item_id = $1", item.ID).Scan(&finalStock)
	if err != nil {
		t.Fatalf("failed to query final inventory: %v", err)
	}
	if finalStock != initialStock {
		t.Errorf("expected inventory restored to %d, got %d", initialStock, finalStock)
	}
}

func TestWorker_IdempotencyCleanup(t *testing.T) {
	db, cfg := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	userRepo := repository.NewUserRepository(db)
	user, err := userRepo.Create(
		ctx,
		"Idemp Worker User",
		fmt.Sprintf("idemp_user_%d@campusbite.internal", time.Now().UnixNano()),
		"pass",
		models.RoleStudent,
	)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// 1. Insert 2 expired idempotency keys (expires_at in past)
	expiredKey1 := fmt.Sprintf("exp_key_1_%d", time.Now().UnixNano())
	expiredKey2 := fmt.Sprintf("exp_key_2_%d", time.Now().UnixNano())
	insertExpiredQuery := `
		INSERT INTO idempotency_keys (user_id, key, request_hash, status, expires_at, created_at)
		VALUES 
			($1, $2, 'hash1', 'COMPLETED', NOW() - INTERVAL '2 hours', NOW() - INTERVAL '26 hours'),
			($1, $3, 'hash2', 'FAILED', NOW() - INTERVAL '1 hour', NOW() - INTERVAL '25 hours')
	`
	_, err = db.Pool.Exec(ctx, insertExpiredQuery, user.ID, expiredKey1, expiredKey2)
	if err != nil {
		t.Fatalf("failed to insert expired idempotency keys: %v", err)
	}

	// 2. Insert 1 active idempotency key (expires_at in future)
	activeKey := fmt.Sprintf("active_key_%d", time.Now().UnixNano())
	insertActiveQuery := `
		INSERT INTO idempotency_keys (user_id, key, request_hash, status, expires_at, created_at)
		VALUES ($1, $2, 'hash3', 'COMPLETED', NOW() + INTERVAL '24 hours', NOW())
	`
	_, err = db.Pool.Exec(ctx, insertActiveQuery, user.ID, activeKey)
	if err != nil {
		t.Fatalf("failed to insert active idempotency key: %v", err)
	}

	// 3. Run idempotency cleanup worker
	idempWorker := worker.NewIdempotencyWorker(db, cfg.IdempotencyCleanupInterval)
	deletedCount, err := idempWorker.CleanupExpiredKeys(ctx)
	if err != nil {
		t.Fatalf("CleanupExpiredKeys failed: %v", err)
	}
	if deletedCount < 2 {
		t.Errorf("expected at least 2 expired keys deleted, got %d", deletedCount)
	}

	// 4. Verify expired keys are gone
	var countExpired int
	err = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM idempotency_keys WHERE key IN ($1, $2)", expiredKey1, expiredKey2).Scan(&countExpired)
	if err != nil {
		t.Fatalf("failed to count expired keys: %v", err)
	}
	if countExpired != 0 {
		t.Errorf("expected 0 expired keys remaining, got %d", countExpired)
	}

	// 5. Verify active key remains intact
	var countActive int
	err = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM idempotency_keys WHERE key = $1", activeKey).Scan(&countActive)
	if err != nil {
		t.Fatalf("failed to count active key: %v", err)
	}
	if countActive != 1 {
		t.Errorf("expected active key to remain, got count %d", countActive)
	}
}

func TestWorker_Manager_StartAndGracefulStop(t *testing.T) {
	db, cfg := setupTestDB(t)
	defer db.Close()

	// Create manager with small intervals
	testCfg := *cfg
	testCfg.ReservationWorkerInterval = 100 * time.Millisecond
	testCfg.IdempotencyCleanupInterval = 100 * time.Millisecond

	manager := worker.NewManager(db, &testCfg)

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	// Start workers
	manager.Start(appCtx)

	// Allow workers to run briefly
	time.Sleep(250 * time.Millisecond)

	// Trigger graceful stop
	appCancel()
	err := manager.Stop(2 * time.Second)
	if err != nil {
		t.Fatalf("expected clean manager stop, got error: %v", err)
	}
}

// Suppress unused auth package import warning if any
var _ = auth.NewTokenService
