package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"image/png"
	"os"
	"path/filepath"
	"pokget/internal/models"
	"regexp"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func TestApplicationTemplatesExecute(t *testing.T) {
	templates := parseApplicationTemplates(t)

	for _, name := range []string{"auth.html", "centering_tool.html", "error_database_page.html", "index.html"} {
		t.Run(name, func(t *testing.T) {
			executeApplicationTemplate(t, templates, name)
		})
	}
}

func TestWantlistTemplateSupportsDecimalMarketPrice(t *testing.T) {
	t.Parallel()

	templates := parseApplicationTemplates(t)
	data := map[string]interface{}{
		"CSRFToken":      "test-csrf-token",
		"CurrencySymbol": "€",
		"UserCurrency":   "EUR",
		"Items": []wantlistViewItem{{
			WantlistItem: models.WantlistItem{
				ID:          "want-1",
				CardID:      "card-1",
				TargetPrice: 10,
				Card: models.Card{
					ID:       "card-1",
					Name:     "Test Card",
					PriceEUR: decimal.NewFromInt(15),
					PriceUSD: decimal.NewFromInt(16),
				},
			},
			PriceEUR:    15,
			PriceUSD:    16,
			ProgressEUR: 150,
			ProgressUSD: 160,
		}},
	}

	var output bytes.Buffer
	if err := templates.ExecuteTemplate(&output, "wantlist.html", data); err != nil {
		t.Fatalf("execute wantlist.html with decimal price: %v", err)
	}
	if !regexp.MustCompile(`150%`).MatchString(output.String()) {
		t.Fatalf("wantlist progress does not contain 150%%: %q", output.String())
	}
}

func TestErrorDatabaseTemplateRendersErrorCardFields(t *testing.T) {
	t.Parallel()

	templates := parseApplicationTemplates(t)
	data := map[string]interface{}{
		"Errors": []ErrorCard{{
			ID:                       "error-1",
			CardID:                   "card-1",
			ErrorType:                "Miscut",
			Description:              "Shifted border",
			EstimatedValueMultiplier: 1.5,
			CardName:                 "Test Card",
			SetName:                  "Test Set",
			ImageURL:                 "/static/img/logo.png",
		}},
	}

	var output bytes.Buffer
	if err := templates.ExecuteTemplate(&output, "error_database.html", data); err != nil {
		t.Fatalf("execute error_database.html: %v", err)
	}
	for _, expected := range []string{"Test Card", "Test Set", "Shifted border"} {
		if !regexp.MustCompile(regexp.QuoteMeta(expected)).MatchString(output.String()) {
			t.Errorf("error database output does not contain %q", expected)
		}
	}
}

func TestCenteringToolRendersDetectedCardID(t *testing.T) {
	t.Parallel()

	templates := parseApplicationTemplates(t)
	output := executeApplicationTemplate(t, templates, "centering_tool.html")
	idBinding := regexp.MustCompile(
		`<span[^>]*data-testid="detected-card-id"[^>]*x-text="detectedID"[^>]*>`,
	)
	if !idBinding.MatchString(output) {
		t.Error("centering_tool.html does not render detectedID in the detected-card-id element")
	}
}

func TestCenteringToolRendersAccessibleFullScreenProgress(t *testing.T) {
	t.Parallel()

	templates := parseApplicationTemplates(t)
	output := executeApplicationTemplate(t, templates, "centering_tool.html")
	for _, expected := range []string{
		`data-testid="scan-progress-overlay"`,
		`role="dialog"`,
		`aria-modal="true"`,
		`data-testid="scan-progress-elapsed"`,
		`data-testid="scan-progress-cancel"`,
		`:inert="scanning"`,
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("centering_tool.html does not contain %q", expected)
		}
	}
}

