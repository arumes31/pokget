package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestWantlistPriceAvailabilityUsesPreferredCurrency(t *testing.T) {
	for _, tc := range []struct {
		name, currency     string
		usd, eur           any
		target             float64
		unavailable, ratio bool
	}{
		{"EUR missing despite USD quote", "EUR", 10.0, nil, 20, true, false},
		{"USD missing despite EUR quote", "USD", nil, 10.0, 20, true, false},
		{"explicit zero quote", "EUR", nil, 0.0, 20, false, true},
		{"zero target has no ratio", "USD", 10.0, nil, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, mock, cleanup := setupTestHandler(t)
			defer cleanup()
			h.Templates = parseApplicationTemplates(t)
			mock.ExpectQuery("SELECT w.id").WithArgs("test-user").
				WillReturnRows(sqlmock.NewRows([]string{"id", "cid", "target", "notes", "name", "set", "usd", "eur", "image"}).
					AddRow("w1", "c1", tc.target, "", "Test card", "Set", tc.usd, tc.eur, ""))
			mock.ExpectQuery("SELECT xp, rank_title, currency").WithArgs("test-user").
				WillReturnRows(sqlmock.NewRows([]string{"xp", "rank", "currency"}).AddRow(0, "Novice", tc.currency))
			rr := httptest.NewRecorder()
			h.Wantlist(rr, authedRequest(t, http.MethodGet, "/wantlist", ""))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
			}
			if got := strings.Contains(rr.Body.String(), "Price unavailable"); got != tc.unavailable {
				t.Errorf("unavailable = %t, want %t", got, tc.unavailable)
			}
			if got := strings.Contains(rr.Body.String(), "Market / target"); got != tc.ratio {
				t.Errorf("ratio displayed = %t, want %t", got, tc.ratio)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
