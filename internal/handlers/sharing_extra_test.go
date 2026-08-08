package handlers

import (
	"database/sql"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
)

func TestToggleVisibility_Branches(t *testing.T) {
	t.Run("MethodNotAllowed", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/portfolio/visibility", nil)
		rr := httptest.NewRecorder()

		h.ToggleVisibility(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", rr.Code)
		}
	})

	t.Run("MissingItemID", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/portfolio/visibility", "is_public=true")
		rr := httptest.NewRecorder()

		h.ToggleVisibility(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/portfolio/visibility", "item_id=1&is_public=false")
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE portfolio SET is_public").WithArgs(false, "1", "test-user").
			WillReturnResult(sqlmock.NewResult(0, 0))

		h.ToggleVisibility(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})
}

func TestPublicVault_USDCollectorWithoutAtSign(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create SQL mock: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	templates := template.Must(template.New("public_vault.html").Parse(
		`{{define "public_vault.html"}}{{.CurrencySymbol}}|{{.Username}}|{{len .Portfolio}}{{end}}`,
	))
	handler := &Handler{Templates: templates, DB: database}

	// USD currency flips the symbol; an email without '@' is used as-is.
	mock.ExpectQuery("SELECT id, email").WithArgs("public-user").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "rank_title", "xp", "currency"}).
			AddRow("user-1", "collector", "Collector", 100, "USD"))
	mock.ExpectQuery("SELECT p.id").WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "condition", "format", "grade", "grading_company", "notes",
			"name", "set_name", "price_usd", "price_eur", "image_url", "game",
		}).AddRow(
			"item-1", "Near Mint", "Raw", nil, nil, nil,
			"Test Card", "Test Set", "10.00", "9.00", "/card.png", "pokemon",
		))

	request := httptest.NewRequest(http.MethodGet, "/vault/public-user", nil)
	request = mux.SetURLVars(request, map[string]string{"slug": "public-user"})
	response := httptest.NewRecorder()

	handler.PublicVault(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	if response.Body.String() != "$|collector|1" {
		t.Fatalf("body = %q, want %q", response.Body.String(), "$|collector|1")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicVault_PortfolioErrors(t *testing.T) {
	newVaultHandler := func(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
		templates := template.Must(template.New("public_vault.html").Parse(
			`{{define "public_vault.html"}}vault{{end}}`,
		))
		database, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("create SQL mock: %v", err)
		}
		return &Handler{Templates: templates, DB: database}, mock, func() { _ = database.Close() }
	}

	expectUser := func(mock sqlmock.Sqlmock) {
		mock.ExpectQuery("SELECT id, email").WithArgs("public-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "email", "rank_title", "xp", "currency"}).
				AddRow("user-1", "collector@example.com", "Collector", 100, "EUR"))
	}

	t.Run("QueryError", func(t *testing.T) {
		handler, mock, cleanup := newVaultHandler(t)
		defer cleanup()

		expectUser(mock)
		mock.ExpectQuery("SELECT p.id").WithArgs("user-1").WillReturnError(sql.ErrConnDone)

		request := httptest.NewRequest(http.MethodGet, "/vault/public-user", nil)
		request = mux.SetURLVars(request, map[string]string{"slug": "public-user"})
		response := httptest.NewRecorder()

		handler.PublicVault(response, request)

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("ScanError", func(t *testing.T) {
		handler, mock, cleanup := newVaultHandler(t)
		defer cleanup()

		expectUser(mock)
		mock.ExpectQuery("SELECT p.id").WithArgs("user-1").
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "condition", "format", "grade", "grading_company", "notes",
				"name", "set_name", "price_usd", "price_eur", "image_url", "game",
			}).AddRow(
				"item-1", "Near Mint", "Raw", nil, nil, nil,
				"Test Card", "Test Set", "not-a-price", "9.00", "/card.png", "pokemon",
			))

		request := httptest.NewRequest(http.MethodGet, "/vault/public-user", nil)
		request = mux.SetURLVars(request, map[string]string{"slug": "public-user"})
		response := httptest.NewRecorder()

		handler.PublicVault(response, request)

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("RowsError", func(t *testing.T) {
		handler, mock, cleanup := newVaultHandler(t)
		defer cleanup()

		expectUser(mock)
		rows := sqlmock.NewRows([]string{
			"id", "condition", "format", "grade", "grading_company", "notes",
			"name", "set_name", "price_usd", "price_eur", "image_url", "game",
		}).AddRow(
			"item-1", "Near Mint", "Raw", nil, nil, nil,
			"Test Card", "Test Set", "10.00", "9.00", "/card.png", "pokemon",
		).RowError(0, errors.New("row stream failed"))
		mock.ExpectQuery("SELECT p.id").WithArgs("user-1").WillReturnRows(rows)

		request := httptest.NewRequest(http.MethodGet, "/vault/public-user", nil)
		request = mux.SetURLVars(request, map[string]string{"slug": "public-user"})
		response := httptest.NewRecorder()

		handler.PublicVault(response, request)

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})
}
