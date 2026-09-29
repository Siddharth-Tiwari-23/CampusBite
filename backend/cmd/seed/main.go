package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"campusbite/internal/auth"
	"campusbite/internal/config"
	"campusbite/internal/database"
	"campusbite/internal/models"
	"campusbite/internal/repository"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Config load failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	defer db.Close()

	userRepo := repository.NewUserRepository(db)

	adminEmail := "admin@campusbite.internal"
	adminPassword := "AdminPass123!"

	existing, err := userRepo.GetByEmail(ctx, adminEmail)
	if err == nil && existing != nil {
		fmt.Printf("Admin user already exists with ID: %s\n", existing.ID)
		return
	}

	hash, err := auth.HashPassword(adminPassword)
	if err != nil {
		log.Fatalf("Failed to hash password: %v", err)
	}

	admin, err := userRepo.Create(ctx, "Admin User", adminEmail, hash, models.RoleAdmin)
	if err != nil {
		log.Fatalf("Failed to create admin user: %v", err)
	}

	fmt.Printf("Admin user created successfully:\n  ID:    %s\n  Email: %s\n  Role:  %s\n", admin.ID, admin.Email, admin.Role)
}
