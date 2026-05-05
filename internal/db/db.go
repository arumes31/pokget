// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

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

	wd, _ := os.Getwd()
	slog.Info("Database initialization", "working_dir", wd)

	if host == "" || port == "" || user == "" || dbname == "" {
		slog.Warn("Missing required database environment variables, skipping DB initialization")
		return
	}

	if sslmode == "" {
		sslmode = "disable" // Default to disable for easy local/docker setup
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

	// Get absolute path for migrations
	absPath, err := filepath.Abs("migrations")
	if err != nil {
		return fmt.Errorf("could not get absolute path for migrations: %w", err)
	}

	// Verify migrations directory exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("migrations directory not found at: %s", absPath)
	}

	driver, err := postgres.WithInstance(DB, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("could not create migration driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://"+absPath,
		"postgres", driver)
	if err != nil {
		return fmt.Errorf("could not create migration instance: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("could not apply migrations: %w", err)
	}

	slog.Info("Database migrations applied successfully")
	return nil
}
