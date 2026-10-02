package routes

import (
	"time"

	"campusbite/internal/auth"
	"campusbite/internal/cache"
	"campusbite/internal/config"
	"campusbite/internal/database"
	"campusbite/internal/handlers"
	"campusbite/internal/middleware"
	"campusbite/internal/models"
	"campusbite/internal/repository"
	"campusbite/internal/service"
	"campusbite/internal/ws"

	"github.com/gin-gonic/gin"
)

// SetupRouter initializes the Gin engine with registered routes, cache, and middleware.
// Optional services (RazorpayService, CacheService, *ws.Hub) can be passed variadically.
func SetupRouter(db *database.DB, tokenService *auth.TokenService, optionalServices ...interface{}) *gin.Engine {
	router := gin.Default()
	router.Use(middleware.CORSMiddleware())

	var razorpayService service.RazorpayService
	var cacheService cache.CacheService
	var wsHub *ws.Hub
	var geminiService service.GeminiService

	for _, s := range optionalServices {
		switch v := s.(type) {
		case service.RazorpayService:
			razorpayService = v
		case cache.CacheService:
			cacheService = v
		case *ws.Hub:
			wsHub = v
		case service.GeminiService:
			geminiService = v
		}
	}

	if wsHub == nil {
		wsHub = ws.NewHub()
	}

	cfg, err := config.Load()
	menuCacheTTL := 60 * time.Second
	if err == nil && cfg != nil && cfg.MenuCacheTTL > 0 {
		menuCacheTTL = cfg.MenuCacheTTL
	}

	if razorpayService == nil {
		if err != nil || cfg == nil {
			razorpayService = service.NewRazorpayService("rzp_test_campusbite_mock_key", "rzp_test_campusbite_mock_secret", "rzp_test_campusbite_mock_webhook_secret")
		} else {
			razorpayService = service.NewRazorpayService(cfg.RazorpayKeyID, cfg.RazorpayKeySecret, cfg.RazorpayWebhookSecret)
		}
	}

	if cacheService == nil {
		if cfg != nil && cfg.RedisURL != "" {
			c, err := cache.NewRedisCache(cfg.RedisURL)
			if err == nil {
				cacheService = c
			} else {
				cacheService = cache.NewNoOpCache()
			}
		} else {
			cacheService = cache.NewNoOpCache()
		}
	}

	if geminiService == nil {
		if cfg != nil {
			geminiService = service.NewHTTPGeminiService(cfg.GeminiAPIKey, cfg.GeminiModel)
		} else {
			geminiService = service.NewHTTPGeminiService("", "gemini-2.5-flash")
		}
	}

	// Public Health check endpoint (preserved)
	healthHandler := handlers.NewHealthHandler(db)
	router.GET("/health", healthHandler.Health)

	// API v1 routes
	apiV1 := router.Group("/api/v1")
	{
		// Dependencies
		userRepo := repository.NewUserRepository(db)
		menuRepo := repository.NewMenuRepository(db)
		inventoryRepo := repository.NewInventoryRepository(db)
		cartRepo := repository.NewCartRepository(db)
		orderRepo := repository.NewOrderRepository(db)
		paymentRepo := repository.NewPaymentRepository(db, orderRepo)
		webhookRepo := repository.NewWebhookRepository(db)
		notifRepo := repository.NewNotificationRepository(db)
		analyticsRepo := repository.NewAnalyticsRepository(db)

		authHandler := handlers.NewAuthHandler(userRepo, tokenService)
		menuHandler := handlers.NewMenuHandler(menuRepo, cacheService, menuCacheTTL)
		inventoryHandler := handlers.NewInventoryHandler(inventoryRepo, cacheService)
		cartHandler := handlers.NewCartHandler(cartRepo)
		orderHandler := handlers.NewOrderHandler(orderRepo, wsHub)
		paymentHandler := handlers.NewPaymentHandler(paymentRepo, orderRepo, webhookRepo, razorpayService, wsHub)
		notifHandler := handlers.NewNotificationHandler(notifRepo, wsHub)
		analyticsRouter := service.NewAnalyticsRouter(geminiService, analyticsRepo)
		analyticsHandler := handlers.NewAnalyticsHandler(analyticsRouter)
		wsHandler := ws.NewWSHandler(wsHub, tokenService)

		authMiddleware := middleware.AuthMiddleware(tokenService)
		adminRequired := middleware.RequireRole(models.RoleAdmin)

		// WebSocket endpoint (authenticated via JWT query param or Authorization header)
		apiV1.GET("/ws", wsHandler.HandleWS)

		// Authentication routes (Rate limited by IP: 10 requests / min)
		authRoutes := apiV1.Group("/auth")
		{
			authRoutes.POST("/register", middleware.RateLimit(cacheService, "auth_register", 10, time.Minute), authHandler.Register)
			authRoutes.POST("/login", middleware.RateLimit(cacheService, "auth_login", 10, time.Minute), authHandler.Login)

			// Protected verification endpoints
			authRoutes.GET("/me", authMiddleware, authHandler.Me)
			authRoutes.GET("/admin-test", authMiddleware, adminRequired, authHandler.AdminTest)
		}

		// Menu routes (Read cached with Redis cache-aside)
		menuRoutes := apiV1.Group("/menu")
		{
			// Public catalog access
			menuRoutes.GET("", menuHandler.GetMenu)
			menuRoutes.GET("/:id", menuHandler.GetMenuItem)

			// Admin-only management (Invalidates menu cache)
			menuRoutes.POST("", authMiddleware, adminRequired, menuHandler.CreateMenuItem)
			menuRoutes.PATCH("/:id", authMiddleware, adminRequired, menuHandler.UpdateMenuItem)
			menuRoutes.PUT("/:id", authMiddleware, adminRequired, menuHandler.UpdateMenuItem)
			menuRoutes.DELETE("/:id", authMiddleware, adminRequired, menuHandler.DeleteMenuItem)
		}

		// Inventory routes
		inventoryRoutes := apiV1.Group("/inventory")
		{
			// Public/read access
			inventoryRoutes.GET("", inventoryHandler.GetInventory)
			inventoryRoutes.GET("/:menuItemId", inventoryHandler.GetInventoryItem)

			// Admin-only update (Invalidates menu cache)
			inventoryRoutes.PATCH("/:menuItemId", authMiddleware, adminRequired, inventoryHandler.UpdateInventory)
			inventoryRoutes.PUT("/:menuItemId", authMiddleware, adminRequired, inventoryHandler.UpdateInventory)
		}

		// Cart routes
		cartRoutes := apiV1.Group("/cart")
		cartRoutes.Use(authMiddleware)
		{
			cartRoutes.GET("", cartHandler.GetCart)
			cartRoutes.DELETE("", cartHandler.ClearCart)
			cartRoutes.POST("/items", cartHandler.AddToCart)
			cartRoutes.PATCH("/items/:menuItemId", cartHandler.UpdateCartItem)
			cartRoutes.DELETE("/items/:menuItemId", cartHandler.DeleteCartItem)
		}

		// Order routes (Checkout rate limited by User: 15 requests / min; Payment: 20 requests / min)
		orderRoutes := apiV1.Group("/orders")
		orderRoutes.Use(authMiddleware)
		{
			orderRoutes.GET("", orderHandler.ListOrders)
			orderRoutes.GET("/:id", orderHandler.GetOrder)
			orderRoutes.POST("", middleware.RateLimit(cacheService, "checkout", 15, time.Minute), orderHandler.CreateOrder)
			orderRoutes.POST("/:id/payment", middleware.RateLimit(cacheService, "payment", 20, time.Minute), paymentHandler.CreatePaymentOrder)
			orderRoutes.PATCH("/:id/status", adminRequired, orderHandler.UpdateOrderStatus)
		}

		// Payment routes
		paymentRoutes := apiV1.Group("/payments")
		{
			// Client verification (auth required)
			paymentRoutes.POST("/verify", authMiddleware, paymentHandler.VerifyPayment)

			// Webhook notifications (public, validated via X-Razorpay-Signature)
			paymentRoutes.POST("/webhook", paymentHandler.HandleWebhook)
		}

		// Notification routes
		notifRoutes := apiV1.Group("/notifications")
		notifRoutes.Use(authMiddleware)
		{
			notifRoutes.GET("", notifHandler.ListNotifications)
			notifRoutes.PATCH("/:id/read", notifHandler.MarkRead)
			notifRoutes.POST("/read-all", notifHandler.MarkAllRead)
		}

		// Analytics routes (ADMIN only)
		analyticsRoutes := apiV1.Group("/analytics")
		analyticsRoutes.Use(authMiddleware, adminRequired)
		{
			analyticsRoutes.POST("/query", analyticsHandler.Query)
			analyticsRoutes.POST("/trends", analyticsHandler.Trends)
			analyticsRoutes.GET("/trends", analyticsHandler.Trends)
		}
	}

	return router
}
