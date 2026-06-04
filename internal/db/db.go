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
	db, err := Connect()
	if err != nil {
		slog.Error("Database connection failed", "error", err)
		return
	}
	DB = db

	if err := RunMigrations(); err != nil {
		slog.Error("Migration error", "error", err)
	}
}

var sqlOpen = sql.Open

func Connect() (*sql.DB, error) {
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")
	sslmode := os.Getenv("DB_SSLMODE")

	wd, _ := os.Getwd()
	slog.Info("Database initialization", "working_dir", wd)

	if host == "" || port == "" || user == "" || dbname == "" {
		return nil, fmt.Errorf("missing required database environment variables")
	}

	if sslmode == "" {
		sslmode = "disable"
	}

	psqlInfo := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode)

	db, err := sqlOpen("postgres", psqlInfo)
	if err != nil {
		return nil, fmt.Errorf("error opening database: %w", err)
	}

	if err = db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("error connecting to database: %w", err)
	}

	slog.Info("Successfully connected to PostgreSQL")
	return db, nil
}

func RunMigrations() error {
	if DB == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	absPath, err := filepath.Abs("migrations")
	if err != nil {
		return err
	}
	return ApplyMigrations(DB, absPath)
}

var NewMigrator = func(db *sql.DB, absPath string) (interface {
	Up() error
	Force(int) error
	Version() (uint, bool, error)
}, error) {
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return nil, err
	}
	return migrate.NewWithDatabaseInstance("file://"+absPath, "postgres", driver)
}

func ApplyMigrations(db *sql.DB, absPath string) error {
	// Verify migrations directory exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("migrations directory not found at: %s", absPath)
	}

	m, err := NewMigrator(db, absPath)
	if err != nil {
		return fmt.Errorf("could not create migration instance: %w", err)
	}

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		version, dirty, vErr := m.Version()
		if vErr != nil {
			return fmt.Errorf("could not apply migrations (and failed to get version): %w (version error: %v)", err, vErr)
		}

		if dirty {
			slog.Warn("Database is dirty, attempting to force version and retry", "version", version)
			if fErr := m.Force(int(version)); fErr != nil { // nolint:gosec // version is expected to be within int range
				return fmt.Errorf("could not force version %d after dirty state: %w", version, fErr)
			}
			// Retry Up after forcing
			if retryErr := m.Up(); retryErr != nil && retryErr != migrate.ErrNoChange {
				return fmt.Errorf("could not apply migrations after forcing: %w", retryErr)
			}
		} else {
			return fmt.Errorf("could not apply migrations: %w", err)
		}
	}

	slog.Info("Database migrations applied successfully")
	return nil
}
