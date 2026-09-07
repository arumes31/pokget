package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"pokget/internal/auth"

	"github.com/DATA-DOG/go-sqlmock"
)

const editorCurrencyQuery = "SELECT COALESCE(currency, 'EUR') FROM users WHERE id = $1"
const editorBindersQuery = "SELECT id, name FROM binders WHERE user_id = $1 ORDER BY name, id"

func TestEditorMetadataRequiresAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name string
		user any
	}{
		{name: "missing user"},
		{name: "empty user", user: ""},
		{name: "invalid user context", user: 123},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/portfolio/editor-metadata", nil)
			if tc.user != nil {
				req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey{}, tc.user))
			}
			rr := httptest.NewRecorder()
			(&Handler{}).EditorMetadata(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rr.Code)
			}
			if rr.Header().Get("Cache-Control") != "no-store" {
				t.Error("private metadata responses must not be cached")
			}
		})
	}
}

func TestEditorMetadataFreshOwnedBindersAndCurrency(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	h := &Handler{DB: database}

	// Each request must reload metadata. The second snapshot represents a
	// currency change and newly created binder since the editor last opened.
	for _, tc := range []struct {
		name     string
		currency string
		symbol   string
		binders  []map[string]string
	}{
		{"initial editor", "EUR", "€", []map[string]string{{"id": "owned-2", "name": "Zulu"}}},
		{"reopened after changes", "USD", "$", []map[string]string{{"id": "owned-1", "name": "Alpha"}, {"id": "owned-2", "name": "Zulu"}}},
		{"no binders", "EUR", "€", []map[string]string{}},
		{"legacy empty currency", "", "€", []map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock.ExpectQuery(regexp.QuoteMeta(editorCurrencyQuery)).WithArgs("test-user").
				WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow(tc.currency))
			rows := sqlmock.NewRows([]string{"id", "name"})
			for _, binder := range tc.binders {
				rows.AddRow(binder["id"], binder["name"])
			}
			mock.ExpectQuery(regexp.QuoteMeta(editorBindersQuery)).WithArgs("test-user").
				WillReturnRows(rows).RowsWillBeClosed()
			rr := httptest.NewRecorder()
			// User-supplied identity must never override the authenticated owner.
			h.EditorMetadata(rr, authedRequest(t, http.MethodGet, "/portfolio/editor-metadata?user_id=other-user", ""))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
			}
			if rr.Header().Get("Cache-Control") != "no-store" || rr.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected headers: %v", rr.Header())
			}
			var got struct {
				Currency       string              `json:"currency"`
				CurrencySymbol string              `json:"currency_symbol"`
				Binders        []map[string]string `json:"binders"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			wantCurrency := tc.currency
			if wantCurrency == "" {
				wantCurrency = "EUR"
			}
			if got.Currency != wantCurrency || got.CurrencySymbol != tc.symbol || !reflect.DeepEqual(got.Binders, tc.binders) {
				t.Fatalf("unexpected metadata: %+v", got)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEditorMetadataDatabaseFailureDoesNotReturnPartialMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(sqlmock.Sqlmock)
		status int
	}{
		{"user missing", func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(regexp.QuoteMeta(editorCurrencyQuery)).WithArgs("test-user").WillReturnError(sql.ErrNoRows)
		}, http.StatusNotFound},
		{"currency lookup failed", func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(regexp.QuoteMeta(editorCurrencyQuery)).WithArgs("test-user").WillReturnError(errors.New("private database detail"))
		}, http.StatusInternalServerError},
		{"binder lookup failed", func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(regexp.QuoteMeta(editorCurrencyQuery)).WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("EUR"))
			mock.ExpectQuery(regexp.QuoteMeta(editorBindersQuery)).WithArgs("test-user").WillReturnError(errors.New("private database detail"))
		}, http.StatusInternalServerError},
		{"binder scan failed", func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(regexp.QuoteMeta(editorCurrencyQuery)).WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("EUR"))
			mock.ExpectQuery(regexp.QuoteMeta(editorBindersQuery)).WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow("owned-1", nil)).RowsWillBeClosed()
		}, http.StatusInternalServerError},
		{"binder iteration failed", func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(regexp.QuoteMeta(editorCurrencyQuery)).WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("EUR"))
			mock.ExpectQuery(regexp.QuoteMeta(editorBindersQuery)).WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow("owned-1", "Alpha").AddRow("owned-2", "Zulu").RowError(1, errors.New("private database detail"))).RowsWillBeClosed()
		}, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			tc.setup(mock)
			rr := httptest.NewRecorder()
			(&Handler{DB: database}).EditorMetadata(rr, authedRequest(t, http.MethodGet, "/portfolio/editor-metadata", ""))
			if rr.Code != tc.status {
				t.Fatalf("status = %d, want %d", rr.Code, tc.status)
			}
			if rr.Header().Get("Cache-Control") != "no-store" {
				t.Error("failed metadata responses must not be cached")
			}
			if json.Valid(rr.Body.Bytes()) || strings.Contains(rr.Body.String(), "private database detail") {
				t.Fatalf("failure leaked metadata or database details: %s", rr.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
