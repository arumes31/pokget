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
	"os"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestConnect_MissingEnv(t *testing.T) {
	// Clear env
	os.Setenv("DB_HOST", "")

	_, err := Connect()
	if err == nil {
		t.Error("Expected error when env vars are missing")
	}
}

func TestConnect_PingError(t *testing.T) {
	// Set dummy env to pass the first check but fail the ping
	os.Setenv("DB_HOST", "localhost")
	os.Setenv("DB_PORT", "5432")
	os.Setenv("DB_USER", "dummy")
	os.Setenv("DB_PASSWORD", "dummy")
	os.Setenv("DB_NAME", "dummy")
	defer func() {
		os.Unsetenv("DB_HOST")
		os.Unsetenv("DB_PORT")
		os.Unsetenv("DB_USER")
		os.Unsetenv("DB_PASSWORD")
		os.Unsetenv("DB_NAME")
	}()

	_, err := Connect()
	if err == nil {
		t.Error("Expected error when ping fails")
	}
}

func TestSeedDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to open mock db: %v", err)
	}
	defer db.Close()

	// Mock safety check
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	// Mock transaction
	mock.ExpectBegin()
	// SeedDatabase has 4 cards in mockCards
	mock.ExpectExec("INSERT INTO cards").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO cards").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO cards").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO cards").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err = SeedDatabase(db)
	if err != nil {
		t.Errorf("SeedDatabase failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Expectations not met: %v", err)
	}
}

func TestSeedDatabase_Error(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to open mock db: %v", err)
	}
	defer db.Close()

	// Mock safety check to return true
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO cards").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	err = SeedDatabase(db)
	if err == nil {
		t.Error("Expected error from SeedDatabase when Exec fails")
	}
}

func TestInitDB_ConnectionError(t *testing.T) {
	// Set env but invalid connection string (postgres will fail to open or ping)
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "1") // Invalid port
	t.Setenv("DB_USER", "user")
	t.Setenv("DB_PASSWORD", "")
	t.Setenv("DB_NAME", "name")

	staleDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create stale database: %v", err)
	}
	t.Cleanup(func() {
		_ = staleDB.Close()
		DB = nil
	})
	DB = staleDB

	InitDB()

	if DB != nil {
		t.Error("Expected DB to be nil or failed ping when connection is invalid")
	}
}

func TestRunMigrations_NilDB(t *testing.T) {
	DB = nil
	err := RunMigrations()
	if err == nil {
		t.Error("Expected error for nil DB")
	}
}

func TestApplyMigrations_NoDir(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()

	err := ApplyMigrations(db, "/non/existent/path")
	if err == nil {
		t.Error("Expected error for non-existent migration directory")
	}
}

func TestApplyMigrations_NilDatabase(t *testing.T) {
	err := ApplyMigrations(nil, t.TempDir())
	if err == nil {
		t.Fatal("ApplyMigrations() error = nil, want uninitialized database error")
	}
	if !strings.Contains(err.Error(), "database connection is not initialized") {
		t.Fatalf("ApplyMigrations() error = %q, want uninitialized database error", err)
	}
}

type mockMigrator struct {
	err        error
	version    uint
	dirty      bool
	versionErr error
	forced     bool
	closed     bool
}

func (m *mockMigrator) Up() error                    { return m.err }
func (m *mockMigrator) Force(_ int) error            { m.forced = true; return nil }
func (m *mockMigrator) Version() (uint, bool, error) { return m.version, m.dirty, m.versionErr }
func (m *mockMigrator) Close() (error, error)        { m.closed = true; return nil, nil }

func TestApplyMigrations_Success(t *testing.T) {
	dbMock, _, _ := sqlmock.New()
	defer dbMock.Close()

	oldNewMigrator := NewMigrator
	NewMigrator = func(_ *sql.DB, _ string) (migrationRunner, error) {
		return &mockMigrator{err: nil}, nil
	}
	defer func() { NewMigrator = oldNewMigrator }()

	// Use existing dir
	wd, _ := os.Getwd()
	err := ApplyMigrations(dbMock, wd)
	if err != nil {
		t.Errorf("ApplyMigrations failed: %v", err)
	}
}

func TestApplyMigrations_ErrorNew(t *testing.T) {
	dbMock, _, _ := sqlmock.New()
	defer dbMock.Close()

	oldNewMigrator := NewMigrator
	NewMigrator = func(_ *sql.DB, _ string) (migrationRunner, error) {
		return nil, fmt.Errorf("new fail")
	}
	defer func() { NewMigrator = oldNewMigrator }()

	wd, _ := os.Getwd()
	err := ApplyMigrations(dbMock, wd)
	if err == nil {
		t.Error("Expected error from NewMigrator")
	}
}

func TestApplyMigrations_ErrorUp(t *testing.T) {
	dbMock, _, _ := sqlmock.New()
	defer dbMock.Close()

	oldNewMigrator := NewMigrator
	NewMigrator = func(_ *sql.DB, _ string) (migrationRunner, error) {
		return &mockMigrator{err: fmt.Errorf("up fail")}, nil
	}
	defer func() { NewMigrator = oldNewMigrator }()

	wd, _ := os.Getwd()
	err := ApplyMigrations(dbMock, wd)
	if err == nil {
		t.Error("Expected error from migrator.Up")
	}
}