func TestPortfolioWorkflowTemplatesExposeSafeActions(t *testing.T) {
	t.Parallel()

	templates := parseApplicationTemplates(t)
	indexData := map[string]interface{}{
		"CSRFToken":    "test-csrf-token",
		"BuildVersion": "test",
		"InitialView":  "home",
		"InitialPath":  "/dashboard",
		"Binders": []Binder{{
			ID:   "binder-1",
			Name: "Master Set",
		}},
	}
	var indexOutput bytes.Buffer
	if err := templates.ExecuteTemplate(&indexOutput, "index.html", indexData); err != nil {
		t.Fatalf("execute index.html: %v", err)
	}
	for _, expected := range []string{
		`hx-post="/auth/logout"`,
		`hx-post="/portfolio/delete"`,
		`hx-post="/portfolio/binder"`,
		`role="dialog"`,
		`value="binder-1"`,
	} {
		if !strings.Contains(indexOutput.String(), expected) {
			t.Errorf("index output does not contain %q", expected)
		}
	}
	if strings.Contains(indexOutput.String(), `hx-get="/auth/logout"`) {
		t.Error("index still exposes state-changing GET logout")
	}

	dashboardData := map[string]interface{}{
		"TotalValuation": float64(0),
		"Change24h":      float64(0),
		"XPPercent":      0,
		"BinderCount":    1,
		"CurrencySymbol": "€",
		"UserCurrency":   "EUR",
		"Portfolio": []models.PortfolioItem{{
			ID:       "item-1",
			BinderID: "binder-1",
			Card: models.Card{
				Name: "Test Card",
			},
		}},
	}
	var dashboardOutput bytes.Buffer
	if err := templates.ExecuteTemplate(&dashboardOutput, "dashboard.html", dashboardData); err != nil {
		t.Fatalf("execute dashboard.html: %v", err)
	}
	for _, expected := range []string{`data-portfolio-item`, `data-id="item-1"`, `data-binder-id="binder-1"`} {
		if !strings.Contains(dashboardOutput.String(), expected) {
			t.Errorf("dashboard output does not contain %q", expected)
		}
	}
	if strings.Contains(dashboardOutput.String(), "&lt;no value&gt;") {
		t.Errorf("dashboard renders a missing template field: %q", dashboardOutput.String())
	}
}

func TestMutationTemplatesRetainErrorsAndTradeIdentity(t *testing.T) {
	t.Parallel()

	templates := parseApplicationTemplates(t)
	checks := []struct {
		name     string
		data     map[string]interface{}
		contains []string
	}{
		{
			name: "binders.html",
			data: map[string]interface{}{"CSRFToken": "test"},
			contains: []string{
				`createError = $event.detail.xhr.responseText.trim()`,
				`role="dialog"`,
			},
		},
		{
			name: "wantlist.html",
			data: map[string]interface{}{"CSRFToken": "test"},
			contains: []string{
				`addError = $event.detail.xhr.responseText.trim()`,
				`aria-live="assertive"`,
			},
		},
		{
			name: "error_database.html",
			data: map[string]interface{}{"CSRFToken": "test", "CanSubmit": true},
			contains: []string{
				`submitError = $event.detail.xhr.responseText.trim()`,
				`htmx.ajax('GET', '/errors'`,
			},
		},
		{
			name: "trade.html",
			data: map[string]interface{}{
				"CurrencySymbol": "€",
				"UserCurrency":   "EUR",
				"Portfolio": []models.PortfolioItem{{
					ID:   "portfolio-1",
					Card: models.Card{Name: "Identity Card", Set: "Test Set"},
				}},
			},
			contains: []string{
				`Portfolio item: `,
				`value="portfolio-1"`,
				`data-label="Identity Card — Test Set`,
			},
		},
	}

	for _, check := range checks {
		check := check
		t.Run(check.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := templates.ExecuteTemplate(&output, check.name, check.data); err != nil {
				t.Fatalf("execute %s: %v", check.name, err)
			}
			for _, expected := range check.contains {
				if !strings.Contains(output.String(), expected) {
					t.Errorf("output does not contain %q", expected)
				}
			}
		})
	}
}

func TestVaultJavaScriptUsesCanonicalRoutesAndScopedSwipe(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join("..", "..", "static", "js", "vault.js"))
	if err != nil {
		t.Fatalf("read vault.js: %v", err)
	}
	script := string(contents)
	for _, expected := range []string{
		"currentFragmentPath()",
		"initSwipeToDelete(event.detail.target || document)",
		"[data-portfolio-item]:not([data-swipe-ready])",
		"method: 'POST'",
		"pokget-view-change",
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("vault.js does not contain %q", expected)
		}
	}
	if strings.Contains(script, "htmx.ajax('GET', window.location.pathname") {
		t.Error("pull-to-refresh still loads the shell pathname into the fragment target")
	}
}

