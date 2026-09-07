package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestUncertainPrintingRequiresExplicitConfirmation(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed")
	}
	server := newScannerProgressServer(t)
	defer server.Close()
	browser := newHeadlessBrowserContext(t, chromePath)
	for _, viewport := range []struct{ width, height int64 }{{320, 568}, {390, 844}, {1440, 1000}} {
		t.Run(fmt.Sprintf("width-%d", viewport.width), func(t *testing.T) {
			ctx, cancel := chromedp.NewContext(browser)
			defer cancel()
			ctx, timeout := context.WithTimeout(ctx, 30*time.Second)
			defer timeout()
			if err := chromedp.Run(ctx,
				chromedp.EmulateViewport(viewport.width, viewport.height),
				chromedp.Navigate(server.URL),
				chromedp.Poll(`document.readyState === 'complete' && window.Alpine && document.querySelector('#scanner-root .scanner-shell') && Alpine.$data(document.querySelector('#scanner-root .scanner-shell')).$refs.scanResult`, nil),
				chromedp.ActionFunc(func(context.Context) error { t.Log("scanner ready"); return nil }),
				chromedp.Evaluate(`Alpine.$data(document.querySelector('#scanner-root .scanner-shell')).applyScanResult({
					detected: 'Pikachu', id: 'base1-58-en', price: 12.5, image_url: '/static/img/logo.png', set: 'Base Set', collector_number: '58/102', language: 'en', needs_review: true,
					top_matches: [
						{id: 'base1-58-en', name: 'Pikachu', price: 12.5, image_url: '/static/img/logo.png', set: 'Base Set', collector_number: '58/102', language: 'en'},
						{id: 'base1-58-de', name: 'Pikachu', price: 14.75, image_url: '/static/img/logo.png', set: 'Basis', collector_number: '58/102', language: 'de'}
					]
				})`, nil),
				chromedp.WaitVisible(".scan-result"),
				chromedp.Poll(`!document.querySelector('.scan-result').innerText.includes('base1-58')`, nil, chromedp.WithPollingTimeout(time.Second)),
				chromedp.Poll(`document.activeElement === document.querySelector('.scan-result')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
				chromedp.ActionFunc(func(context.Context) error { t.Log("result focused"); return nil }),
				chromedp.Poll(`document.querySelectorAll('.scan-candidate').length === 2 && document.querySelectorAll('.scan-candidate')[1].textContent.includes('Basis') && document.querySelectorAll('.scan-candidate')[1].textContent.includes('#58/102') && document.querySelectorAll('.scan-candidate')[1].textContent.includes('DE')`, nil),
				chromedp.Click(".scan-candidate:nth-of-type(2)", chromedp.ByQuery),
				chromedp.ActionFunc(func(context.Context) error { t.Log("candidate clicked"); return nil }),
				chromedp.Poll(`document.querySelector('[data-testid="detected-card-id"]').dataset.cardId === 'base1-58-de' && document.querySelectorAll('.scan-candidate')[1].getAttribute('aria-pressed') === 'true' && !Alpine.$data(document.querySelector('#scanner-root .scanner-shell')).matchConfirmed`, nil),
				chromedp.Poll(`!document.querySelector('button[\\@click="addToCollection()"]' ).getClientRects().length`, nil),
			); err != nil {
				var diagnostic string
				_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({active: document.activeElement?.outerHTML?.slice(0,180), visibility: document.visibilityState, focused: document.hasFocus(), result: document.querySelector('.scan-result')?.getBoundingClientRect().toJSON(), display: getComputedStyle(document.querySelector('.scan-result')).display, refMatches: Alpine.$data(document.querySelector('#scanner-root .scanner-shell')).$refs.scanResult === document.querySelector('.scan-result')})`, &diagnostic))
				t.Log(diagnostic)
				t.Fatalf("review uncertain fixture: %v", err)
			}
			if dir := os.Getenv("POKGET_UI_SCREENSHOT_DIR"); dir != "" {
				var shot []byte
				if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.scan-result').scrollIntoView({block: 'start'})`, nil), chromedp.CaptureScreenshot(&shot)); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("scanner-review-%d.png", viewport.width)), shot, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := chromedp.Run(ctx,
				chromedp.Click(`[data-testid="confirm-printing"]`, chromedp.ByQuery),
				chromedp.WaitVisible(`button[\@click="addToCollection()"]`, chromedp.ByQuery),
				chromedp.Poll(`!document.querySelector('button[\\@click="addToCollection()"]' ).disabled`, nil),
				chromedp.Click(`button[\@click="reviewMatches()"]`, chromedp.ByQuery),
				chromedp.WaitVisible(`[data-testid="confirm-printing"]`, chromedp.ByQuery),
				chromedp.Poll(`!document.querySelector('button[\\@click="addToCollection()"]' ).getClientRects().length`, nil),
			); err != nil {
				t.Fatalf("confirm and reopen printing review: %v", err)
			}
		})
	}
}
