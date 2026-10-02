package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"campusbite/internal/auth"
	"campusbite/internal/cache"
	"campusbite/internal/config"
	"campusbite/internal/database"
	"campusbite/internal/models"
	"campusbite/internal/repository"
)

func printSanitizedTarget(rawURL string) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		fmt.Printf("Target Database: %s\n", rawURL)
		return
	}
	user := parsed.User.Username()
	host := parsed.Host
	dbPath := strings.TrimPrefix(parsed.Path, "/")
	fmt.Printf("Target Database: host=%s, db=%s, user=%s\n", host, dbPath, user)
}

func main() {
	var dbURLFlag string
	flag.StringVar(&dbURLFlag, "database-url", "", "Override database URL connection string")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Config load failed: %v", err)
	}

	targetURL := cfg.DatabaseURL
	if dbURLFlag != "" {
		targetURL = dbURLFlag
	}

	printSanitizedTarget(targetURL)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := database.NewPool(ctx, targetURL)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	defer db.Close()

	userRepo := repository.NewUserRepository(db)
	menuRepo := repository.NewMenuRepository(db)

	// 1. Seed Production Roles & Default Accounts (Idempotent)
	defaultPassword := "password123"
	hashedPassword, err := auth.HashPassword(defaultPassword)
	if err != nil {
		log.Fatalf("Failed to hash password: %v", err)
	}

	users := []struct {
		Name  string
		Email string
		Role  string
	}{
		{Name: "Student User", Email: "student@campusbite.com", Role: models.RoleStudent},
		{Name: "Cafeteria Manager", Email: "admin@campusbite.com", Role: models.RoleAdmin},
		{Name: "Campus Admin", Email: "admin@campusbite.internal", Role: models.RoleAdmin},
	}

	fmt.Println("--- Provisioning Users ---")
	for _, u := range users {
		existing, err := userRepo.GetByEmail(ctx, u.Email)
		if err == nil && existing != nil {
			fmt.Printf("✓ User '%s' (%s) [Role: %s] exists.\n", u.Name, u.Email, existing.Role)
		} else {
			created, err := userRepo.Create(ctx, u.Name, u.Email, hashedPassword, u.Role)
			if err != nil {
				log.Printf("Warning: Failed to provision user %s: %v", u.Email, err)
			} else {
				fmt.Printf("✓ Created user '%s' (%s) [Role: %s]\n", created.Name, created.Email, created.Role)
			}
		}
	}

	// 2. Production Cafeteria Catalog Provisioning
	// Validated cafeteria items with confirmed prices from the campus reference list.
	// NOTE: Chowmein, Maggi, and Sandwich are omitted until actual prices are supplied.
	productionCatalog := []struct {
		Name        string
		Description string
		Price       float64
		ImageURL    string
		Stock       int
	}{
		{
			Name:        "Veg Momos (10 pcs)",
			Description: "Fresh steamed vegetable dumplings stuffed with seasoned cabbage and carrots, served with spicy red chili sauce.",
			Price:       100.00,
			ImageURL:    "/images/menu/veg_momos.jpg",
			Stock:       50,
		},
		{
			Name:        "Paneer Momos (10 pcs)",
			Description: "Steamed dumplings filled with savory grated paneer and herbs, served with garlic chili dip.",
			Price:       140.00,
			ImageURL:    "/images/menu/paneer_momos.jpg",
			Stock:       40,
		},
		{
			Name:        "Hot Coffee (Coffee Special)",
			Description: "Freshly brewed steaming hot milk coffee made in classic cafeteria special style.",
			Price:       30.00,
			ImageURL:    "/images/menu/hot_coffee.jpg",
			Stock:       100,
		},
		{
			Name:        "Cold Coffee",
			Description: "Chilled whipped milk coffee blended with rich chocolate syrup and cocoa dusting.",
			Price:       80.00,
			ImageURL:    "/images/menu/cold_coffee.jpg",
			Stock:       80,
		},
		{
			Name:        "Chai Special",
			Description: "Freshly brewed traditional Indian milk tea infused with aromatic cardamom and crushed ginger.",
			Price:       15.00,
			ImageURL:    "/images/menu/chai_special.jpg",
			Stock:       150,
		},
		{
			Name:        "Paneer Paratha (2 pcs)",
			Description: "Whole wheat griddled flatbreads stuffed with spiced cottage cheese, served with butter and pickle.",
			Price:       100.00,
			ImageURL:    "/images/menu/paneer_paratha.jpg",
			Stock:       40,
		},
		{
			Name:        "Aloo Paratha (2 pcs)",
			Description: "Whole wheat griddled flatbreads stuffed with seasoned potato mash, served with butter and pickle.",
			Price:       80.00,
			ImageURL:    "/images/menu/aloo_paratha.jpg",
			Stock:       50,
		},
		{
			Name:        "Veg Samosa",
			Description: "Crisp golden fried triangular pastry stuffed with spicy potato and peas filling, served with chutney.",
			Price:       15.00,
			ImageURL:    "/images/menu/veg_samosa.jpg",
			Stock:       100,
		},
		{
			Name:        "French Fries Plate",
			Description: "Deep-fried golden salted potato fries served with ketchup and dip.",
			Price:       80.00,
			ImageURL:    "/images/menu/french_fries.jpg",
			Stock:       60,
		},
		{
			Name:        "Pasta Sada (1 plate)",
			Description: "Classic seasoned penne pasta tossed in herb tomato sauce with vegetables.",
			Price:       90.00,
			ImageURL:    "/images/menu/pasta_sada.jpg",
			Stock:       40,
		},
		{
			Name:        "Veg Pizza (1 pc)",
			Description: "Single-serving pizza baked with melted mozzarella cheese, onions, capsicum, and sliced tomatoes.",
			Price:       140.00,
			ImageURL:    "/images/menu/veg_pizza.jpg",
			Stock:       35,
		},
		{
			Name:        "Spring Roll (2 pcs)",
			Description: "Crispy fried rolls packed with julienned vegetables and spices, served with sweet chili dip.",
			Price:       60.00,
			ImageURL:    "/images/menu/spring_roll.jpg",
			Stock:       50,
		},
	}

	fmt.Println("\n--- Provisioning Production Catalog & Archiving Test Data ---")

	var canonicalActiveIDs []string

	for _, item := range productionCatalog {
		var existingID string
		var existingPrice float64
		queryExisting := `
			SELECT id, price FROM menu_items WHERE name = $1 ORDER BY created_at ASC LIMIT 1
		`
		err := db.Pool.QueryRow(ctx, queryExisting, item.Name).Scan(&existingID, &existingPrice)
		if err == nil {
			// Item exists: update image_url, description, price, and mark available
			updateQuery := `
				UPDATE menu_items
				SET description = $1, price = $2, image_url = $3, is_available = true, updated_at = NOW()
				WHERE id = $4
			`
			if _, uErr := db.Pool.Exec(ctx, updateQuery, item.Description, item.Price, item.ImageURL, existingID); uErr != nil {
				log.Printf("Warning: failed to update item %s: %v", item.Name, uErr)
			}

			// Ensure inventory row exists and has at least target stock
			invQuery := `
				INSERT INTO inventory (menu_item_id, quantity)
				VALUES ($1, $2)
				ON CONFLICT (menu_item_id) DO UPDATE SET quantity = GREATEST(inventory.quantity, EXCLUDED.quantity)
			`
			if _, invErr := db.Pool.Exec(ctx, invQuery, existingID, item.Stock); invErr != nil {
				log.Printf("Warning: failed to verify inventory for %s: %v", item.Name, invErr)
			}
			canonicalActiveIDs = append(canonicalActiveIDs, existingID)
			fmt.Printf("✓ Verified production item: %-28s (₹%.2f, Image: %s, ID: %s)\n", item.Name, item.Price, item.ImageURL, existingID)
		} else {
			// Insert new item and its initial inventory record
			created, err := menuRepo.Create(ctx, item.Name, item.Description, item.Price, item.ImageURL, true, item.Stock)
			if err != nil {
				log.Printf("Warning: Failed to create menu item %s: %v", item.Name, err)
			} else {
				canonicalActiveIDs = append(canonicalActiveIDs, created.ID)
				fmt.Printf("✓ Provisioned item:        %-28s (₹%.2f, Stock: %d, Image: %s, ID: %s)\n", created.Name, created.Price, item.Stock, created.ImageURL, created.ID)
			}
		}
	}

	// Archive any non-canonical / test catalog records by ID
	archiveQuery := `
		UPDATE menu_items
		SET is_available = false, updated_at = NOW()
		WHERE id != ALL($1) AND is_available = true
	`
	tag, err := db.Pool.Exec(ctx, archiveQuery, canonicalActiveIDs)
	if err != nil {
		log.Printf("Warning: failed to archive non-production items: %v", err)
	} else if tag.RowsAffected() > 0 {
		fmt.Printf("✓ Archived %d non-production / test catalog records (set is_available = false).\n", tag.RowsAffected())
	}

	// Invalidate Redis menu cache so changes are immediately active
	if redisCache, err := cache.NewRedisCache(cfg.RedisURL); err == nil {
		_ = redisCache.Del(ctx, cache.KeyMenuAvailable)
		fmt.Printf("✓ Invalidated Redis menu cache key '%s'.\n", cache.KeyMenuAvailable)
	}

	var activeCount, totalCount int
	_ = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM menu_items WHERE is_available = true").Scan(&activeCount)
	_ = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM menu_items").Scan(&totalCount)
	fmt.Printf("\n✓ Production catalog converged: %d active items (Total in DB: %d)\n", activeCount, totalCount)
	fmt.Println("Production catalog provisioning completed successfully!")
}
