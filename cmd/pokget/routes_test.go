package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"pokget/internal/handlers"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
)

func TestBuildRouterRegistersRoutes(t *testing.T) {
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := newTestConfig(t)
	router := buildRouter(cfg, database, &handlers.Handler{})
	if router == nil {
		t.Fatal("buildRouter() returned nil")
	}

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/health/live"},
		{http.MethodGet, "/health/ready"},
		{http.MethodGet, "/sw.js"},
		{http.MethodGet, "/static/css/app.css"},
		{http.MethodPost, "/api/scan"},
		{http.MethodGet, "/"},
		{http.MethodGet, "/auth"},
		{http.MethodPost, "/auth/register"},
		{http.MethodPost, "/auth/login"},
		{http.MethodPost, "/auth/resend"},
		{http.MethodGet, "/auth/confirm"},
		{http.MethodPost, "/auth/confirm"},
		{http.MethodPost, "/auth/logout"},
		{http.MethodGet, "/vault/some-slug"},
		{http.MethodGet, "/errors"},
		{http.MethodGet, "/dashboard"},
		{http.MethodGet, "/centering"},
		{http.MethodGet, "/binders"},
		{http.MethodPost, "/binders/create"},
		{http.MethodPost, "/binders/auto-name"},
		{http.MethodGet, "/binders/binder-1"},
		{http.MethodGet, "/trade"},
		{http.MethodGet, "/settings"},
		{http.MethodPost, "/settings"},
		{http.MethodPost, "/settings/change-password"},
		{http.MethodPost, "/settings/public-profile"},
		{http.MethodPost, "/portfolio/add"},
		{http.MethodPost, "/portfolio/edit"},
		{http.MethodPost, "/portfolio/delete"},
		{http.MethodDelete, "/portfolio/delete"},
		{http.MethodPost, "/portfolio/binder"},
		{http.MethodPost, "/portfolio/toggle-visibility"},
		{http.MethodGet, "/wantlist"},
		{http.MethodPost, "/wantlist/add"},
		{http.MethodPost, "/wantlist/edit"},
		{http.MethodPost, "/wantlist/delete"},
		{http.MethodDelete, "/wantlist/delete"},
		{http.MethodPost, "/errors/submit"},
		{http.MethodPost, "/api/gamification/heartbeat"},
		{http.MethodPost, "/api/portfolio/add"},
		{http.MethodPost, "/api/admin/refresh-cache"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			request := httptest.NewRequest(route.method, route.path, nil)
			match := &mux.RouteMatch{}
			if !router.Match(request, match) {
				t.Fatalf("no route registered for %s %s", route.method, route.path)
			}
		})
	}
}

func TestBuildRouterRejectsUnregisteredRoutes(t *testing.T) {
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	router := buildRouter(newTestConfig(t), database, &handlers.Handler{})

	rejected := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/health/live"},            // health is GET only
		{http.MethodGet, "/api/scan"},                // scan is POST only
		{http.MethodDelete, "/dashboard"},            // dashboard is GET only
		{http.MethodGet, "/no-such-page"},            // unknown path
		{http.MethodGet, "/api/admin/refresh-cache"}, // admin refresh is POST only
	}

	for _, route := range rejected {
		request := httptest.NewRequest(route.method, route.path, nil)
		if router.Match(request, &mux.RouteMatch{}) {
			t.Errorf("unexpected route match for %s %s", route.method, route.path)
		}
	}
}

func TestBuildRouterServesLiveness(t *testing.T) {
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	router := buildRouter(newTestConfig(t), database, &handlers.Handler{})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("GET /health/live status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want %q", got, "no-store")
	}
}
