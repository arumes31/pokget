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
	"os"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestInitDB_MissingEnv(t *testing.T) {
	// Clear env
	os.Setenv("DB_HOST", "")
	
	InitDB()

	if DB != nil {
		t.Error("Expected DB to be nil when env vars are missing")
	}
}

func TestInitDB_PingError(t *testing.T) {
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

	InitDB()

	if DB != nil {
		t.Error("Expected DB to be nil when ping fails")
	}
}

func TestSeedDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to open mock db: %v", err)
	}
	defer db.Close()

	// SeedDatabase has 4 cards in mockCards
	mock.ExpectExec("INSERT INTO cards").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO cards").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO cards").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO cards").WillReturnResult(sqlmock.NewResult(1, 1))

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

	mock.ExpectExec("INSERT INTO cards").WillReturnError(sql.ErrConnDone)

	err = SeedDatabase(db)
	if err == nil {
		t.Error("Expected error from SeedDatabase when Exec fails")
	}
}

func TestInitDB_ConnectionError(t *testing.T) {
	// Set env but invalid connection string (postgres will fail to open or ping)
	os.Setenv("DB_HOST", "localhost")
	os.Setenv("DB_PORT", "1") // Invalid port
	os.Setenv("DB_USER", "user")
	os.Setenv("DB_NAME", "name")
	defer os.Unsetenv("DB_HOST")
	
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
