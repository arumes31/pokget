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

func authedRequest(t *testing.T, method, target, form string) *http.Request {
	t.Helper()

	var req *http.Request
	if form == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return req.WithContext(context.WithValue(req.Context(), auth.UserContextKey{}, "test-user"))
}

// settingsLoadExpectation queues the user row lookup performed by Settings.
func settingsLoadExpectation(mock sqlmock.Sqlmock, userID string) {
	mock.ExpectQuery("SELECT email, COALESCE").WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"email", "currency", "slug", "public"}).
			AddRow("test@example.com", "EUR", "collector-1", true))
}

// renderUserDataExpectation queues the gamification lookup done by render.
func renderUserDataExpectation(mock sqlmock.Sqlmock, userID string) {
	mock.ExpectQuery("SELECT xp, rank_title, currency").WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"xp", "rank", "curr"}).AddRow(100, "Novice", "EUR"))
}

func TestSettings(t *testing.T) {
	t.Run("Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/settings", nil)
		rr := httptest.NewRecorder()

		h.Settings(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("GetSuccess", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/settings", "")
		rr := httptest.NewRecorder()

		settingsLoadExpectation(mock, "test-user")
		renderUserDataExpectation(mock, "test-user")

		h.Settings(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("GetUserNotFound", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/settings", "")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT email, COALESCE").WithArgs("test-user").WillReturnError(sql.ErrNoRows)

		h.Settings(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	t.Run("GetDBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/settings", "")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT email, COALESCE").WithArgs("test-user").WillReturnError(sql.ErrConnDone)

		h.Settings(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("PostInvalidCurrency", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings", "currency=BTC")
		rr := httptest.NewRecorder()

		h.Settings(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("PostUpdateError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings", "currency=USD")
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE users SET currency").WithArgs("USD", "test-user").
			WillReturnError(sql.ErrConnDone)

		h.Settings(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("PostSuccess", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings", "currency=USD")
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE users SET currency").WithArgs("USD", "test-user").
			WillReturnResult(sqlmock.NewResult(0, 1))
		settingsLoadExpectation(mock, "test-user")
		renderUserDataExpectation(mock, "test-user")

		h.Settings(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Header().Get("HX-Trigger"), "Settings Updated") {
			t.Errorf("Expected settings notification trigger, got %q", rr.Header().Get("HX-Trigger"))
		}
	})

	t.Run("GetHTMXFragment", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodGet, "/settings", "")
		req.Header.Set("HX-Request", "true")
		rr := httptest.NewRecorder()

		settingsLoadExpectation(mock, "test-user")

		h.Settings(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "settings_fragment") {
			t.Errorf("Expected settings fragment body, got %q", rr.Body.String())
		}
	})
}

func TestUpdatePublicProfile(t *testing.T) {
	t.Run("MethodNotAllowed", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/settings/public-profile", nil)
		rr := httptest.NewRecorder()

		h.UpdatePublicProfile(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", rr.Code)
		}
	})

	t.Run("Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/settings/public-profile", nil)
		rr := httptest.NewRecorder()

		h.UpdatePublicProfile(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("UserNotFound", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings/public-profile", "is_public_profile=true")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT COALESCE").WithArgs("test-user").WillReturnError(sql.ErrNoRows)

		h.UpdatePublicProfile(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	t.Run("LookupDBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings/public-profile", "is_public_profile=true")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT COALESCE").WithArgs("test-user").WillReturnError(sql.ErrConnDone)

		h.UpdatePublicProfile(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("UpdateDBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings/public-profile", "is_public_profile=true")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT COALESCE").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"slug"}).AddRow("collector-1"))
		mock.ExpectExec("UPDATE users SET public_slug").WithArgs("collector-1", true, "test-user").
			WillReturnError(sql.ErrConnDone)

		h.UpdatePublicProfile(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("UpdateNoRows", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings/public-profile", "is_public_profile=true")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT COALESCE").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"slug"}).AddRow("collector-1"))
		mock.ExpectExec("UPDATE users SET public_slug").WithArgs("collector-1", true, "test-user").
			WillReturnResult(sqlmock.NewResult(0, 0))

		h.UpdatePublicProfile(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	t.Run("EnableGeneratesSlug", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings/public-profile", "is_public_profile=true")
		rr := httptest.NewRecorder()

		// No existing slug — the handler must generate a random collector- slug.
		mock.ExpectQuery("SELECT COALESCE").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"slug"}).AddRow(""))
		mock.ExpectExec("UPDATE users SET public_slug").WithArgs(sqlmock.AnyArg(), true, "test-user").
			WillReturnResult(sqlmock.NewResult(0, 1))
		settingsLoadExpectation(mock, "test-user")
		renderUserDataExpectation(mock, "test-user")

		h.UpdatePublicProfile(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Header().Get("HX-Trigger"), "Public profile updated") {
			t.Errorf("Expected public profile notification trigger, got %q", rr.Header().Get("HX-Trigger"))
		}
	})

	t.Run("DisableKeepsSlug", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings/public-profile", "")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT COALESCE").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"slug"}).AddRow("collector-1"))
		mock.ExpectExec("UPDATE users SET public_slug").WithArgs("collector-1", false, "test-user").
			WillReturnResult(sqlmock.NewResult(0, 1))
		settingsLoadExpectation(mock, "test-user")
		renderUserDataExpectation(mock, "test-user")

		h.UpdatePublicProfile(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})
}

