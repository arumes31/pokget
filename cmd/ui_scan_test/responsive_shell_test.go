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
	"github.com/chromedp/chromedp/kb"
)

func TestResponsivePagesAndCollectionFormatting(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed")
	}
	server := newMobileShellServer(t)
	defer server.Close()
	browser := newHeadlessBrowserContext(t, chromePath)
	for _, viewport := range []struct {
		name          string
		width, height int64
	}{{"small-phone", 320, 568}, {"phone", 390, 844}, {"desktop", 1440, 1000}} {
		for _, filled := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/filled-%d", viewport.name, filled), func(t *testing.T) {
				ctx, cancel := chromedp.NewContext(browser)
				defer cancel()
				ctx, timeout := context.WithTimeout(ctx, 60*time.Second)
				defer timeout()
				if err := chromedp.Do(ctx, chromedp.EmulateViewport(viewport.width, viewport.height), chromedp.Navigate(fmt.Sprintf("%s/?filled=%d", server.URL, filled)), chromedp.WaitVisible("#main-content .app-page")); err != nil {
					t.Fatal(err)
				}
				for _, page := range []struct{ name, route string }{{"vault", "/dashboard"}, {"grails", "/wantlist"}, {"binders", "/binders"}, {"binder", "/binders/test"}, {"misprints", "/errors"}, {"trade", "/trade"}, {"settings", "/settings"}} {
					t.Run(page.name, func(t *testing.T) {
						var issues []string
						if err := chromedp.Do(ctx, loadReviewRoute(page.route), evaluateInto(`(() => {
							const issues = [];
							const main = document.querySelector('#main-content');
							if (document.documentElement.scrollWidth > innerWidth + 1) issues.push('horizontal page overflow');
							for (const element of main.querySelectorAll('input:not([type=hidden]), select, textarea, h2, article')) {
								if (!element.getClientRects().length || getComputedStyle(element).display === 'none') continue;
								const box = element.getBoundingClientRect();
								if (box.width > 1 && (box.left < -1 || box.right > innerWidth + 1)) issues.push(element.tagName + ' extends outside viewport');
							}
							if (main.textContent.includes('%!')) issues.push('invalid formatted price');
							return issues;
						})()`, &issues)); err != nil {
							t.Fatal(err)
						}
						if len(issues) != 0 {
							t.Errorf("rendered page issues: %v", issues)
						}
						if dir := os.Getenv("POKGET_UI_SCREENSHOT_DIR"); dir != "" {
							var shot []byte
							if err := chromedp.Do(ctx, captureScreenshotInto(&shot)); err != nil {
								t.Fatal(err)
							}
							if err := os.MkdirAll(dir, 0o750); err != nil {
								t.Fatal(err)
							}
							name := fmt.Sprintf("%s-filled-%d-%s.png", viewport.name, filled, page.name)
							if err := os.WriteFile(filepath.Join(dir, name), shot, 0o600); err != nil {
								t.Fatal(err)
							}
						}
					})
				}
			})
		}
	}
}

func TestCollectionDialogsRestoreKeyboardFocus(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed")
	}
	server := newMobileShellServer(t)
	defer server.Close()
	browser := newHeadlessBrowserContext(t, chromePath)
	for _, page := range []struct{ name, route, trigger, input string }{
		{"binders", "/binders", `[x-ref="newBinderTrigger"]`, `#new-binder-name`},
		{"grails", "/wantlist", `[x-ref="addGrailTrigger"]`, `#grail-card-id`},
	} {
		for _, filled := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/filled-%d", page.name, filled), func(t *testing.T) {
				ctx, cancel := chromedp.NewContext(browser)
				defer cancel()
				ctx, timeout := context.WithTimeout(ctx, 20*time.Second)
				defer timeout()
				if err := chromedp.Do(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(fmt.Sprintf("%s/?filled=%d", server.URL, filled)), chromedp.WaitVisible("#main-content .app-page"), loadReviewRoute(page.route), chromedp.Click(page.trigger), poll(`document.activeElement === document.querySelector('`+page.input+`')`),
					evaluate(`document.activeElement.closest('[role="dialog"]').querySelector('button[type="submit"]').focus()`),
					chromedp.KeyEvent(kb.Tab),
					poll(`Boolean(document.activeElement.closest('[role="dialog"]'))`, chromedp.WithPollingTimeout(2*time.Second)),
					chromedp.KeyEvent(kb.Escape), poll(`document.activeElement === document.querySelector('`+page.trigger+`')`)); err != nil {
					t.Fatalf("dialog keyboard focus: %v", err)
				}
			})
		}
	}
}

