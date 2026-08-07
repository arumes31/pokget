package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestServiceWorkerInstallUpdateAndOfflineNavigation(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed; skipping service-worker lifecycle check")
	}

	server, workerVersion, networkFailure := newServiceWorkerTestServer(t)
	defer server.Close()
	browserContext := newHeadlessBrowserContext(t, chromePath)
	tabContext, cancelTab := chromedp.NewContext(browserContext)
	defer cancelTab()
	ctx, cancel := context.WithTimeout(tabContext, 75*time.Second)
	defer cancel()

	var initialCaches []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL),
		chromedp.Poll(`navigator.serviceWorker.ready.then(() => Boolean(navigator.serviceWorker.controller))`, nil,
			chromedp.WithPollingTimeout(20*time.Second)),
		chromedp.Evaluate(`window.__pokgetCacheNames = []; caches.keys().then((names) => { window.__pokgetCacheNames = names; })`, nil),
		chromedp.Poll(`window.__pokgetCacheNames.length > 0`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`window.__pokgetCacheNames`, &initialCaches),
	); err != nil {
		t.Fatalf("install tracked service worker: %v", err)
	}
	if !containsString(initialCaches, "pokget-shell-v7") {
		t.Fatalf("initial cache names = %v, want pokget-shell-v7", initialCaches)
	}

	workerVersion.Store(1)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`navigator.serviceWorker.getRegistration().then((registration) => registration.update())`, nil),
		chromedp.Poll(`caches.keys().then((names) => names.includes('pokget-shell-v8-test') && !names.includes('pokget-shell-v7'))`, nil,
			chromedp.WithPollingTimeout(20*time.Second)),
	); err != nil {
		t.Fatalf("activate service-worker update and remove stale cache: %v", err)
	}

	networkFailure.Store(true)
	var offlineBody string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL+"/offline-probe"),
		chromedp.Text("body", &offlineBody, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate through service worker while offline: %v", err)
	}
	if !strings.Contains(strings.ToLower(offlineBody), "private vault need a network connection") {
		t.Fatalf("offline navigation body = %q", offlineBody)
	}
}

func newServiceWorkerTestServer(t *testing.T) (*httptest.Server, *atomic.Int32, *atomic.Bool) {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	workerSource, err := os.ReadFile(filepath.Join(root, "static", "js", "sw.js"))
	if err != nil {
		t.Fatalf("read tracked service worker: %v", err)
	}

	var version atomic.Int32
	var networkFailure atomic.Bool
	staticFiles := http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(root, "static"))))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/":
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(writer, `<!doctype html><html><body>online<script>
				navigator.serviceWorker.register('/sw.js', {scope: '/', updateViaCache: 'none'});
			</script></body></html>`)
		case "/sw.js":
			writer.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			writer.Header().Set("Service-Worker-Allowed", "/")
			source := string(workerSource)
			if version.Load() > 0 {
				source = strings.Replace(source, "shell-v7", "shell-v8-test", 1)
			}
			_, _ = fmt.Fprint(writer, source)
		case "/offline-probe":
			if networkFailure.Load() {
				hijacker, ok := writer.(http.Hijacker)
				if !ok {
					t.Error("test server does not support connection hijacking")
					return
				}
				connection, _, hijackErr := hijacker.Hijack()
				if hijackErr != nil {
					t.Errorf("hijack offline test connection: %v", hijackErr)
					return
				}
				_ = connection.Close()
				return
			}
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(writer, "network response")
		default:
			staticFiles.ServeHTTP(writer, request)
		}
	}))
	return server, &version, &networkFailure
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
