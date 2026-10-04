package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"campusbite/internal/auth"
	"campusbite/internal/cache"
	"campusbite/internal/config"
	"campusbite/internal/database"
	"campusbite/internal/routes"
	"campusbite/internal/service"
	"campusbite/internal/worker"
	"campusbite/internal/ws"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	log.Printf("Successfully connected to PostgreSQL database")

	// Initialize Redis Cache and Rate Limiting client
	var cacheService cache.CacheService
	redisCache, err := cache.NewRedisCache(cfg.RedisURL)
	if err != nil {
		log.Printf("[Redis] Warning: Redis initialization failed: %v (falling back to in-memory no-op mode)", err)
		cacheService = cache.NewNoOpCache()
	} else {
		cacheService = redisCache
		defer redisCache.Close()
	}

	// Initialize JWT Token Service (24-hour expiration)
	tokenService, err := auth.NewTokenService(cfg.JWTSecret, 24*time.Hour)
	if err != nil {
		log.Fatalf("Failed to initialize token service: %v", err)
	}

	// Initialize WebSocket Hub
	wsHub := ws.NewHub()

	// Initialize Razorpay Service (enforcing production validation when APP_ENV=production)
	razorpayService := service.NewRazorpayService(cfg.RazorpayKeyID, cfg.RazorpayKeySecret, cfg.RazorpayWebhookSecret, cfg.IsProduction())

	router := routes.SetupRouter(db, tokenService, razorpayService, cacheService, wsHub)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	// Create application-level context for background worker lifecycle
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	// Initialize and start background maintenance workers
	workerManager := worker.NewManager(db, cfg)
	workerManager.Start(appCtx)

	go func() {
		log.Printf("Starting CampusBite server on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server listen failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server gracefully...")

	// 1. Cancel application context to signal workers
	appCancel()

	// 2. Stop background workers gracefully
	if err := workerManager.Stop(5 * time.Second); err != nil {
		log.Printf("Warning: Worker manager shutdown: %v", err)
	}

	// 3. Gracefully shutdown HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown forced: %v", err)
	}

	log.Println("Server exited cleanly")
}
