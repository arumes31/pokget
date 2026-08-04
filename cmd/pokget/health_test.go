package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDatabaseReadinessHandler(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		database, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		mock.ExpectPing()

		response := httptest.NewRecorder()
		databaseReadinessHandler(database).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
		}
	})

	t.Run("database unavailable", func(t *testing.T) {
		database, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		mock.ExpectPing().WillReturnError(errors.New("unavailable"))

		response := httptest.NewRecorder()
		databaseReadinessHandler(database).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("nil database", func(t *testing.T) {
		response := httptest.NewRecorder()
		databaseReadinessHandler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
		}
	})
}
