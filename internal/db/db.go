package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file" // Register file source for migrations
	_ "github.com/lib/pq"                                // Register PostgreSQL driver
)

var DB *sql.DB

func InitDB() {
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")
	sslmode := os.Getenv("DB_SSLMODE")

	if host == "" || port == "" || user == "" || dbname == "" {
		slog.Warn("Missing required database environment variables, skipping DB initialization")
		return
	}

	if sslmode == "" {
		sslmode = "require" // Secure default
	}

	psqlInfo := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode)

	var err error
	DB, err = sql.Open("postgres", psqlInfo)
	if err != nil {
		slog.Error("Error opening database", "error", err)
		return
	}

	err = DB.Ping()
	if err != nil {
		slog.Error("Error connecting to database", "error", err)
		if err := DB.Close(); err != nil {
			slog.Error("Error closing database", "error", err)
		}
		DB = nil
		return
	}

	slog.Info("Successfully connected to PostgreSQL")

	if err := RunMigrations(); err != nil {
		slog.Error("Migration error", "error", err)
	}
}

func RunMigrations() error {
	if DB == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	driver, err := postgres.WithInstance(DB, &postgres.Config{})
	if err != nil {
		return err
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://migrations",
		"postgres", driver)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}

	slog.Info("Database migrations applied successfully")
	return nil
}
