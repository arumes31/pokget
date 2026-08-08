package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAddCardToPortfolio_Branches(t *testing.T) {
	t.Run("MethodNotAllowed", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/portfolio/add", nil)
		rr := httptest.NewRecorder()

		h.AddCardToPortfolio(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", rr.Code)
		}
	})

	t.Run("Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/portfolio/add", nil)
		rr := httptest.NewRecorder()

		h.AddCardToPortfolio(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("MissingCardID", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/portfolio/add", "")
		rr := httptest.NewRecorder()

		h.AddCardToPortfolio(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("CardNotFound", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/portfolio/add", "card_id=c1")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT EXISTS").WithArgs("c1").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

		h.AddCardToPortfolio(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("BinderValidationError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/portfolio/add", "card_id=c1&binder_id=b1")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT EXISTS").WithArgs("c1").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		mock.ExpectQuery("SELECT EXISTS").WithArgs("b1", "test-user").
			WillReturnError(sql.ErrConnDone)

		h.AddCardToPortfolio(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/portfolio/add", "card_id=c1&notes=note&custom_price=25.5")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT EXISTS").WithArgs("c1").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		// No binder_id supplied and no default binder exists -> NULL binder.
		mock.ExpectQuery("SELECT id FROM binders").WithArgs("test-user").WillReturnError(sql.ErrNoRows)
		mock.ExpectExec("INSERT INTO portfolio").
			WithArgs("test-user", "c1", "", "note", 25.5, "Near Mint", "Raw").
			WillReturnResult(sqlmock.NewResult(1, 1))
		// Gamification: AddXP transaction, then badge checks that find nothing.
		mock.ExpectBegin()
		mock.ExpectQuery("UPDATE users SET xp").WithArgs(100, "test-user").
			WillReturnRows(sqlmock.NewRows([]string{"xp"}).AddRow(100))
		mock.ExpectExec("UPDATE users SET rank_title").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()
		mock.ExpectQuery("SELECT COUNT").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery("SELECT COALESCE").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(0.0))

		h.AddCardToPortfolio(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d; body=%q", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Header().Get("HX-Trigger"), "Asset Secured") {
			t.Errorf("Expected add notification trigger, got %q", rr.Header().Get("HX-Trigger"))
		}
	})
}

func TestUpdatePortfolioBinder_Branches(t *testing.T) {
	t.Run("MethodNotAllowed", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/portfolio/binder", nil)
		rr := httptest.NewRecorder()

		h.UpdatePortfolioBinder(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", rr.Code)
		}
	})

	t.Run("Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/portfolio/binder", nil)
		rr := httptest.NewRecorder()

		h.UpdatePortfolioBinder(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("MissingItemID", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/portfolio/binder", "binder_id=b1")
		rr := httptest.NewRecorder()

		h.UpdatePortfolioBinder(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("BinderValidationError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/portfolio/binder", "item_id=i1&binder_id=b1")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT EXISTS").WithArgs("b1", "test-user").
			WillReturnError(sql.ErrConnDone)

		h.UpdatePortfolioBinder(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("UpdateError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/portfolio/binder", "item_id=i1&binder_id=")
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE portfolio SET binder_id").WithArgs("", "i1", "test-user").
			WillReturnError(sql.ErrConnDone)

		h.UpdatePortfolioBinder(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("UpdateNoRows", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/portfolio/binder", "item_id=i1&binder_id=")
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE portfolio SET binder_id").WithArgs("", "i1", "test-user").
			WillReturnResult(sqlmock.NewResult(0, 0))

		h.UpdatePortfolioBinder(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})
}
