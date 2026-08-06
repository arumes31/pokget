package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceWorkerHandlerUsesRootScopeHeaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sw.js")
	if err := os.WriteFile(path, []byte("self.addEventListener('fetch', () => {});"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/sw.js", nil)
	response := httptest.NewRecorder()
	serviceWorkerHandler(path).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/javascript") {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cacheControl)
	}
}
