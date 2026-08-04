package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"pokget/internal/auth"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestParseFiniteFloat(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "valid", value: "12.34"},
		{name: "negative", value: "-1", wantErr: true},
		{name: "too large", value: "10000000000", wantErr: true},
		{name: "NaN", value: "NaN", wantErr: true},
		{name: "positive infinity", value: "+Inf", wantErr: true},
		{name: "negative infinity", value: "-Inf", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseFiniteFloat(test.value, 0, maxStoredPrice)
			if (err != nil) != test.wantErr {
				t.Fatalf("parseFiniteFloat(%q) error = %v, wantErr %t", test.value, err, test.wantErr)
			}
		})
	}
}

func TestMoneyHandlersRejectNonFiniteValues(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		form        url.Values
		needsCardDB bool
		invoke      func(*Handler, http.ResponseWriter, *http.Request)
	}{
		{
			name:        "portfolio custom price",
			path:        "/portfolio/add",
			form:        url.Values{"card_id": {"card-1"}, "custom_price": {"NaN"}},
			needsCardDB: true,
			invoke:      (*Handler).AddCardToPortfolio,
		},
		{
			name:        "wantlist target price",
			path:        "/wantlist/add",
			form:        url.Values{"card_id": {"card-1"}, "target_price": {"+Inf"}},
			needsCardDB: true,
			invoke:      (*Handler).AddToWantlist,
		},
		{
			name:   "error multiplier",
			path:   "/errors/submit",
			form:   url.Values{"card_id": {"card-1"}, "error_type": {"miscut"}, "description": {"test"}, "multiplier": {"NaN"}},
			invoke: (*Handler).SubmitError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			if test.needsCardDB {
				mock.ExpectQuery("SELECT EXISTS").
					WithArgs("card-1").
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
			}

			handler := &Handler{DB: database}
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request = request.WithContext(context.WithValue(request.Context(), auth.UserContextKey{}, "user-1"))
			response := httptest.NewRecorder()
			test.invoke(handler, response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAddCardToPortfolioRejectsForeignBinder(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("card-1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("binder-owned-by-another-user", "user-1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	form := url.Values{
		"card_id":   {"card-1"},
		"binder_id": {"binder-owned-by-another-user"},
	}
	request := httptest.NewRequest(http.MethodPost, "/portfolio/add", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request = request.WithContext(context.WithValue(request.Context(), auth.UserContextKey{}, "user-1"))
	response := httptest.NewRecorder()
	(&Handler{DB: database}).AddCardToPortfolio(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAddCardToPortfolioFailsOnDefaultBinderDatabaseError(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("card-1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("SELECT id FROM binders").
		WithArgs("user-1").
		WillReturnError(sql.ErrConnDone)

	form := url.Values{"card_id": {"card-1"}}
	request := httptest.NewRequest(http.MethodPost, "/portfolio/add", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request = request.WithContext(context.WithValue(request.Context(), auth.UserContextKey{}, "user-1"))
	response := httptest.NewRecorder()
	(&Handler{DB: database}).AddCardToPortfolio(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIndexFailsWhenCurrencyLookupFails(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	mock.ExpectQuery("SELECT currency").
		WithArgs("user-1").
		WillReturnError(sql.ErrConnDone)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	cookieResponse := httptest.NewRecorder()
	session, err := auth.Store.Get(request, "session")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	session.Values["user_id"] = "user-1"
	if err := session.Save(request, cookieResponse); err != nil {
		t.Fatalf("save session: %v", err)
	}
	for _, cookie := range cookieResponse.Result().Cookies() {
		request.AddCookie(cookie)
	}

	response := httptest.NewRecorder()
	(&Handler{DB: database}).Index(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
