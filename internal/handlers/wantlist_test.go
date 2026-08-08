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

// wantlistRerenderExpectations queues the SELECT issued by the Wantlist
// re-render that follows a successful mutation, plus the render-time
// gamification lookup.
func wantlistRerenderExpectations(mock sqlmock.Sqlmock, userID string) {
	mock.ExpectQuery("SELECT").WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "card_id", "tp", "notes", "name", "set", "price_usd", "price_eur", "url"}).
			AddRow("1", "c1", 10.0, "note", "Mew", "151", 15.0, 14.0, "url"))
	mock.ExpectQuery("SELECT xp, rank_title, currency").WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"xp", "rank", "curr"}).AddRow(100, "Novice", "EUR"))
}

func TestUpdateWantlistItem(t *testing.T) {
	t.Run("MethodNotAllowed", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/wantlist/update", nil)
		rr := httptest.NewRecorder()

		h.UpdateWantlistItem(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", rr.Code)
		}
	})

	t.Run("Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/wantlist/update", nil)
		rr := httptest.NewRecorder()

		h.UpdateWantlistItem(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("MissingItemID", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/wantlist/update", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		h.UpdateWantlistItem(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("InvalidTargetPrice", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/wantlist/update", strings.NewReader("item_id=1&target_price=NaN"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		h.UpdateWantlistItem(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/wantlist/update", strings.NewReader("item_id=1&target_price=12.5&notes=note"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE wantlist SET target_price").WithArgs(12.5, "note", "1", "test-user").
			WillReturnError(sql.ErrConnDone)

		h.UpdateWantlistItem(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/wantlist/update", strings.NewReader("item_id=1&target_price=12.5"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE wantlist SET target_price").WithArgs(12.5, "", "1", "test-user").
			WillReturnResult(sqlmock.NewResult(0, 0))

		h.UpdateWantlistItem(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/wantlist/update", strings.NewReader("item_id=1&target_price=12.5&notes=updated"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE wantlist SET target_price").WithArgs(12.5, "updated", "1", "test-user").
			WillReturnResult(sqlmock.NewResult(0, 1))
		wantlistRerenderExpectations(mock, "test-user")

		h.UpdateWantlistItem(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Header().Get("HX-Trigger"), "Wantlist item updated") {
			t.Errorf("Expected update notification trigger, got %q", rr.Header().Get("HX-Trigger"))
		}
	})
}

func TestDeleteWantlistItem(t *testing.T) {
	t.Run("MethodNotAllowed", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/wantlist/delete", nil)
		rr := httptest.NewRecorder()

		h.DeleteWantlistItem(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", rr.Code)
		}
	})

	t.Run("Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/wantlist/delete", nil)
		rr := httptest.NewRecorder()

		h.DeleteWantlistItem(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("MissingItemID", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/wantlist/delete", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		h.DeleteWantlistItem(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodDelete, "/wantlist/delete?item_id=1", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("DELETE FROM wantlist").WithArgs("1", "test-user").
			WillReturnError(sql.ErrConnDone)

		h.DeleteWantlistItem(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/wantlist/delete", strings.NewReader("item_id=1"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("DELETE FROM wantlist").WithArgs("1", "test-user").
			WillReturnResult(sqlmock.NewResult(0, 0))

		h.DeleteWantlistItem(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/wantlist/delete", strings.NewReader("item_id=1"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("DELETE FROM wantlist").WithArgs("1", "test-user").
			WillReturnResult(sqlmock.NewResult(0, 1))
		wantlistRerenderExpectations(mock, "test-user")

		h.DeleteWantlistItem(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Header().Get("HX-Trigger"), "Wantlist item removed") {
			t.Errorf("Expected removal notification trigger, got %q", rr.Header().Get("HX-Trigger"))
		}
	})
}

func TestWantlist_Errors(t *testing.T) {
	t.Run("Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/wantlist", nil)
		rr := httptest.NewRecorder()

		h.Wantlist(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/wantlist", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT").WithArgs("test-user").WillReturnError(sql.ErrConnDone)

		h.Wantlist(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("ScanError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/wantlist", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		// target_price is not numeric, so scanning into float64 fails
		mock.ExpectQuery("SELECT").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "card_id", "tp", "notes", "name", "set", "price_usd", "price_eur", "url"}).
				AddRow("1", "c1", "not-a-price", "note", "Mew", "151", 15.0, 14.0, "url"))

		h.Wantlist(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("RowsError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/wantlist", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		rows := sqlmock.NewRows([]string{"id", "card_id", "tp", "notes", "name", "set", "price_usd", "price_eur", "url"}).
			AddRow("1", "c1", 10.0, "note", "Mew", "151", 15.0, 14.0, "url").
			RowError(0, errors.New("row stream failed"))
		mock.ExpectQuery("SELECT").WithArgs("test-user").WillReturnRows(rows)

		h.Wantlist(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})
}

func TestAddToWantlist_More(t *testing.T) {
	t.Run("MethodNotAllowed", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/wantlist/add", nil)
		rr := httptest.NewRecorder()

		h.AddToWantlist(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", rr.Code)
		}
	})

	t.Run("CardNotFound", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/wantlist/add", strings.NewReader("card_id=c1"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT EXISTS").WithArgs("c1").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

		h.AddToWantlist(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	t.Run("InvalidTargetPrice", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/wantlist/add", strings.NewReader("card_id=c1&target_price=not-a-number"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT EXISTS").WithArgs("c1").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

		h.AddToWantlist(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/wantlist/add", strings.NewReader("card_id=c1&target_price=10.0&notes=note"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT EXISTS").WithArgs("c1").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		mock.ExpectExec("INSERT INTO wantlist").WithArgs("test-user", "c1", 10.0, "note").
			WillReturnResult(sqlmock.NewResult(1, 1))
		wantlistRerenderExpectations(mock, "test-user")

		h.AddToWantlist(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Header().Get("HX-Trigger"), "Grail added") {
			t.Errorf("Expected add notification trigger, got %q", rr.Header().Get("HX-Trigger"))
		}
	})
}
