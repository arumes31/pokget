package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
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

func TestRegisterStaticServesNativeAndContainerOCRAssets(t *testing.T) {
	for _, directory := range []string{"static/vendor/ocr", "dist/static/vendor/ocr"} {
		t.Run(directory, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.MkdirAll(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "fixture.js"), []byte("ocr asset"), 0o600); err != nil {
				t.Fatal(err)
			}
			router := mux.NewRouter()
			registerStaticRoutes(router)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/static/vendor/ocr/fixture.js", nil))
			if response.Code != http.StatusOK || response.Body.String() != "ocr asset" {
				t.Fatalf("response = %d %s", response.Code, response.Body)
			}
		})
	}
}
