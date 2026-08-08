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
	"github.com/gorilla/mux"
)

func TestDeletePortfolioItem(t *testing.T) {
	t.Run("MethodNotAllowed", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/portfolio/delete", nil)
		rr := httptest.NewRecorder()

		h.DeletePortfolioItem(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", rr.Code)
		}
	})

	t.Run("Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/portfolio/delete", nil)
		rr := httptest.NewRecorder()

		h.DeletePortfolioItem(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("MissingItemID", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/portfolio/delete", "")
		rr := httptest.NewRecorder()

		h.DeletePortfolioItem(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/portfolio/delete", "item_id=item-1")
		rr := httptest.NewRecorder()

		mock.ExpectExec("DELETE FROM portfolio").WithArgs("item-1", "test-user").
			WillReturnError(sql.ErrConnDone)

		h.DeletePortfolioItem(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/portfolio/delete", "item_id=item-1")
		rr := httptest.NewRecorder()

		mock.ExpectExec("DELETE FROM portfolio").WithArgs("item-1", "test-user").
			WillReturnResult(sqlmock.NewResult(0, 0))

		h.DeletePortfolioItem(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/portfolio/delete", "item_id=item-1")
		rr := httptest.NewRecorder()

		mock.ExpectExec("DELETE FROM portfolio").WithArgs("item-1", "test-user").
			WillReturnResult(sqlmock.NewResult(0, 1))

		h.DeletePortfolioItem(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "Item deleted successfully") {
			t.Errorf("Expected deletion confirmation body, got %q", rr.Body.String())
		}
		if !strings.Contains(rr.Header().Get("HX-Trigger"), "Card removed from vault") {
			t.Errorf("Expected removal notification trigger, got %q", rr.Header().Get("HX-Trigger"))
		}
	})

	t.Run("SuccessDeleteMethod", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodDelete, "/portfolio/delete?item_id=item-1", nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey{}, "test-user"))
		rr := httptest.NewRecorder()

		mock.ExpectExec("DELETE FROM portfolio").WithArgs("item-1", "test-user").
			WillReturnResult(sqlmock.NewResult(0, 1))

		h.DeletePortfolioItem(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})
}

func TestBinderDetail(t *testing.T) {
	t.Run("Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/binders/b1", nil)
		req = mux.SetURLVars(req, map[string]string{"id": "b1"})
		rr := httptest.NewRecorder()

		h.BinderDetail(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/binders/b1", "")
		req = mux.SetURLVars(req, map[string]string{"id": "b1"})
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id, name, COALESCE").WithArgs("b1", "test-user").
			WillReturnError(sql.ErrNoRows)

		h.BinderDetail(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	t.Run("BinderDBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/binders/b1", "")
		req = mux.SetURLVars(req, map[string]string{"id": "b1"})
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id, name, COALESCE").WithArgs("b1", "test-user").
			WillReturnError(sql.ErrConnDone)

		h.BinderDetail(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("CardsDBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/binders/b1", "")
		req = mux.SetURLVars(req, map[string]string{"id": "b1"})
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id, name, COALESCE").WithArgs("b1", "test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "desc"}).AddRow("b1", "Binder", "Desc"))
		mock.ExpectQuery("SELECT p.id, p.condition").WithArgs("b1", "test-user").
			WillReturnError(sql.ErrConnDone)

		h.BinderDetail(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("CardsScanError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/binders/b1", "")
		req = mux.SetURLVars(req, map[string]string{"id": "b1"})
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id, name, COALESCE").WithArgs("b1", "test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "desc"}).AddRow("b1", "Binder", "Desc"))
		// price_usd is not numeric, so scanning into the decimal fails
		mock.ExpectQuery("SELECT p.id, p.condition").WithArgs("b1", "test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "cond", "price", "cid", "name", "set", "url", "usd", "eur", "game"}).
				AddRow("p1", "NM", nil, "c1", "Mew", "151", "url", "not-a-price", 9.0, "Pokemon"))

		h.BinderDetail(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("CardsRowsError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/binders/b1", "")
		req = mux.SetURLVars(req, map[string]string{"id": "b1"})
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id, name, COALESCE").WithArgs("b1", "test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "desc"}).AddRow("b1", "Binder", "Desc"))
		rows := sqlmock.NewRows([]string{"id", "cond", "price", "cid", "name", "set", "url", "usd", "eur", "game"}).
			AddRow("p1", "NM", nil, "c1", "Mew", "151", "url", 10.0, 9.0, "Pokemon").
			RowError(0, errors.New("row stream failed"))
		mock.ExpectQuery("SELECT p.id, p.condition").WithArgs("b1", "test-user").WillReturnRows(rows)

		h.BinderDetail(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/binders/b1", "")
		req = mux.SetURLVars(req, map[string]string{"id": "b1"})
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id, name, COALESCE").WithArgs("b1", "test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "desc"}).AddRow("b1", "Binder", "Desc"))
		mock.ExpectQuery("SELECT p.id, p.condition").WithArgs("b1", "test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "cond", "price", "cid", "name", "set", "url", "usd", "eur", "game"}).
				AddRow("p1", "NM", nil, "c1", "Mew", "151", "url", 10.0, 9.0, "Pokemon"))
		renderUserDataExpectation(mock, "test-user")

		h.BinderDetail(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d; body=%q", rr.Code, rr.Body.String())
		}
	})
}

