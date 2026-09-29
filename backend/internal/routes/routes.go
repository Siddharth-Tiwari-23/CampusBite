package routes

import (
	"campusbite/internal/auth"
	"campusbite/internal/database"
	"campusbite/internal/handlers"
	"campusbite/internal/middleware"
	"campusbite/internal/models"
	"campusbite/internal/repository"

	"github.com/gin-gonic/gin"
)

// SetupRouter initializes the Gin engine with registered routes and middleware.
func SetupRouter(db *database.DB, tokenService *auth.TokenService) *gin.Engine {
	router := gin.Default()

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

		authHandler := handlers.NewAuthHandler(userRepo, tokenService)
		menuHandler := handlers.NewMenuHandler(menuRepo)
		inventoryHandler := handlers.NewInventoryHandler(inventoryRepo)
		cartHandler := handlers.NewCartHandler(cartRepo)
		orderHandler := handlers.NewOrderHandler(orderRepo)

		authMiddleware := middleware.AuthMiddleware(tokenService)
		adminRequired := middleware.RequireRole(models.RoleAdmin)

		// Authentication routes
		authRoutes := apiV1.Group("/auth")
		{
			authRoutes.POST("/register", authHandler.Register)
			authRoutes.POST("/login", authHandler.Login)

			// Protected verification endpoints
			authRoutes.GET("/me", authMiddleware, authHandler.Me)
			authRoutes.GET("/admin-test", authMiddleware, adminRequired, authHandler.AdminTest)
		}

		// Menu routes
		menuRoutes := apiV1.Group("/menu")
		{
			// Public catalog access
			menuRoutes.GET("", menuHandler.GetMenu)
			menuRoutes.GET("/:id", menuHandler.GetMenuItem)

			// Admin-only management
			menuRoutes.POST("", authMiddleware, adminRequired, menuHandler.CreateMenuItem)
			menuRoutes.PATCH("/:id", authMiddleware, adminRequired, menuHandler.UpdateMenuItem)
			menuRoutes.DELETE("/:id", authMiddleware, adminRequired, menuHandler.DeleteMenuItem)
		}

		// Inventory routes
		inventoryRoutes := apiV1.Group("/inventory")
		{
			// Public/read access
			inventoryRoutes.GET("", inventoryHandler.GetInventory)
			inventoryRoutes.GET("/:menuItemId", inventoryHandler.GetInventoryItem)

			// Admin-only update
			inventoryRoutes.PATCH("/:menuItemId", authMiddleware, adminRequired, inventoryHandler.UpdateInventory)
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

		// Order routes
		orderRoutes := apiV1.Group("/orders")
		orderRoutes.Use(authMiddleware)
		{
			orderRoutes.GET("", orderHandler.ListOrders)
			orderRoutes.GET("/:id", orderHandler.GetOrder)
			orderRoutes.POST("", orderHandler.CreateOrder)
		}
	}

	return router
}
