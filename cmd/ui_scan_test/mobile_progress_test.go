package main

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

type progressLayout struct {
	ViewportWidth  float64 `json:"viewportWidth"`
	ViewportHeight float64 `json:"viewportHeight"`
	Left           float64 `json:"left"`
	Top            float64 `json:"top"`
	Width          float64 `json:"width"`
	Height         float64 `json:"height"`
	Position       string  `json:"position"`
	ZIndex         string  `json:"zIndex"`
	CancelHeight   float64 `json:"cancelHeight"`
	CardLeft       float64 `json:"cardLeft"`
	CardRight      float64 `json:"cardRight"`
	CardTop        float64 `json:"cardTop"`
	CardBottom     float64 `json:"cardBottom"`
	Elapsed        string  `json:"elapsed"`
	Detail         string  `json:"detail"`
	FocusedCancel  bool    `json:"focusedCancel"`
	InertRegions   int     `json:"inertRegions"`
	ScrollLocked   bool    `json:"scrollLocked"`
}

func TestMobileScanProgressOccupiesViewportAndCancels(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed; skipping rendered mobile progress check")
	}

	server := newScannerProgressServer(t)
	defer server.Close()

	for _, viewport := range []struct {
		name          string
		width, height int64
	}{
		{name: "small phone", width: 320, height: 568},
		{name: "compact phone", width: 360, height: 800},
		{name: "modern phone", width: 390, height: 844},
		{name: "phone landscape", width: 844, height: 390},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			screenshotPath := ""
			if screenshotDir := os.Getenv("POKGET_MOBILE_SCREENSHOT_DIR"); screenshotDir != "" {
				if err := os.MkdirAll(screenshotDir, 0o750); err != nil {
					t.Fatalf("create screenshot directory: %v", err)
				}
				screenshotPath = filepath.Join(screenshotDir, strings.ReplaceAll(viewport.name, " ", "-")+".png")
			}
			layout := inspectProgressAtViewport(
				t, chromePath, server.URL, viewport.width, viewport.height, screenshotPath,
			)
			if layout.Position != "fixed" || layout.Left != 0 || layout.Top != 0 {
				t.Errorf("overlay position = %s at %.1f,%.1f", layout.Position, layout.Left, layout.Top)
			}
			if difference(layout.Width, layout.ViewportWidth) > 1 || difference(layout.Height, layout.ViewportHeight) > 1 {
				t.Errorf("overlay %.1fx%.1f does not fill viewport %.1fx%.1f",
					layout.Width, layout.Height, layout.ViewportWidth, layout.ViewportHeight)
			}
			if layout.CancelHeight < 44 {
				t.Errorf("cancel target height = %.1f, want at least 44", layout.CancelHeight)
			}
			if layout.CardLeft < 0 || layout.CardRight > layout.ViewportWidth ||
				layout.CardTop < 0 || layout.CardBottom > layout.ViewportHeight {
				t.Errorf("progress card overflows viewport: %+v", layout)
			}
			if layout.Elapsed != "0:16 elapsed" {
				t.Errorf("elapsed label = %q", layout.Elapsed)
			}
			if layout.Detail == "" || layout.InertRegions < 3 || !layout.ScrollLocked {
				t.Errorf("progress safeguards = %+v", layout)
			}
			if !layout.FocusedCancel {
				t.Error("cancel control did not receive focus when progress opened")
			}
		})
	}
}