func TestManifestIconMetadataMatchesAsset(t *testing.T) {
	t.Parallel()

	staticRoot := filepath.Join("..", "..", "static")
	contents, err := os.ReadFile(filepath.Join(staticRoot, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest.json: %v", err)
	}

	var manifest struct {
		Icons []struct {
			Src     string `json:"src"`
			Sizes   string `json:"sizes"`
			Type    string `json:"type"`
			Purpose string `json:"purpose"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(contents, &manifest); err != nil {
		t.Fatalf("decode manifest.json: %v", err)
	}

	expected := map[string]int{
		"/static/img/icon-192.png": 192,
		"/static/img/icon-512.png": 512,
	}
	if len(manifest.Icons) != len(expected) {
		t.Fatalf("manifest icon count = %d, want %d", len(manifest.Icons), len(expected))
	}

	worker, err := os.ReadFile(filepath.Join(staticRoot, "js", "sw.js"))
	if err != nil {
		t.Fatalf("read service worker: %v", err)
	}
	for _, icon := range manifest.Icons {
		icon := icon
		t.Run(icon.Sizes, func(t *testing.T) {
			size, ok := expected[icon.Src]
			if !ok {
				t.Fatalf("unexpected manifest icon source %q", icon.Src)
			}
			wantSizes := fmt.Sprintf("%dx%d", size, size)
			if icon.Sizes != wantSizes {
				t.Errorf("declared sizes = %q, want %q", icon.Sizes, wantSizes)
			}
			if icon.Type != "image/png" || icon.Purpose != "any" {
				t.Errorf("icon metadata = type %q purpose %q, want image/png and any", icon.Type, icon.Purpose)
			}

			assetPath := filepath.Join("..", "..", filepath.FromSlash(strings.TrimPrefix(icon.Src, "/")))
			asset, err := os.Open(assetPath)
			if err != nil {
				t.Fatalf("open icon asset: %v", err)
			}
			defer asset.Close()
			config, err := png.DecodeConfig(asset)
			if err != nil {
				t.Fatalf("decode PNG config: %v", err)
			}
			if config.Width != size || config.Height != size {
				t.Errorf("decoded dimensions = %dx%d, want %dx%d", config.Width, config.Height, size, size)
			}
			if !bytes.Contains(worker, []byte(icon.Src)) {
				t.Errorf("service worker does not precache %q", icon.Src)
			}
		})
	}
}

// TestTemplatesUseMobileFirstResponsiveMarkup asserts every page ships
// mobile-first markup: a correct viewport meta on full documents, no fixed
// desktop-only pixel widths, and ~44px minimum touch targets on buttons
// (44px enforced either by Tailwind classes or by styles.css component rules).
func TestTemplatesUseMobileFirstResponsiveMarkup(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob(filepath.Join("..", "..", "templates", "*.html"))
	if err != nil {
		t.Fatalf("glob templates: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no templates found")
	}

	viewportNameRe := regexp.MustCompile(`<meta\b[^>]*name="viewport"[^>]*>`)
	fixedWidthRe := regexp.MustCompile(`w-\[\d{3,}px\]`)
	constrainedWidthRe := regexp.MustCompile(`(?:min|max)-w-\[\d+px\]`)
	buttonRe := regexp.MustCompile(`<button\b(?:[^>"']|"[^"]*"|'[^']*')*>`)
	// 44px targets: explicit min sizing, btn components (min-h-11), the 44px
	// scanner guides, h-11/w-11 squares, p-4..p-6 padded card surfaces, or the
	// styles.css-enforced scan-progress-cancel (min-height: 3rem).
	touchTargetRe := regexp.MustCompile(`min-h-|min-w-|btn|\[44px\]|\bh-11\b|\bw-11\b|\bp-[456]\b|scan-progress-cancel`)

	for _, file := range files {
		file := file
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()

			contents, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read template: %v", err)
			}
			source := string(contents)

			if strings.Contains(source, "<!DOCTYPE") || strings.Contains(source, "<!doctype") {
				viewport := viewportNameRe.FindString(source)
				if viewport == "" {
					t.Error("full document is missing a viewport meta tag")
				} else {
					if !strings.Contains(viewport, "width=device-width") {
						t.Errorf("viewport meta lacks width=device-width: %s", viewport)
					}
					if !strings.Contains(viewport, "viewport-fit=cover") {
						t.Errorf("viewport meta lacks viewport-fit=cover: %s", viewport)
					}
				}
			}

			if match := fixedWidthRe.FindString(constrainedWidthRe.ReplaceAllString(source, "")); match != "" {
				t.Errorf("template uses fixed desktop-only width %q", match)
			}

			for _, button := range buttonRe.FindAllString(source, -1) {
				if !touchTargetRe.MatchString(button) {
					t.Errorf("button without a ~44px touch target: %s", button)
				}
			}
		})
	}
}

func parseApplicationTemplates(t *testing.T) *template.Template {
	t.Helper()

	templates, err := template.New("").Funcs(template.FuncMap{
		"div": func(a, b float64) float64 {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"mul": func(a, b float64) float64 {
			return a * b
		},
	}).ParseGlob(filepath.Join("..", "..", "templates", "*.html"))
	if err != nil {
		t.Fatalf("parse application templates: %v", err)
	}
	return templates
}

func executeApplicationTemplate(t *testing.T, templates *template.Template, name string) string {
	t.Helper()

	data := map[string]interface{}{
		"CSRFToken":    "test-csrf-token",
		"BuildVersion": "test",
	}
	var output bytes.Buffer
	if err := templates.ExecuteTemplate(&output, name, data); err != nil {
		t.Fatalf("execute %s: %v", name, err)
	}
	return output.String()
}