func TestStandaloneAccountAndPublicVault(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed")
	}
	server := newMobileShellServer(t)
	defer server.Close()
	browser := newHeadlessBrowserContext(t, chromePath)
	for _, width := range []int64{320, 1440} {
		t.Run(fmt.Sprintf("width-%d", width), func(t *testing.T) {
			ctx, cancel := chromedp.NewContext(browser)
			defer cancel()
			ctx, timeout := context.WithTimeout(ctx, 30*time.Second)
			defer timeout()
			if err := chromedp.Do(ctx, chromedp.EmulateViewport(width, 900), chromedp.Navigate(server.URL+"/?filled=1"), chromedp.WaitVisible("#main-content .app-page")); err != nil {
				t.Fatal(err)
			}
			for _, route := range []struct{ name, path, marker string }{{"auth", "/auth", "#login-tab"}, {"public", "/vault/test", "article"}} {
				var body string
				var overflow bool
				if err := chromedp.Do(ctx, chromedp.Navigate(server.URL+route.path), chromedp.WaitVisible(route.marker), textInto("body", &body), evaluateInto(`document.documentElement.scrollWidth > innerWidth + 1`, &overflow)); err != nil {
					t.Fatal(err)
				}
				if overflow {
					t.Errorf("%s overflows at %dpx", route.name, width)
				}
				if route.name == "auth" {
					if err := chromedp.Do(ctx, chromedp.Focus("#login-tab"), chromedp.KeyEvent(kb.ArrowRight), poll(`document.activeElement.id === 'register-tab' && document.querySelector('#register-tab').getAttribute('aria-selected') === 'true'`)); err != nil {
						t.Fatal(err)
					}
				}
				if route.name == "public" && !strings.Contains(body, "24.95") {
					t.Errorf("public vault did not render decimal price: %s", body)
				}
				if dir := os.Getenv("POKGET_UI_SCREENSHOT_DIR"); dir != "" {
					var shot []byte
					if err := chromedp.Do(ctx, captureScreenshotInto(&shot)); err != nil {
						t.Fatal(err)
					}
					if err := os.MkdirAll(dir, 0o750); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.png", route.name, width)), shot, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func loadReviewRoute(route string) action {
	view := map[string]string{"/dashboard": "home", "/wantlist": "wantlist", "/binders": "binders", "/binders/test": "binders", "/errors": "errors", "/trade": "trade", "/settings": "settings"}[route]
	return chromedp.Steps(
		evaluate(`window.dispatchEvent(new CustomEvent('pokget-view-change', {detail: {view: '`+view+`'}}))`),
		evaluate(`(() => {
			const main = document.querySelector('#main-content');
			delete main.dataset.reviewReady;
			const settled = event => {
				if (event.detail.target !== main) return;
				if (event.detail.requestConfig?.path && event.detail.requestConfig.path !== '`+route+`') return;
				main.dataset.reviewReady = '1';
				document.body.removeEventListener('htmx:afterSettle', settled);
			};
			document.body.addEventListener('htmx:afterSettle', settled);
			htmx.ajax('GET', '`+route+`', {target: main, source: document.body});
		})()`),
		poll(`document.querySelector('#main-content').dataset.reviewReady === '1'`, chromedp.WithPollingTimeout(5*time.Second)),
	)
}

func TestDesktopScanCancelsOlderNavigation(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed")
	}
	fixture := newMobileShellServer(t)
	defer fixture.Close()
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var dashboards atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dashboard" && dashboards.Add(1) == 2 {
			close(started)
			select {
			case <-r.Context().Done():
				close(canceled)
				return
			case <-release:
			}
		}
		fixture.Config.Handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	defer close(release)
	browser := newHeadlessBrowserContext(t, chromePath)
	ctx, timeout := context.WithTimeout(browser, 25*time.Second)
	defer timeout()
	if err := chromedp.Do(ctx, chromedp.EmulateViewport(1440, 1000), chromedp.Navigate(server.URL), chromedp.WaitVisible("#main-content .app-page"), chromedp.Click(`.app-sidebar [hx-get="/dashboard"]`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("older navigation did not start")
	}
	if err := chromedp.Do(ctx, chromedp.Click(`.app-sidebar [hx-get="/centering"]`), chromedp.WaitVisible(".scanner-shell")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("desktop Scan did not cancel the older navigation request")
	}
}
