package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"pokget/internal/auth"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
)

func TestCSRFMiddlewareAcceptsSameOriginPlaintextHTTP(t *testing.T) {
	t.Parallel()

	middleware := newCSRFMiddleware([]byte("0123456789abcdef0123456789abcdef"), false)
	handler := middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			_, _ = io.WriteString(writer, csrf.Token(request))
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))

	getRequest := httptest.NewRequest(http.MethodGet, "http://pokget.test/auth", nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET status = %d", getResponse.Code)
	}
	token := strings.TrimSpace(getResponse.Body.String())
	cookies := getResponse.Result().Cookies()
	if token == "" || len(cookies) == 0 {
		t.Fatal("GET did not issue a CSRF token and cookie")
	}

	form := url.Values{"gorilla.csrf.Token": {token}}
	postRequest := httptest.NewRequest(http.MethodPost, "http://pokget.test/auth/login", strings.NewReader(form.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Origin", "http://pokget.test")
	for _, cookie := range cookies {
		postRequest.AddCookie(cookie)
	}
	postResponse := httptest.NewRecorder()
	handler.ServeHTTP(postResponse, postRequest)
	if postResponse.Code != http.StatusNoContent {
		t.Fatalf("POST status = %d, body = %q", postResponse.Code, postResponse.Body.String())
	}
}

func TestGlobalMiddlewareUsesClientIPAndBypassesStaticAssets(t *testing.T) {
	t.Setenv("TRUST_PROXY", "true")
	t.Setenv("TRUST_CLOUDFLARE", "false")
	t.Setenv("RATE_LIMIT", "0.0001")
	t.Setenv("BURST_LIMIT", "1")

	router := mux.NewRouter()
	useGlobalMiddleware(router)
	router.HandleFunc("/resource", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	router.HandleFunc("/static/app.css", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	for _, forwardedIP := range []string{"198.51.100.201", "198.51.100.202"} {
		request := httptest.NewRequest(http.MethodGet, "/resource", nil)
		request.RemoteAddr = "10.0.0.8:1234"
		request.Header.Set("X-Forwarded-For", forwardedIP)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("client %s status = %d, want %d", forwardedIP, response.Code, http.StatusNoContent)
		}
	}

	for range 8 {
		request := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
		request.RemoteAddr = "192.0.2.210:4321"
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("static status = %d, want %d", response.Code, http.StatusNoContent)
		}
	}
}

func TestRegisterScanRouteRequiresVersionedSession(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	identityCSRF := func(next http.Handler) http.Handler { return next }
	router := mux.NewRouter()
	registerScanRoute(
		router,
		database,
		identityCSRF,
		1,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Context().Value(auth.UserContextKey{}); got != "user-1" {
				t.Errorf("context user = %v, want user-1", got)
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	unauthenticated := httptest.NewRecorder()
	router.ServeHTTP(
		unauthenticated,
		httptest.NewRequest(http.MethodPost, "/api/scan", strings.NewReader("payload")),
	)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}

	cookieRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	cookieResponse := httptest.NewRecorder()
	session, err := auth.Store.Get(cookieRequest, "session")
	if err != nil {
		t.Fatal(err)
	}
	session.Values["user_id"] = "user-1"
	session.Values["session_version"] = int64(7)
	if err := session.Save(cookieRequest, cookieResponse); err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery("SELECT COALESCE\\(session_version, 0\\)").
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{"session_version"}).AddRow(7))
	authenticatedRequest := httptest.NewRequest(http.MethodPost, "/api/scan", strings.NewReader("payload"))
	authenticatedRequest.Header.Set("Cookie", cookieResponse.Header().Get("Set-Cookie"))
	authenticatedRequest.RemoteAddr = "203.0.113.210:9876"
	authenticated := httptest.NewRecorder()
	router.ServeHTTP(authenticated, authenticatedRequest)
	if authenticated.Code != http.StatusNoContent {
		t.Fatalf("authenticated status = %d, want %d; body=%q", authenticated.Code, http.StatusNoContent, authenticated.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalFragmentRedirectMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantView   string
		wantBinder string
	}{
		{name: "dashboard", path: "/dashboard", wantView: "home"},
		{name: "wantlist", path: "/wantlist", wantView: "wantlist"},
		{name: "binders", path: "/binders", wantView: "binders"},
		{name: "binder detail", path: "/binders/binder-1", wantView: "binders", wantBinder: "binder-1"},
		{name: "centering", path: "/centering", wantView: "scan"},
		{name: "trade", path: "/trade", wantView: "trade"},
		{name: "settings", path: "/settings", wantView: "settings"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := mux.NewRouter()
			router.Use(canonicalFragmentRedirectMiddleware)
			router.HandleFunc("/binders/{id}", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}).Methods(http.MethodGet)
			router.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}).Methods(http.MethodGet)

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
			}
			location, err := url.Parse(response.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			if got := location.Query().Get("view"); got != test.wantView {
				t.Fatalf("view = %q, want %q", got, test.wantView)
			}
			if got := location.Query().Get("binder"); got != test.wantBinder {
				t.Fatalf("binder = %q, want %q", got, test.wantBinder)
			}
		})
	}

	router := mux.NewRouter()
	router.Use(canonicalFragmentRedirectMiddleware)
	router.HandleFunc("/dashboard", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("HX-Request", "true")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("HTMX status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestCSRFMiddlewareSecureMode(t *testing.T) {
	t.Parallel()

	middleware := newCSRFMiddleware([]byte("0123456789abcdef0123456789abcdef"), true)
	handler := middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, csrf.Token(request))
	}))

	request := httptest.NewRequest(http.MethodGet, "https://pokget.test/auth", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET status = %d", response.Code)
	}
	token := strings.TrimSpace(response.Body.String())
	cookies := response.Result().Cookies()
	if token == "" || len(cookies) == 0 {
		t.Fatal("GET did not issue a CSRF token and cookie")
	}
	if !cookies[0].Secure {
		t.Fatal("secure CSRF cookie missing the Secure flag")
	}
}
