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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq" // Register PostgreSQL driver
)

// DB is the global database connection pool used throughout the application.
//
// BUG-L01 FIX: Document the global mutable variable pattern. This package-level
// variable is initialized by InitDB() and read by all handlers and services.
// While a global mutable variable is not ideal (it makes testing harder and
// creates hidden coupling), it is the established pattern in this codebase.
// Prefer passing *sql.DB via dependency injection in new code. When the DB
// is nil (e.g. startup failure), handlers must check and return 503.
var DB *sql.DB

// REFACTOR(step 5): remove this compatibility global after all application,
// handler, OCR, and command callers receive an injected database handle.

func InitDB() {
	db, err := Connect()
	if err != nil {
		// Never leave a stale connection visible after a failed initialization.
		// This matters for retrying startup in-process and keeps callers from
		// mistaking a previous pool for the newly requested connection.
		DB = nil
		slog.Error("Database connection failed", "error", err)
		return
	}
	DB = db

	if err := RunMigrations(); err != nil {
		slog.Error("Migration error", "error", err)
	}
}

var sqlOpen = sql.Open

const (
	maxOpenConnections = 20
	maxIdleConnections = 5
	connectionLifetime = 30 * time.Minute
	connectionIdleTime = 5 * time.Minute
)

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
		sslmode = "prefer"
	}
	if !validSSLMode(sslmode) {
		return nil, fmt.Errorf("unsupported DB_SSLMODE %q", sslmode)
	}

	connectionURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   net.JoinHostPort(host, port),
		Path:   "/" + dbname,
	}
	query := connectionURL.Query()
	query.Set("sslmode", sslmode)
	query.Set("connect_timeout", "5")
	query.Set("application_name", "pokget")
	connectionURL.RawQuery = query.Encode()

	db, err := sqlOpen("postgres", connectionURL.String())
	if err != nil {
		return nil, fmt.Errorf("error opening database: %w", err)
	}
	db.SetMaxOpenConns(maxOpenConnections)
	db.SetMaxIdleConns(maxIdleConnections)
	db.SetConnMaxLifetime(connectionLifetime)
	db.SetConnMaxIdleTime(connectionIdleTime)

	if err = db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("error connecting to database: %w", err)
	}

	slog.Info("Successfully connected to PostgreSQL")
	return db, nil
}

func validSSLMode(mode string) bool {
	switch mode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return true
	default:
		return false
	}
}

// ConnectWithRetry tolerates a database container starting shortly after the
// application while keeping startup bounded and cancellation-aware.
func ConnectWithRetry(
	ctx context.Context,
	attempts int,
	baseDelay time.Duration,
) (*sql.DB, error) {
	if attempts < 1 {
		return nil, errors.New("database connection attempts must be positive")
	}
	var connectErrors []error
	for attempt := 0; attempt < attempts; attempt++ {
		database, err := Connect()
		if err == nil {
			return database, nil
		}
		connectErrors = append(connectErrors, err)
		if attempt+1 == attempts {
			break
		}
		delay := baseDelay * time.Duration(1<<min(attempt, 6))
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, errors.Join(ctx.Err(), errors.Join(connectErrors...))
		}
	}
	return nil, fmt.Errorf("database connection failed after %d attempts: %w", attempts, errors.Join(connectErrors...))
}

func RunMigrations() error {
	if DB == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	migrationsPath := os.Getenv("MIGRATIONS_PATH")
	if migrationsPath == "" {
		migrationsPath = "migrations"
	}

	absPath, err := filepath.Abs(migrationsPath)
	if err != nil {
		return err
	}
	return ApplyMigrations(DB, absPath)
}

type migrationRunner interface {
	Up() error
	Version() (uint, bool, error)
	Close() (sourceErr, databaseErr error)
}

var NewMigrator = func(db *sql.DB, absPath string) (migrationRunner, error) {
	sourceDriver, err := iofs.New(os.DirFS(absPath), ".")
	if err != nil {
		return nil, err
	}

	// The migration driver owns and closes its connection. Give it a dedicated
	// connection from the application pool rather than the pool itself;
	// postgres.WithInstance would close the shared *sql.DB when m.Close runs.
	conn, err := db.Conn(context.Background())
	if err != nil {
		_ = sourceDriver.Close()
		return nil, err
	}
	databaseDriver, err := postgres.WithConnection(context.Background(), conn, &postgres.Config{})
	if err != nil {
		_ = conn.Close()
		_ = sourceDriver.Close()
		return nil, err
	}

	return migrate.NewWithInstance("iofs", sourceDriver, "postgres", databaseDriver)
}

func ApplyMigrations(db *sql.DB, absPath string) (retErr error) {
	if db == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	// Verify migrations directory exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("migrations directory not found at: %s", absPath)
	}

	m, err := NewMigrator(db, absPath)
	if err != nil {
		return fmt.Errorf("could not create migration instance: %w", err)
	}
	defer func() {
		sourceErr, databaseErr := m.Close()
		if closeErr := errors.Join(sourceErr, databaseErr); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("could not close migration drivers: %w", closeErr))
		}
	}()

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		version, dirty, vErr := m.Version()
		if vErr != nil {
			return fmt.Errorf("could not apply migrations (and failed to get version): %w (version error: %v)", err, vErr)
		}

		if dirty {
			return fmt.Errorf(
				"database migration version %d is dirty; manual recovery is required before restart: %w",
				version,
				err,
			)
		}
		return fmt.Errorf("could not apply migrations: %w", err)
	}

	slog.Info("Database migrations applied successfully")
	return nil
}
