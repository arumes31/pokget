package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func TestManifestHandlerVersionsIconURLs(t *testing.T) {
	manifest := `{
  "name": "Pokget",
  "icons": [{"src": "/static/img/icon-192.png", "sizes": "192x192"}],
  "shortcuts": [{"name": "Scan", "url": "/?view=scan", "icons": [{"src": "/static/img/icon-192.png", "sizes": "192x192"}]}]
}`
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	manifestHandler(path, "12345").ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/static/manifest.json", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/manifest+json") {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cacheControl)
	}
	var body struct {
		Icons []struct {
			Src string `json:"src"`
		} `json:"icons"`
		Shortcuts []struct {
			Icons []struct {
				Src string `json:"src"`
			} `json:"icons"`
		} `json:"shortcuts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if len(body.Icons) != 1 || body.Icons[0].Src != "/static/img/icon-192.png?v=12345" {
		t.Fatalf("icons = %+v, want versioned icon", body.Icons)
	}
	if len(body.Shortcuts) != 1 || len(body.Shortcuts[0].Icons) != 1 ||
		body.Shortcuts[0].Icons[0].Src != "/static/img/icon-192.png?v=12345" {
		t.Fatalf("shortcut icons = %+v, want versioned icon", body.Shortcuts)
	}
}

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
			if err := os.MkdirAll(filepath.Join(directory, "7.0.0"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "7.0.0", "fixture.js"), []byte("ocr asset"), 0o600); err != nil {
				t.Fatal(err)
			}
			router := mux.NewRouter()
			registerStaticRoutes(router)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/static/vendor/ocr/7.0.0/fixture.js", nil))
			if response.Code != http.StatusOK || response.Body.String() != "ocr asset" {
				t.Fatalf("response = %d %s", response.Code, response.Body)
			}
			if got := response.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
				t.Fatalf("OCR Cache-Control = %q", got)
			}
		})
	}
}
