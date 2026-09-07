package main

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"pokget/internal/models"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/shopspring/decimal"
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
		paths := []string{filepath.Join(root, "templates", name)}
		if name == "centering_tool.html" {
			paths = append(paths, filepath.Join(root, "templates", "card_measure.html"))
		}
		if name == "auth.html" {
			paths = append(paths, filepath.Join(root, "templates", "auth_fragment.html"))
			paths = append(paths, filepath.Join(root, "templates", "auth_pack.html"))
		}
		if name == "error_database_page.html" {
			paths = append(paths, filepath.Join(root, "templates", "error_database.html"))
		}
		tmpl, parseErr := template.ParseFiles(paths...)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		var output bytes.Buffer
		templateName := name
		if name == "settings.html" {
			templateName = "settings"
		}
		if executeErr := tmpl.ExecuteTemplate(&output, templateName, data); executeErr != nil {
			t.Fatalf("render %s: %v", name, executeErr)
		}
		return output.Bytes()
	}

	type previewPage struct {
		name string
		data map[string]any
	}
	fragments := map[string]previewPage{
		"/dashboard": {"dashboard.html", map[string]any{
			"TotalValuation": 0.0, "CurrencySymbol": "€", "Change24h": 0.0,
			"RankIcon": "/static/img/logo.png", "Rank": "Novice collector", "XPPercent": 6,
			"Portfolio": []any{}, "BinderCount": 0, "UserCurrency": "EUR",
		}},
		"/wantlist":     {"wantlist.html", map[string]any{"Items": []any{}, "CSRFToken": "test", "CurrencySymbol": "€", "UserCurrency": "EUR"}},
		"/binders":      {"binders.html", map[string]any{"Binders": []any{}, "CSRFToken": "test"}},
		"/errors":       {"error_database.html", map[string]any{"Errors": []any{}, "CanSubmit": true, "CSRFToken": "test"}},
		"/trade":        {"trade.html", map[string]any{"Portfolio": []any{}, "CurrencySymbol": "€", "UserCurrency": "EUR"}},
		"/settings":     {"settings.html", map[string]any{"Currency": "EUR", "Email": "collector@example.com", "CSRFToken": "test"}},
		"/auth":         {"auth.html", map[string]any{"CSRFToken": "test"}},
		"/binders/test": {"binder_detail.html", map[string]any{"Binder": map[string]any{"ID": "test", "Name": "Base set", "Description": "My first collection"}, "Cards": []any{}, "CurrencySymbol": "€", "UserCurrency": "EUR"}},
		"/vault/test":   {"public_vault.html", map[string]any{"Username": "Alex", "Portfolio": []any{}, "CurrencySymbol": "€", "UserCurrency": "EUR"}},
		"/centering":    {"centering_tool.html", map[string]any{"CSRFToken": "test", "CurrencySymbol": "€"}},
	}
	card := models.Card{ID: "test-card", Name: "Pikachu — collector's edition", Set: "Scarlet & Violet", Game: "Pokemon", Language: "en", ImageURL: "/static/img/logo.png", PriceEUR: decimal.RequireFromString("24.95"), PriceUSD: decimal.RequireFromString("27.50"), PriceEURValid: true, PriceUSDValid: true}
	portfolio := []models.PortfolioItem{{ID: "test-item", BinderID: "test", Card: card, Condition: "NM", IsPublic: true}}
	populated := map[string]map[string]any{
		"/dashboard":    {"TotalValuation": 24.95, "CurrencySymbol": "€", "Change24h": 0.0, "RankIcon": "/static/img/ranks/novice.png", "Rank": "Novice collector", "XPPercent": 6, "Portfolio": portfolio, "BinderCount": 1, "UserCurrency": "EUR"},
		"/wantlist":     {"Items": []map[string]any{{"ID": "test-grail", "Card": card, "TargetPrice": 20.0, "PriceEUR": 24.95, "PriceUSD": 27.50, "ProgressEUR": 124.75, "ProgressUSD": 137.5}}, "CSRFToken": "test", "CurrencySymbol": "€", "UserCurrency": "EUR"},
		"/binders":      {"Binders": []map[string]any{{"ID": "test", "Name": "Base set", "Description": "My first collection", "CardCount": 1}}, "CSRFToken": "test"},
		"/binders/test": {"Binder": map[string]any{"ID": "test", "Name": "Base set", "Description": "My first collection"}, "Cards": portfolio, "CurrencySymbol": "€", "UserCurrency": "EUR"},
		"/vault/test":   {"Username": "Alex", "Portfolio": portfolio, "CurrencySymbol": "€", "UserCurrency": "EUR"},
		"/trade":        {"Portfolio": portfolio, "CurrencySymbol": "€", "UserCurrency": "EUR"},
		"/errors":       {"Errors": []map[string]any{{"CardName": "Pikachu", "SetName": "Scarlet & Violet", "ErrorType": "Miscut", "Description": "The printed border is visibly shifted.", "EstimatedValueMultiplier": 1.2, "ImageURL": card.ImageURL}}, "CanSubmit": true, "CSRFToken": "test"},
	}
	indexData := map[string]any{
		"BuildVersion": "test", "CSRFToken": "test", "InitialPath": "/dashboard", "InitialView": "home",
		"UserRank": "Novice collector", "UserXPPercent": 6, "Binders": []any{},
	}
	staticFiles := http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(root, "static"))))
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Has("collection") {
			http.SetCookie(writer, &http.Cookie{Name: "ui-fixture-collection", Value: request.URL.Query().Get("collection"), Path: "/", SameSite: http.SameSiteStrictMode})
		}
		if request.URL.Path == "/portfolio/editor-metadata" {
			state, _ := request.Cookie("ui-fixture-editor")
			if state != nil && state.Value == "fail" {
				http.Error(writer, "Synthetic editor metadata outage", http.StatusServiceUnavailable)
				return
			}
			data := map[string]any{"currency": "EUR", "currency_symbol": "€", "binders": []map[string]string{{"id": "test", "name": "Base set"}}}
			if state != nil && state.Value == "updated" {
				data = map[string]any{"currency": "USD", "currency_symbol": "$", "binders": []map[string]string{{"id": "test", "name": "Base set"}, {"id": "new", "name": "New collector binder"}}}
			}
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(data)
			return
		}
		if request.URL.Path == "/centering" {
			if state, _ := request.Cookie("ui-fixture-navigation-fail"); state != nil && state.Value == "1" {
				http.Error(writer, "Synthetic scanner outage", http.StatusInternalServerError)
				return
			}
		}
		if (request.URL.Path == "/errors/submit" || request.URL.Path == "/portfolio/edit") && request.Method == http.MethodPost {
			writer.WriteHeader(http.StatusOK)
			return
		}
		if request.URL.Path == "/dashboard" {
			if state, _ := request.Cookie("ui-fixture-delay-dashboard"); state != nil && state.Value == "1" {
				select {
				case <-time.After(600 * time.Millisecond):
				case <-request.Context().Done():
					return
				}
			}
		}
		if request.URL.Path == "/" {
			if request.URL.Query().Has("filled") {
				http.SetCookie(writer, &http.Cookie{Name: "ui-fixture-filled", Value: request.URL.Query().Get("filled"), Path: "/", SameSite: http.SameSiteStrictMode})
			}
			if request.URL.Query().Has("catalog") {
				http.SetCookie(writer, &http.Cookie{Name: "ui-fixture-catalog", Value: request.URL.Query().Get("catalog"), Path: "/", SameSite: http.SameSiteStrictMode})
			}
			data := make(map[string]any, len(indexData))
			for key, value := range indexData {
				data[key] = value
			}
			view := request.URL.Query().Get("view")
			if route, ok := map[string]string{"home": "/dashboard", "wantlist": "/wantlist", "binders": "/binders", "errors": "/errors", "trade": "/trade", "settings": "/settings", "scan": "/centering"}[view]; ok {
				data["InitialView"], data["InitialPath"] = view, route
				if view == "binders" && request.URL.Query().Get("binder") == "test" {
					data["InitialPath"] = "/binders/test"
				}
			}
			if pageNumber := request.URL.Query().Get("page"); pageNumber != "" {
				data["InitialPath"] = data["InitialPath"].(string) + "?page=" + pageNumber
			}
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = writer.Write(render("index.html", data))
			return
		}
		if page, ok := fragments[request.URL.Path]; ok {
			if request.URL.Path == "/errors" && request.URL.Query().Get("q") == "fail-request" {
				http.Error(writer, "Synthetic catalog outage", http.StatusInternalServerError)
				return
			}
			if cookie, err := request.Cookie("ui-fixture-filled"); err == nil && cookie.Value == "1" {
				if data, exists := populated[request.URL.Path]; exists {
					page.data = data
				}
			}
			if request.URL.Query().Get("hardening") == "1" {
				switch request.URL.Path {
				case "/binders":
					page.data = map[string]any{"Binders": []map[string]any{
						{"ID": "z", "Name": "Zeta", "CardCount": 2},
						{"ID": "a", "Name": "Alpha", "CardCount": 1},
						{"ID": "m", "Name": "Moon", "CardCount": 3},
					}, "CSRFToken": "test"}
				case "/errors":
					page.data = map[string]any{"Errors": []map[string]any{{
						"CardName": "Pikachu", "SetName": "Base Set", "ErrorType": "Miscut",
						"Description":              strings.Repeat("The printing is shifted beyond the border. ", 14) + "Final diagnostic detail.",
						"EstimatedValueMultiplier": 1.2, "ImageURL": card.ImageURL,
					}}, "CanSubmit": true, "CSRFToken": "test"}
				case "/trade":
					unpriced := card
					unpriced.PriceEUR, unpriced.PriceUSD = decimal.Zero, decimal.Zero
					unpriced.PriceEURValid, unpriced.PriceUSDValid = false, false
					page.data = map[string]any{"Portfolio": []models.PortfolioItem{{ID: "unpriced-item", Card: unpriced, Condition: "NM"}}, "CurrencySymbol": "€", "UserCurrency": "EUR"}
				}
			}
			if request.URL.Path == "/errors" {
				page.data = misprintFixturePage(request, page.data)
				if request.Header.Get("HX-Request") != "true" {
					page.name = "error_database_page.html"
				}
			}
			page.data = pricingFixturePage(request, page.data, card)
			page.data = collectionFixturePage(request, page.data, card)
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = writer.Write(render(page.name, page.data))
			return
		}
		if request.URL.Path == "/sw.js" {
			writer.Header().Set("Content-Type", "application/javascript")
			_, _ = writer.Write([]byte("// No service worker caching in the UI fixture."))
			return
		}
		staticFiles.ServeHTTP(writer, request)
	}))
}
