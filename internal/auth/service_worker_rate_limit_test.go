package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServiceWorkerReadsDoNotConsumeApplicationRateLimit(t *testing.T) {
	t.Setenv("RATE_LIMIT", "0.001")
	t.Setenv("BURST_LIMIT", "5")
	const address = "198.51.100.213:4321"
	t.Cleanup(func() {
		mu.Lock()
		delete(limiters, "198.51.100.213")
		mu.Unlock()
	})
	handler := RateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := func(method, path string) int {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = address
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	for range 8 {
		if status := request(http.MethodGet, "/sw.js?v=build"); status != http.StatusNoContent {
			t.Fatalf("service-worker script was rate limited: %d", status)
		}
	}
	for _, path := range []string{"/auth", "/", "/dashboard", "/centering", "/portfolio/editor-metadata"} {
		if status := request(http.MethodGet, path); status != http.StatusNoContent {
			t.Fatalf("static worker reads consumed the application budget for %s: %d", path, status)
		}
	}
	if status := request(http.MethodHead, "/sw.js"); status != http.StatusNoContent {
		t.Fatalf("service-worker HEAD request was rate limited: %d", status)
	}
	for _, path := range []string{"/auth/login", "/api/scan", "/sw.js", "/sw.js/extra"} {
		if status := request(http.MethodPost, path); status != http.StatusTooManyRequests {
			t.Fatalf("application rate limit was bypassed for POST %s: %d", path, status)
		}
	}
	if status := request(http.MethodGet, "/sw.js/extra"); status != http.StatusTooManyRequests {
		t.Fatalf("a service-worker path prefix bypassed the rate limit: %d", status)
	}
}