func inspectProgressAtViewport(
	t *testing.T,
	chromePath, pageURL string,
	width, height int64,
	screenshotPath string,
) progressLayout {
	t.Helper()

	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options,
		chromedp.ExecPath(chromePath),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-sandbox", true),
	)
	allocatorContext, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	defer cancelAllocator()
	browserContext, cancelBrowser := chromedp.NewContext(allocatorContext)
	defer cancelBrowser()
	ctx, cancel := context.WithTimeout(browserContext, 30*time.Second)
	defer cancel()

	var layout progressLayout
	var screenshot []byte
	actions := []chromedp.Action{
		chromedp.EmulateViewport(width, height),
		chromedp.Navigate(pageURL),
		chromedp.Poll(`document.querySelector('#scanner-root > [x-data]') && window.Alpine && Alpine.$data(document.querySelector('#scanner-root > [x-data]')).setScanning`, nil,
			chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`(() => {
			const state = Alpine.$data(document.querySelector('#scanner-root > [x-data]'));
			state.setStatus('Uploading the crop and running detection…', 2);
			state.setScanning(true);
			state.scanElapsedSeconds = 16;
		})()`, nil),
		chromedp.WaitVisible(`[data-testid="scan-progress-overlay"]`, chromedp.ByQuery),
		chromedp.Poll(`document.activeElement === document.querySelector('[data-testid="scan-progress-cancel"]')`, nil,
			chromedp.WithPollingTimeout(3*time.Second)),
		chromedp.Evaluate(`(() => {
			const overlay = document.querySelector('[data-testid="scan-progress-overlay"]');
			const cancel = document.querySelector('[data-testid="scan-progress-cancel"]');
			const card = document.querySelector('.scan-progress-card');
			const bounds = overlay.getBoundingClientRect();
			const cancelBounds = cancel.getBoundingClientRect();
			const cardBounds = card.getBoundingClientRect();
			const style = getComputedStyle(overlay);
			return {
				viewportWidth: innerWidth,
				viewportHeight: innerHeight,
				left: bounds.left,
				top: bounds.top,
				width: bounds.width,
				height: bounds.height,
				position: style.position,
				zIndex: style.zIndex,
				cancelHeight: cancelBounds.height,
				cardLeft: cardBounds.left,
				cardRight: cardBounds.right,
				cardTop: cardBounds.top,
				cardBottom: cardBounds.bottom,
				elapsed: document.querySelector('[data-testid="scan-progress-elapsed"]').textContent.trim(),
				detail: document.querySelector('#scan-progress-detail').textContent.trim(),
				focusedCancel: document.activeElement === cancel,
				inertRegions: document.querySelectorAll('#scanner-root [inert]').length,
				scrollLocked: document.documentElement.classList.contains('scan-progress-open') &&
					getComputedStyle(document.documentElement).overflow === 'hidden'
			};
		})()`, &layout),
	}
	if screenshotPath != "" {
		actions = append(actions, chromedp.CaptureScreenshot(&screenshot))
	}
	actions = append(actions,
		chromedp.Click(`[data-testid="scan-progress-cancel"]`, chromedp.ByQuery),
		chromedp.Poll(`(() => {
			const root = document.querySelector('#scanner-root > [x-data]');
			return !Alpine.$data(root).scanning && !document.documentElement.classList.contains('scan-progress-open');
		})()`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	err := chromedp.Run(ctx, actions...)
	if err != nil {
		t.Fatalf("inspect rendered mobile progress: %v", err)
	}
	if screenshotPath != "" {
		if err := os.WriteFile(screenshotPath, screenshot, 0o600); err != nil {
			t.Fatalf("write mobile progress screenshot: %v", err)
		}
	}
	return layout
}

func newScannerProgressServer(t *testing.T) *httptest.Server {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	templates, err := template.ParseFiles(filepath.Join(root, "templates", "centering_tool.html"))
	if err != nil {
		t.Fatalf("parse scanner template: %v", err)
	}
	var scanner bytes.Buffer
	if err := templates.ExecuteTemplate(&scanner, "centering_tool.html", map[string]any{
		"CSRFToken": "test-token", "CurrencySymbol": "€",
	}); err != nil {
		t.Fatalf("render scanner template: %v", err)
	}

	page := fmt.Sprintf(`<!doctype html><html class="dark" lang="en"><head>
		<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
		<link rel="stylesheet" href="/static/css/tailwind.css"><link rel="stylesheet" href="/static/css/styles.css">
		<script src="/static/js/scanner.js" defer></script><script src="/static/js/alpine.min.js" defer></script>
		</head><body><main id="scanner-root">%s</main></body></html>`, scanner.String())
	staticFiles := http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(root, "static"))))
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/" {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = writer.Write([]byte(page))
			return
		}
		staticFiles.ServeHTTP(writer, request)
	}))
}

func mobileTestChromePath() string {
	if configured := os.Getenv("POKGET_CHROME_PATH"); configured != "" {
		if _, err := os.Stat(configured); err == nil {
			return configured
		}
	}
	if runtime.GOOS != "windows" {
		for _, candidate := range []string{"google-chrome", "chromium", "chromium-browser"} {
			if path, err := exec.LookPath(candidate); err == nil {
				return path
			}
		}
		return ""
	}
	for _, candidate := range []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func difference(left, right float64) float64 {
	if left < right {
		return right - left
	}
	return left - right
}