func TestBinders_Errors(t *testing.T) {
	t.Run("Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/binders", nil)
		rr := httptest.NewRecorder()

		h.Binders(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/binders", "")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT b.id").WithArgs("test-user").WillReturnError(sql.ErrConnDone)

		h.Binders(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("ScanError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/binders", "")
		rr := httptest.NewRecorder()

		// card_count is not numeric, so scanning into int fails
		mock.ExpectQuery("SELECT b.id").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "desc", "created", "count"}).
				AddRow("b1", "Test Binder", "Desc", "2026-01-01", "not-a-count"))

		h.Binders(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("RowsError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/binders", "")
		rr := httptest.NewRecorder()

		rows := sqlmock.NewRows([]string{"id", "name", "desc", "created", "count"}).
			AddRow("b1", "Test Binder", "Desc", "2026-01-01", 5).
			RowError(0, errors.New("row stream failed"))
		mock.ExpectQuery("SELECT b.id").WithArgs("test-user").WillReturnRows(rows)

		h.Binders(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})
}

func TestTrade_Branches(t *testing.T) {
	t.Run("Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/trade", nil)
		rr := httptest.NewRecorder()

		h.Trade(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/trade", "")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT p.id, p.condition").WithArgs("test-user").
			WillReturnError(sql.ErrConnDone)

		h.Trade(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("ScanError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/trade", "")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT p.id, p.condition").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "condition", "card_id", "name", "set", "price_usd", "price_eur"}).
				AddRow("p1", "NM", "c1", "Mew", "151", "not-a-price", 9.0))

		h.Trade(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("RowsError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/trade", "")
		rr := httptest.NewRecorder()

		rows := sqlmock.NewRows([]string{"id", "condition", "card_id", "name", "set", "price_usd", "price_eur"}).
			AddRow("p1", "NM", "c1", "Mew", "151", 10.0, 9.0).
			RowError(0, errors.New("row stream failed"))
		mock.ExpectQuery("SELECT p.id, p.condition").WithArgs("test-user").WillReturnRows(rows)

		h.Trade(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("SuccessWithItems", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/trade", "")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT p.id, p.condition").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "condition", "card_id", "name", "set", "price_usd", "price_eur"}).
				AddRow("p1", "NM", "c1", "Mew", "151", 10.0, 9.0))
		renderUserDataExpectation(mock, "test-user")

		h.Trade(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})
}
