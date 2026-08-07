package main

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

type mobilePageLayout struct {
	ViewportWidth float64 `json:"viewportWidth"`
	ScrollWidth   float64 `json:"scrollWidth"`
	PageLeft      float64 `json:"pageLeft"`
	PageRight     float64 `json:"pageRight"`
	HeaderLeft    float64 `json:"headerLeft"`
	HeaderRight   float64 `json:"headerRight"`
	NavLeft       float64 `json:"navLeft"`
	NavRight      float64 `json:"navRight"`
	NavItems      int     `json:"navItems"`
	SmallTargets  int     `json:"smallTargets"`
}

func TestMobilePrimaryPagesStayInsideViewport(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed; skipping rendered mobile shell check")
	}

	server := newMobileShellServer(t)
	defer server.Close()
	browserContext := newHeadlessBrowserContext(t, chromePath)
	tabContext, cancelTab := chromedp.NewContext(browserContext)
	defer cancelTab()
	ctx, cancel := context.WithTimeout(tabContext, 45*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(server.URL),
		chromedp.Poll(`document.querySelector('#main-content .app-page') && document.querySelector('#main-content').textContent.includes('Main vault')`, nil,
			chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("open mobile shell: %v", err)
	}

	pages := []struct {
		name, route, marker string
	}{
		{name: "vault", route: "/dashboard", marker: "Main vault"},
		{name: "grails", route: "/wantlist", marker: "Grails"},
		{name: "binders", route: "/binders", marker: "Binders"},
		{name: "misprints", route: "/errors", marker: "Misprints"},
		{name: "trade", route: "/trade", marker: "Trade analyzer"},
	}
	for _, page := range pages {
		t.Run(page.name, func(t *testing.T) {
			var layout mobilePageLayout
			var screenshot []byte
			marker := strings.ReplaceAll(page.marker, "'", "\\'")
			actions := []chromedp.Action{
				chromedp.Evaluate(`htmx.ajax('GET', '`+page.route+`', { target: '#main-content', source: document.body })`, nil),
				chromedp.Poll(`document.querySelector('#main-content .app-page') && document.querySelector('#main-content').textContent.includes('`+marker+`')`, nil,
					chromedp.WithPollingTimeout(5*time.Second)),
				chromedp.Evaluate(`(() => {
					const page = document.querySelector('#main-content .app-page').getBoundingClientRect();
					const header = document.querySelector('.app-header').getBoundingClientRect();
					const nav = document.querySelector('.app-bottom-nav').getBoundingClientRect();
					const smallTargets = [...document.querySelectorAll('.app-bottom-nav [data-nav-item]')]
						.filter((element) => { const rect = element.getBoundingClientRect(); return rect.width < 44 || rect.height < 44; }).length;
					return {
						viewportWidth: innerWidth,
						scrollWidth: document.documentElement.scrollWidth,
						pageLeft: page.left,
						pageRight: page.right,
						headerLeft: header.left,
						headerRight: header.right,
						navLeft: nav.left,
						navRight: nav.right,
						navItems: document.querySelectorAll('.app-bottom-nav [data-nav-item]').length,
						smallTargets
					};
				})()`, &layout),
			}
			if screenshotDir := os.Getenv("POKGET_MOBILE_SCREENSHOT_DIR"); screenshotDir != "" {
				if err := os.MkdirAll(screenshotDir, 0o750); err != nil {
					t.Fatalf("create screenshot directory: %v", err)
				}
				actions = append(actions, chromedp.CaptureScreenshot(&screenshot))
				defer func() {
					if err := os.WriteFile(filepath.Join(screenshotDir, "page-"+page.name+".png"), screenshot, 0o600); err != nil {
						t.Errorf("write %s screenshot: %v", page.name, err)
					}
				}()
			}
			if err := chromedp.Run(ctx, actions...); err != nil {
				t.Fatalf("inspect %s: %v", page.name, err)
			}
			if layout.ScrollWidth > layout.ViewportWidth+1 || layout.PageLeft < -1 || layout.PageRight > layout.ViewportWidth+1 {
				t.Errorf("page overflows viewport: %+v", layout)
			}
			if layout.HeaderLeft < -1 || layout.HeaderRight > layout.ViewportWidth+1 || layout.NavLeft < -1 || layout.NavRight > layout.ViewportWidth+1 {
				t.Errorf("shell overflows viewport: %+v", layout)
			}
			if layout.NavItems != 5 || layout.SmallTargets != 0 {
				t.Errorf("primary navigation is not mobile-safe: %+v", layout)
			}
		})
	}
}

func newMobileShellServer(t *testing.T) *httptest.Server {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}

	render := func(name string, data map[string]any) []byte {
		t.Helper()
		tmpl, parseErr := template.ParseFiles(filepath.Join(root, "templates", name))
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		var output bytes.Buffer
		if executeErr := tmpl.ExecuteTemplate(&output, name, data); executeErr != nil {
			t.Fatalf("render %s: %v", name, executeErr)
		}
		return output.Bytes()
	}

	fragments := map[string][]byte{
		"/dashboard": render("dashboard.html", map[string]any{
			"TotalValuation": 0.0, "CurrencySymbol": "€", "Change24h": 0.0,
			"RankIcon": "/static/img/logo.png", "Rank": "Novice collector", "XPPercent": 6,
			"Portfolio": []any{}, "BinderCount": 0, "UserCurrency": "EUR",
		}),
		"/wantlist": render("wantlist.html", map[string]any{"Items": []any{}, "CSRFToken": "test", "CurrencySymbol": "€", "UserCurrency": "EUR"}),
		"/binders":  render("binders.html", map[string]any{"Binders": []any{}, "CSRFToken": "test"}),
		"/errors":   render("error_database.html", map[string]any{"Errors": []any{}, "CanSubmit": true, "CSRFToken": "test"}),
		"/trade":    render("trade.html", map[string]any{"Portfolio": []any{}, "CurrencySymbol": "€", "UserCurrency": "EUR"}),
	}
	index := render("index.html", map[string]any{
		"BuildVersion": "test", "CSRFToken": "test", "InitialPath": "/dashboard", "InitialView": "home",
		"UserRank": "Novice collector", "UserXPPercent": 6, "Binders": []any{},
	})
	staticFiles := http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(root, "static"))))
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/" {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = writer.Write(index)
			return
		}
		if fragment, ok := fragments[request.URL.Path]; ok {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = writer.Write(fragment)
			return
		}
		staticFiles.ServeHTTP(writer, request)
	}))
}
