package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"pokget/internal/auth"
	"pokget/internal/service"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// newStubLLM returns an LLMService backed by a local HTTP server that responds
// with the given status code and Ollama-style JSON body.
func newStubLLM(t *testing.T, status int, body string) (*service.LLMService, func()) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected LLM request path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	llm, err := service.NewLLMServiceWithConfig(service.LLMConfig{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		server.Close()
		t.Fatalf("create stub LLM service: %v", err)
	}
	return llm, server.Close
}

// expectBinderOwned queues the ownership check and card list used by AutoNameBinder.
func expectBinderOwned(mock sqlmock.Sqlmock, binderID, userID string, cardNames ...string) {
	mock.ExpectQuery("SELECT user_id FROM binders").WithArgs(binderID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(userID))
	rows := sqlmock.NewRows([]string{"name"})
	for _, name := range cardNames {
		rows.AddRow(name)
	}
	mock.ExpectQuery("SELECT c.name").WithArgs(binderID, userID).WillReturnRows(rows)
}

func TestAutoNameBinder(t *testing.T) {
	t.Run("MethodNotAllowed", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/binders/auto-name", nil)
		rr := httptest.NewRecorder()

		h.AutoNameBinder(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", rr.Code)
		}
	})

	t.Run("Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/binders/auto-name", nil)
		rr := httptest.NewRecorder()

		h.AutoNameBinder(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("MissingBinderID", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/binders/auto-name", "")
		rr := httptest.NewRecorder()

		h.AutoNameBinder(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("BinderNotFound", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/binders/auto-name", "binder_id=b1")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT user_id FROM binders").WithArgs("b1").WillReturnError(sql.ErrNoRows)

		h.AutoNameBinder(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	t.Run("OwnershipLookupError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/binders/auto-name", "binder_id=b1")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT user_id FROM binders").WithArgs("b1").WillReturnError(sql.ErrConnDone)

		h.AutoNameBinder(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("ForbiddenForeignBinder", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/binders/auto-name", "binder_id=b1")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT user_id FROM binders").WithArgs("b1").
			WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow("someone-else"))

		h.AutoNameBinder(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rr.Code)
		}
	})

	t.Run("CardsQueryError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/binders/auto-name", "binder_id=b1")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT user_id FROM binders").WithArgs("b1").
			WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow("test-user"))
		mock.ExpectQuery("SELECT c.name").WithArgs("b1", "test-user").WillReturnError(sql.ErrConnDone)

		h.AutoNameBinder(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("EmptyBinder", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/binders/auto-name", "binder_id=b1")
		rr := httptest.NewRecorder()

		expectBinderOwned(mock, "b1", "test-user")

		h.AutoNameBinder(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("CardsRowsError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := authedRequest(t, http.MethodPost, "/binders/auto-name", "binder_id=b1")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT user_id FROM binders").WithArgs("b1").
			WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow("test-user"))
		rows := sqlmock.NewRows([]string{"name"}).AddRow("Pikachu").
			RowError(0, errors.New("row stream failed"))
		mock.ExpectQuery("SELECT c.name").WithArgs("b1", "test-user").WillReturnRows(rows)

		h.AutoNameBinder(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("LLMUnavailable", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()
		h.LLM = nil

		req := authedRequest(t, http.MethodPost, "/binders/auto-name", "binder_id=b1")
		rr := httptest.NewRecorder()

		expectBinderOwned(mock, "b1", "test-user", "Pikachu")

		h.AutoNameBinder(rr, req)

		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("Expected status 503, got %d", rr.Code)
		}
	})

	t.Run("LLMFailure", func(t *testing.T) {
		llm, closeLLM := newStubLLM(t, http.StatusInternalServerError, `{"error":"boom"}`)
		defer closeLLM()

		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()
		h.LLM = llm

		req := authedRequest(t, http.MethodPost, "/binders/auto-name", "binder_id=b1")
		rr := httptest.NewRecorder()

		expectBinderOwned(mock, "b1", "test-user", "Pikachu")

		h.AutoNameBinder(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		llm, closeLLM := newStubLLM(t, http.StatusOK, `{"response":"Shiny Vault"}`)
		defer closeLLM()

		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()
		h.LLM = llm

		req := authedRequest(t, http.MethodPost, "/binders/auto-name", "binder_id=b1")
		rr := httptest.NewRecorder()

		expectBinderOwned(mock, "b1", "test-user", "Pikachu", "Mewtwo")
		mock.ExpectExec("UPDATE binders SET name").WithArgs("Shiny Vault", "b1", "test-user").
			WillReturnResult(sqlmock.NewResult(0, 1))

		h.AutoNameBinder(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d; body=%q", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), `"name":"Shiny Vault"`) {
			t.Errorf("Expected generated name in response, got %q", rr.Body.String())
		}
	})

	t.Run("UpdateDBError", func(t *testing.T) {
		llm, closeLLM := newStubLLM(t, http.StatusOK, `{"response":"Shiny Vault"}`)
		defer closeLLM()

		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()
		h.LLM = llm

		req := authedRequest(t, http.MethodPost, "/binders/auto-name", "binder_id=b1")
		rr := httptest.NewRecorder()

		expectBinderOwned(mock, "b1", "test-user", "Pikachu")
		mock.ExpectExec("UPDATE binders SET name").WithArgs("Shiny Vault", "b1", "test-user").
			WillReturnError(sql.ErrConnDone)

		h.AutoNameBinder(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("UpdateNoRows", func(t *testing.T) {
		llm, closeLLM := newStubLLM(t, http.StatusOK, `{"response":"Shiny Vault"}`)
		defer closeLLM()

		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()
		h.LLM = llm

		req := authedRequest(t, http.MethodPost, "/binders/auto-name", "binder_id=b1")
		rr := httptest.NewRecorder()

		expectBinderOwned(mock, "b1", "test-user", "Pikachu")
		mock.ExpectExec("UPDATE binders SET name").WithArgs("Shiny Vault", "b1", "test-user").
			WillReturnResult(sqlmock.NewResult(0, 0))

		h.AutoNameBinder(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})
}

func TestRefreshCache(t *testing.T) {
	cardColumns := []string{
		"id", "name", "set_name", "price_usd", "price_eur", "image_url", "variant",
		"change_24h", "phash", "game", "language", "rarity", "set_code",
		"collector_number", "catalog_active",
	}

	t.Run("QueryError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()
		h.Fingerprint = nil // skip BK-tree rebuild against the mock DB

		req := httptest.NewRequest(http.MethodPost, "/cache/refresh", nil)
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id, name, set_name").WillReturnError(sql.ErrConnDone)

		h.RefreshCache(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()
		h.Fingerprint = nil

		req := httptest.NewRequest(http.MethodPost, "/cache/refresh", nil)
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id, name, set_name").
			WillReturnRows(sqlmock.NewRows(cardColumns).
				AddRow("c1", "Mew", "151", 10.0, 9.0, "url", "Holo", 1.5, nil, "Pokemon", "en", "R", "sv1", "001", nil))

		h.RefreshCache(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "Successfully reloaded 1 cards") {
			t.Errorf("Expected reload confirmation body, got %q", rr.Body.String())
		}

		h.CardsMu.RLock()
		cards := h.MockCards
		h.CardsMu.RUnlock()
		if len(cards) != 1 || cards[0].ID != "c1" {
			t.Errorf("Expected cache to hold reloaded card c1, got %+v", cards)
		}
	})

	t.Run("SkipsUnscannableRows", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()
		h.Fingerprint = nil

		req := httptest.NewRequest(http.MethodPost, "/cache/refresh", nil)
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id, name, set_name").
			WillReturnRows(sqlmock.NewRows(cardColumns).
				// price_usd not numeric — the row must be skipped, not fatal
				AddRow("bad", "Broken", "151", "not-a-price", 9.0, "url", "", 0.0, nil, "Pokemon", "en", "", "", "", nil).
				AddRow("c1", "Mew", "151", 10.0, 9.0, "url", "", 0.0, nil, "Pokemon", "en", "", "", "", nil))

		h.RefreshCache(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "Successfully reloaded 1 cards") {
			t.Errorf("Expected one valid card to be reloaded, got %q", rr.Body.String())
		}
	})

	t.Run("RowsError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()
		h.Fingerprint = nil

		req := httptest.NewRequest(http.MethodPost, "/cache/refresh", nil)
		rr := httptest.NewRecorder()

		rows := sqlmock.NewRows(cardColumns).
			AddRow("c1", "Mew", "151", 10.0, 9.0, "url", "", 0.0, nil, "Pokemon", "en", "", "", "", nil).
			RowError(0, errors.New("row stream failed"))
		mock.ExpectQuery("SELECT id, name, set_name").WillReturnRows(rows)

		h.RefreshCache(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})
}

func TestIndex_PortfolioViews(t *testing.T) {
	newAuthedIndexRequest := func(t *testing.T, target string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		cookieResponse := httptest.NewRecorder()
		session, err := auth.Store.Get(req, "session")
		if err != nil {
			t.Fatalf("load session: %v", err)
		}
		session.Values["user_id"] = "test-user"
		if err := session.Save(req, cookieResponse); err != nil {
			t.Fatalf("save session: %v", err)
		}
		for _, cookie := range cookieResponse.Result().Cookies() {
			req.AddCookie(cookie)
		}
		return req
	}

	t.Run("WithPortfolioBindersAndBinderView", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := newAuthedIndexRequest(t, "/?view=binders&binder=binder-1")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT currency").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("EUR"))
		mock.ExpectQuery("SELECT p.id").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "condition", "custom_price", "notes", "grade", "is_public", "binder_id",
				"card_id", "name", "set_name", "image_url", "price_usd", "price_eur", "game",
			}).AddRow("p1", "NM", 0.0, "", "", false, "binder-1", "c1", "Mew", "151", "url", 10.0, 9.0, "Pokemon"))
		mock.ExpectQuery("SELECT id, name FROM binders").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow("binder-1", "Main"))
		renderUserDataExpectation(mock, "test-user")

		h.Index(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("PortfolioQueryError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := newAuthedIndexRequest(t, "/")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT currency").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("EUR"))
		mock.ExpectQuery("SELECT p.id").WithArgs("test-user").WillReturnError(sql.ErrConnDone)

		h.Index(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("PortfolioScanError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := newAuthedIndexRequest(t, "/")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT currency").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("EUR"))
		mock.ExpectQuery("SELECT p.id").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "condition", "custom_price", "notes", "grade", "is_public", "binder_id",
				"card_id", "name", "set_name", "image_url", "price_usd", "price_eur", "game",
			}).AddRow("p1", "NM", 0.0, "", "", false, "binder-1", "c1", "Mew", "151", "url", "not-a-price", 9.0, "Pokemon"))

		h.Index(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("PortfolioRowsError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := newAuthedIndexRequest(t, "/")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT currency").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("EUR"))
		rows := sqlmock.NewRows([]string{
			"id", "condition", "custom_price", "notes", "grade", "is_public", "binder_id",
			"card_id", "name", "set_name", "image_url", "price_usd", "price_eur", "game",
		}).AddRow("p1", "NM", 0.0, "", "", false, "binder-1", "c1", "Mew", "151", "url", 10.0, 9.0, "Pokemon").
			RowError(0, errors.New("row stream failed"))
		mock.ExpectQuery("SELECT p.id").WithArgs("test-user").WillReturnRows(rows)

		h.Index(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})
}
