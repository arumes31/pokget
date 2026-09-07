package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"pokget/internal/auth"
	"pokget/internal/service"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestChangePasswordRejectsPasswordBeyondBcryptLimit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
	}{
		{name: "ascii", password: strings.Repeat("a", 73)},
		{name: "multibyte", password: strings.Repeat("界", 25)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, mock, cleanup := setupTestHandler(t)
			defer cleanup()
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT password_hash FROM users").
				WithArgs("test-user").
				WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(fastPasswordHash(t, "current-password")))
			mock.ExpectRollback()
			form := url.Values{
				"current_password": {"current-password"}, "new_password": {tc.password},
				"confirm_password": {tc.password},
			}
			request := authedRequest(t, http.MethodPost, "/settings/change-password", form.Encode())
			response := httptest.NewRecorder()

			h.ChangePassword(response, request)

			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "72 bytes") {
				t.Fatalf("status = %d, body = %q; want 400 explaining the 72-byte limit", response.Code, response.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestChangePasswordRotatesSessionVersion(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	currentHash := fastPasswordHash(t, "current-password")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT password_hash FROM users").
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(currentHash))
	mock.ExpectQuery("UPDATE users").
		WithArgs(sqlmock.AnyArg(), "user-1").
		WillReturnRows(sqlmock.NewRows([]string{"session_version"}).AddRow(8))
	mock.ExpectCommit()
	mock.ExpectExec("INSERT INTO audit_logs").
		WillReturnResult(sqlmock.NewResult(1, 1))

	audit := service.NewAuditService(database)
	handler := &Handler{DB: database, Audit: audit, passwordHasher: fastPasswordHasher}
	form := url.Values{
		"current_password": {"current-password"},
		"new_password":     {"new-password"},
		"confirm_password": {"new-password"},
	}
	request := httptest.NewRequest(http.MethodPost, "/settings/change-password", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request = request.WithContext(context.WithValue(request.Context(), auth.UserContextKey{}, "user-1"))
	cookieResponse := httptest.NewRecorder()
	session, err := auth.Store.Get(request, "session")
	if err != nil {
		t.Fatal(err)
	}
	session.Values["user_id"] = "user-1"
	session.Values["session_version"] = int64(7)
	if err := session.Save(request, cookieResponse); err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Cookie", cookieResponse.Header().Get("Set-Cookie"))

	response := httptest.NewRecorder()
	handler.ChangePassword(response, request)
	audit.Close()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", response.Code, http.StatusOK, response.Body.String())
	}
	if response.Header().Get("HX-Redirect") != "/auth" {
		t.Fatalf("HX-Redirect = %q, want /auth", response.Header().Get("HX-Redirect"))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