func TestApplyMigrations_DirtyStateRequiresManualRecovery(t *testing.T) {
	dbMock, _, _ := sqlmock.New()
	defer dbMock.Close()

	dirtyMigrator := &mockMigrator{
		err:     fmt.Errorf("partial migration failure"),
		version: 7,
		dirty:   true,
	}
	oldNewMigrator := NewMigrator
	NewMigrator = func(_ *sql.DB, _ string) (migrationRunner, error) {
		return dirtyMigrator, nil
	}
	defer func() { NewMigrator = oldNewMigrator }()

	err := ApplyMigrations(dbMock, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "manual recovery") {
		t.Fatalf("ApplyMigrations() error = %v, want manual recovery error", err)
	}
	if dirtyMigrator.forced {
		t.Fatal("ApplyMigrations() forced a dirty migration version")
	}
	if !dirtyMigrator.closed {
		t.Fatal("ApplyMigrations() did not close the migration drivers")
	}
}

func TestRunMigrations_Success(t *testing.T) {
	dbMock, _, _ := sqlmock.New()
	defer dbMock.Close()
	DB = dbMock
	t.Cleanup(func() { DB = nil })

	oldNewMigrator := NewMigrator
	NewMigrator = func(_ *sql.DB, _ string) (migrationRunner, error) {
		return &mockMigrator{err: nil}, nil
	}
	defer func() { NewMigrator = oldNewMigrator }()

	t.Setenv("MIGRATIONS_PATH", t.TempDir())

	err := RunMigrations()
	if err != nil {
		t.Errorf("RunMigrations failed: %v", err)
	}
}

func TestConnect_Success(t *testing.T) {
	dbMock, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))
	defer dbMock.Close()

	mock.ExpectPing()
	oldSQLOpen := sqlOpen
	sqlOpen = func(_, _ string) (*sql.DB, error) {
		return dbMock, nil
	}
	defer func() { sqlOpen = oldSQLOpen }()

	os.Setenv("DB_HOST", "localhost")
	os.Setenv("DB_PORT", "5432")
	os.Setenv("DB_USER", "u")
	os.Setenv("DB_NAME", "n")
	defer os.Unsetenv("DB_HOST")

	db, err := Connect()
	if err != nil {
		t.Errorf("Connect failed: %v", err)
	}
	if db != dbMock {
		t.Error("Expected mocked DB instance")
	}
	if got := db.Stats().MaxOpenConnections; got != maxOpenConnections {
		t.Errorf("MaxOpenConnections = %d, want %d", got, maxOpenConnections)
	}
}

func TestConnect_OpenError(t *testing.T) {
	oldSQLOpen := sqlOpen
	sqlOpen = func(_, _ string) (*sql.DB, error) {
		return nil, fmt.Errorf("open fail")
	}
	defer func() { sqlOpen = oldSQLOpen }()

	os.Setenv("DB_HOST", "localhost")
	os.Setenv("DB_PORT", "5432")
	os.Setenv("DB_USER", "u")
	os.Setenv("DB_NAME", "n")
	defer os.Unsetenv("DB_HOST")

	_, err := Connect()
	if err == nil {
		t.Error("Expected error from sql.Open")
	}
}

func TestInitDB(t *testing.T) {
	dbMock, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))
	defer dbMock.Close()

	t.Run("Success", func(t *testing.T) {
		mock.ExpectPing()
		oldSQLOpen := sqlOpen
		sqlOpen = func(_, _ string) (*sql.DB, error) {
			return dbMock, nil
		}
		defer func() { sqlOpen = oldSQLOpen }()

		oldNewMigrator := NewMigrator
		NewMigrator = func(_ *sql.DB, _ string) (migrationRunner, error) {
			return &mockMigrator{err: nil}, nil
		}
		defer func() { NewMigrator = oldNewMigrator }()

		t.Setenv("MIGRATIONS_PATH", t.TempDir())

		os.Setenv("DB_HOST", "localhost")
		os.Setenv("DB_PORT", "5432")
		os.Setenv("DB_USER", "u")
		os.Setenv("DB_NAME", "n")
		defer os.Unsetenv("DB_HOST")

		InitDB()
		if DB != dbMock {
			t.Error("Expected global DB to be set")
		}
	})

	t.Run("ConnectFail", func(_ *testing.T) {
		os.Unsetenv("DB_HOST")
		InitDB()
		// Should just log and return
	})
}

func TestSeedDatabase_ColumnNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to open mock db: %v", err)
	}
	defer db.Close()

	// Mock safety check to return false
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	err = SeedDatabase(db)
	if err != nil {
		t.Errorf("SeedDatabase failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Expectations not met: %v", err)
	}
}

func TestSeedDatabase_SafetyCheckError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to open mock db: %v", err)
	}
	defer db.Close()

	// Mock safety check to return error
	mock.ExpectQuery("SELECT EXISTS").WillReturnError(fmt.Errorf("query fail"))

	err = SeedDatabase(db)
	if err != nil {
		t.Errorf("SeedDatabase failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Expectations not met: %v", err)
	}
}
