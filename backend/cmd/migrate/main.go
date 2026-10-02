package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"campusbite/internal/config"
	"campusbite/internal/database"

	"github.com/jackc/pgx/v5"
)

func findMigrationsDir() (string, error) {
	candidates := []string{
		"migrations",
		"backend/migrations",
		"../migrations",
	}

	for _, dir := range candidates {
		info, err := os.Stat(dir)
		if err == nil && info.IsDir() {
			return filepath.Abs(dir)
		}
	}

	return "", fmt.Errorf("could not locate migrations directory (checked: %v)", candidates)
}

func ensureMigrationTable(ctx context.Context, db *database.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(255) PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	`
	_, err := db.Pool.Exec(ctx, query)
	return err
}

func getAppliedMigrations(ctx context.Context, db *database.DB) (map[string]time.Time, error) {
	rows, err := db.Pool.Query(ctx, "SELECT version, applied_at FROM schema_migrations ORDER BY version ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]time.Time)
	for rows.Next() {
		var version string
		var appliedAt time.Time
		if err := rows.Scan(&version, &appliedAt); err != nil {
			return nil, err
		}
		applied[version] = appliedAt
	}

	return applied, rows.Err()
}

func runUp(ctx context.Context, db *database.DB, migrationsDir string) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	applied, err := getAppliedMigrations(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	var upFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			upFiles = append(upFiles, entry.Name())
		}
	}
	sort.Strings(upFiles)

	if len(upFiles) == 0 {
		fmt.Println("No .up.sql migration files found.")
		return nil
	}

	appliedCount := 0
	for _, file := range upFiles {
		version := strings.TrimSuffix(file, ".up.sql")
		if _, exists := applied[version]; exists {
			fmt.Printf("Migration '%s' is already applied. Skipping.\n", version)
			continue
		}

		filePath := filepath.Join(migrationsDir, file)
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", file, err)
		}

		fmt.Printf("Applying migration '%s'...\n", version)

		tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to execute migration %s: %w", file, err)
		}

		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to record migration %s in schema_migrations: %w", file, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit migration transaction %s: %w", file, err)
		}

		fmt.Printf("Successfully applied migration '%s'.\n", version)
		appliedCount++
	}

	if appliedCount == 0 {
		fmt.Println("Database is already up to date.")
	} else {
		fmt.Printf("Successfully applied %d migration(s).\n", appliedCount)
	}

	return nil
}

func runDown(ctx context.Context, db *database.DB, migrationsDir string) error {
	applied, err := getAppliedMigrations(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	if len(applied) == 0 {
		fmt.Println("No migrations to rollback.")
		return nil
	}

	var appliedVersions []string
	for v := range applied {
		appliedVersions = append(appliedVersions, v)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(appliedVersions)))

	// Rollback the latest applied migration
	latestVersion := appliedVersions[0]
	downFile := latestVersion + ".down.sql"
	filePath := filepath.Join(migrationsDir, downFile)

	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read rollback file %s: %w", downFile, err)
	}

	fmt.Printf("Reverting migration '%s'...\n", latestVersion)

	tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	if _, err := tx.Exec(ctx, string(content)); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("failed to execute rollback %s: %w", downFile, err)
	}

	if _, err := tx.Exec(ctx, "DELETE FROM schema_migrations WHERE version = $1", latestVersion); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("failed to remove migration record %s: %w", latestVersion, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit rollback transaction %s: %w", downFile, err)
	}

	fmt.Printf("Successfully reverted migration '%s'.\n", latestVersion)
	return nil
}

func runStatus(ctx context.Context, db *database.DB, migrationsDir string) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	applied, err := getAppliedMigrations(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	var versions []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			versions = append(versions, strings.TrimSuffix(entry.Name(), ".up.sql"))
		}
	}
	sort.Strings(versions)

	fmt.Println("Migration Status:")
	fmt.Println("------------------------------------------------------------")
	for _, v := range versions {
		if appliedAt, ok := applied[v]; ok {
			fmt.Printf("[APPLIED]  %s (at %s)\n", v, appliedAt.Format(time.RFC3339))
		} else {
			fmt.Printf("[PENDING]  %s\n", v)
		}
	}
	fmt.Println("------------------------------------------------------------")

	return nil
}

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

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: go run ./cmd/migrate [-database-url=URL] [up|down|status]")
		os.Exit(1)
	}

	command := args[0]

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	targetURL := cfg.DatabaseURL
	if dbURLFlag != "" {
		targetURL = dbURLFlag
	}

	printSanitizedTarget(targetURL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := database.NewPool(ctx, targetURL)
	if err != nil {
		log.Fatalf("Database connection error: %v", err)
	}
	defer db.Close()

	if err := ensureMigrationTable(ctx, db); err != nil {
		log.Fatalf("Failed to initialize schema_migrations table: %v", err)
	}

	migrationsDir, err := findMigrationsDir()
	if err != nil {
		log.Fatalf("Migrations directory error: %v", err)
	}

	switch command {
	case "up":
		if err := runUp(ctx, db, migrationsDir); err != nil {
			log.Fatalf("Migration up failed: %v", err)
		}
	case "down":
		if err := runDown(ctx, db, migrationsDir); err != nil {
			log.Fatalf("Migration down failed: %v", err)
		}
	case "status":
		if err := runStatus(ctx, db, migrationsDir); err != nil {
			log.Fatalf("Migration status failed: %v", err)
		}
	default:
		fmt.Printf("Unknown command '%s'. Usage: go run ./cmd/migrate [-database-url=URL] [up|down|status]\n", command)
		os.Exit(1)
	}
}