func TestChangePasswordBranches(t *testing.T) {
	t.Run("MethodNotAllowed", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/settings/change-password", nil)
		rr := httptest.NewRecorder()

		h.ChangePassword(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", rr.Code)
		}
	})

	t.Run("Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/settings/change-password", nil)
		rr := httptest.NewRecorder()

		h.ChangePassword(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("MissingFields", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings/change-password", "current_password=x")
		rr := httptest.NewRecorder()

		h.ChangePassword(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("Mismatch", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings/change-password",
			"current_password=x&new_password=a&confirm_password=b")
		rr := httptest.NewRecorder()

		h.ChangePassword(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "do not match") {
			t.Errorf("Expected mismatch message, got %q", rr.Body.String())
		}
	})

	t.Run("BeginError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings/change-password",
			"current_password=x&new_password=a&confirm_password=a")
		rr := httptest.NewRecorder()

		mock.ExpectBegin().WillReturnError(errors.New("begin failed"))

		h.ChangePassword(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("UserNotFound", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings/change-password",
			"current_password=x&new_password=a&confirm_password=a")
		rr := httptest.NewRecorder()

		mock.ExpectBegin()
		mock.ExpectQuery("SELECT password_hash FROM users").WithArgs("test-user").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()

		h.ChangePassword(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("LookupDBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings/change-password",
			"current_password=x&new_password=a&confirm_password=a")
		rr := httptest.NewRecorder()

		mock.ExpectBegin()
		mock.ExpectQuery("SELECT password_hash FROM users").WithArgs("test-user").
			WillReturnError(sql.ErrConnDone)
		mock.ExpectRollback()

		h.ChangePassword(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("WrongCurrentPassword", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings/change-password",
			"current_password=wrong&new_password=a&confirm_password=a")
		rr := httptest.NewRecorder()

		mock.ExpectBegin()
		mock.ExpectQuery("SELECT password_hash FROM users").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(fastPasswordHash(t, "actual")))
		mock.ExpectRollback()

		h.ChangePassword(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("HashError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()
		h.passwordHasher = func(string) (string, error) { return "", errors.New("hash failed") }

		req := authedRequest(t, http.MethodPost, "/settings/change-password",
			"current_password=current&new_password=a&confirm_password=a")
		rr := httptest.NewRecorder()

		mock.ExpectBegin()
		mock.ExpectQuery("SELECT password_hash FROM users").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(fastPasswordHash(t, "current")))
		mock.ExpectRollback()

		h.ChangePassword(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("UpdateError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings/change-password",
			"current_password=current&new_password=a&confirm_password=a")
		rr := httptest.NewRecorder()

		mock.ExpectBegin()
		mock.ExpectQuery("SELECT password_hash FROM users").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(fastPasswordHash(t, "current")))
		mock.ExpectQuery("UPDATE users").WithArgs(sqlmock.AnyArg(), "test-user").
			WillReturnError(sql.ErrConnDone)
		mock.ExpectRollback()

		h.ChangePassword(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("CommitError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/settings/change-password",
			"current_password=current&new_password=a&confirm_password=a")
		rr := httptest.NewRecorder()

		mock.ExpectBegin()
		mock.ExpectQuery("SELECT password_hash FROM users").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(fastPasswordHash(t, "current")))
		mock.ExpectQuery("UPDATE users").WithArgs(sqlmock.AnyArg(), "test-user").
			WillReturnRows(sqlmock.NewRows([]string{"session_version"}).AddRow(2))
		mock.ExpectCommit().WillReturnError(errors.New("commit failed"))

		h.ChangePassword(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}
