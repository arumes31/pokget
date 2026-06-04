package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"pokget/internal/auth"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestWantlistHandlers(t *testing.T) {
	t.Run("Wantlist_Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/wantlist", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT w.id, w.card_id, w.target_price, w.notes, c.name, c.set_name, c.price_usd, c.image_url").
			WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "card_id", "target_price", "notes", "name", "set_name", "price_usd", "image_url"}).
				AddRow("w1", "c1", 10.0, "note", "Mew", "151", 15.0, "url"))

		h.Wantlist(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("Wantlist_Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/wantlist", nil)
		rr := httptest.NewRecorder()

		h.Wantlist(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("Wantlist_DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/wantlist", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT w.id").WithArgs("test-user").
			WillReturnError(errors.New("db error"))

		h.Wantlist(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("AddToWantlist_Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/wantlist/add", strings.NewReader("card_id=c1&target_price=10.0&notes=test+note"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("INSERT INTO wantlist").
			WithArgs("test-user", "c1", "10.0", "test note").
			WillReturnResult(sqlmock.NewResult(1, 1))

		// AddToWantlist calls Wantlist at the end
		mock.ExpectQuery("SELECT w.id").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "card_id", "target_price", "notes", "name", "set_name", "price_usd", "image_url"}))

		h.AddToWantlist(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}

		if trigger := rr.Header().Get("HX-Trigger"); !strings.Contains(trigger, "Identify Success") {
			t.Errorf("Expected HX-Trigger header, got %s", trigger)
		}
	})

	t.Run("AddToWantlist_Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/wantlist/add", nil)
		rr := httptest.NewRecorder()

		h.AddToWantlist(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("AddToWantlist_MethodNotAllowed", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/wantlist/add", nil)
		rr := httptest.NewRecorder()

		h.AddToWantlist(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", rr.Code)
		}
	})

	t.Run("AddToWantlist_MissingCardID", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/wantlist/add", strings.NewReader("target_price=10.0"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		h.AddToWantlist(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("AddToWantlist_DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/wantlist/add", strings.NewReader("card_id=c1"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("INSERT INTO wantlist").WillReturnError(sql.ErrConnDone)

		h.AddToWantlist(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})
}
