package handlers

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pokget/internal/auth"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
)

func TestExportPortfolioCSV(t *testing.T) {
	t.Run("MethodNotAllowed", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/portfolio/export", nil)
		rr := httptest.NewRecorder()

		h.ExportPortfolioCSV(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", rr.Code)
		}
	})

	t.Run("Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/portfolio/export", nil)
		rr := httptest.NewRecorder()

		h.ExportPortfolioCSV(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", rr.Code)
		}
	})

	t.Run("Success_Empty", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/portfolio/export", nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey{}, "user-123"))
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT currency FROM users").WithArgs("user-123").
			WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("USD"))

		mock.ExpectQuery(`SELECT c.name, COALESCE\(c.game.*FROM portfolio p.*WHERE p.user_id = \$1`).
			WithArgs("user-123").
			WillReturnRows(sqlmock.NewRows([]string{
				"name", "game", "set_name", "set_code", "collector_number",
				"condition", "grade", "grading_company",
				"custom_price", "price_usd", "price_eur", "notes",
				"binder_name", "is_public", "added_at",
			}))

		h.ExportPortfolioCSV(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
		}

		if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
			t.Errorf("expected text/csv Content-Type, got %q", ct)
		}
		if cd := rr.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment; filename=\"pokget-vault-") {
			t.Errorf("expected attachment Content-Disposition, got %q", cd)
		}

		reader := csv.NewReader(rr.Body)
		records, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("failed to read CSV: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("expected 1 record (header), got %d", len(records))
		}
		expectedHeaders := []string{
			"Card Name", "Game", "Set", "Set Code", "Collector Number",
			"Condition", "Grade", "Grading Company",
			"Custom Price", "Market Price", "Currency", "Notes", "Binder", "Public", "Added At",
		}
		for i, h := range expectedHeaders {
			if records[0][i] != h {
				t.Errorf("expected header %q, got %q", h, records[0][i])
			}
		}
	})

	t.Run("Success_WithCards", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/portfolio/export", nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey{}, "user-123"))
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT currency FROM users").WithArgs("user-123").
			WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("EUR"))

		addedTime := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
		rows := sqlmock.NewRows([]string{
			"name", "game", "set_name", "set_code", "collector_number",
			"condition", "grade", "grading_company",
			"custom_price", "price_usd", "price_eur", "notes",
			"binder_name", "is_public", "added_at",
		}).AddRow(
			"Charizard ex", "pokemon", "151", "sv3pt5", "199",
			"NM", "PSA 10", "PSA",
			sql.NullFloat64{Float64: 250.00, Valid: true},
			decimal.NewFromFloat(180.50), decimal.NewFromFloat(165.25), "Pulled from booster bundle",
			"Grails Binder", true, addedTime,
		).AddRow(
			"Pikachu", "pokemon", "Crown Zenith", "swsh12pt5", "160",
			"LP", "", "",
			sql.NullFloat64{Valid: false},
			decimal.NewFromFloat(15.00), decimal.NewFromFloat(13.80), "",
			"", false, addedTime,
		)

		mock.ExpectQuery(`SELECT c.name, COALESCE\(c.game.*FROM portfolio p.*WHERE p.user_id = \$1`).
			WithArgs("user-123").
			WillReturnRows(rows)

		h.ExportPortfolioCSV(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
		}

		reader := csv.NewReader(rr.Body)
		records, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("failed to read CSV: %v", err)
		}
		if len(records) != 3 {
			t.Fatalf("expected 3 records (1 header + 2 cards), got %d", len(records))
		}

		// First card: Charizard ex
		row1 := records[1]
		if row1[0] != "Charizard ex" || row1[1] != "pokemon" || row1[2] != "151" || row1[3] != "sv3pt5" || row1[4] != "199" {
			t.Errorf("unexpected card metadata: %+v", row1[:5])
		}
		if row1[5] != "NM" || row1[6] != "PSA 10" || row1[7] != "PSA" {
			t.Errorf("unexpected condition/grade: %+v", row1[5:8])
		}
		if row1[8] != "250.00" || row1[9] != "165.25" || row1[10] != "EUR" {
			t.Errorf("unexpected pricing (EUR preference): custom=%s market=%s curr=%s", row1[8], row1[9], row1[10])
		}
		if row1[11] != "Pulled from booster bundle" || row1[12] != "Grails Binder" || row1[13] != "Yes" {
			t.Errorf("unexpected notes/binder/public: %+v", row1[11:14])
		}

		// Second card: Pikachu
		row2 := records[2]
		if row2[0] != "Pikachu" || row2[5] != "LP" || row2[6] != "" || row2[7] != "" {
			t.Errorf("unexpected card 2 info: %+v", row2[:8])
		}
		if row2[8] != "" || row2[9] != "13.80" || row2[10] != "EUR" {
			t.Errorf("unexpected card 2 pricing: custom=%s market=%s curr=%s", row2[8], row2[9], row2[10])
		}
		if row2[12] != "" || row2[13] != "No" {
			t.Errorf("unexpected card 2 binder/public: binder=%s public=%s", row2[12], row2[13])
		}
	})

	t.Run("DatabaseError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/portfolio/export", nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey{}, "user-123"))
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT currency FROM users").WithArgs("user-123").
			WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("USD"))

		mock.ExpectQuery(`SELECT c.name, COALESCE\(c.game.*FROM portfolio p.*WHERE p.user_id = \$1`).
			WithArgs("user-123").
			WillReturnError(errors.New("db connection failure"))

		h.ExportPortfolioCSV(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status 500, got %d", rr.Code)
		}
	})
}
