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
			if err := chromedp.Do(ctx,
				chromedp.EmulateViewport(viewport.width, viewport.height),
				chromedp.Navigate(server.URL),
				poll(`document.readyState === 'complete' && window.Alpine && document.querySelector('#scanner-root .scanner-shell') && Alpine.$data(document.querySelector('#scanner-root .scanner-shell')).$refs.scanResult`),
				actionFunc(func(context.Context) error { t.Log("scanner ready"); return nil }),
				evaluate(`Alpine.$data(document.querySelector('#scanner-root .scanner-shell')).applyScanResult({
					detected: 'Pikachu', id: 'base1-58-en', price: 12.5, image_url: '/static/img/logo.png', set: 'Base Set', collector_number: '58/102', language: 'en', needs_review: true,
					top_matches: [
						{id: 'base1-58-en', name: 'Pikachu', price: 12.5, image_url: '/static/img/logo.png', set: 'Base Set', collector_number: '58/102', language: 'en'},
						{id: 'base1-58-de', name: 'Pikachu', price: 14.75, image_url: '/static/img/logo.png', set: 'Basis', collector_number: '58/102', language: 'de'}
					]
				})`),
				chromedp.WaitVisible(".scan-result"),
				poll(`!document.querySelector('.scan-result').innerText.includes('base1-58')`, chromedp.WithPollingTimeout(time.Second)),
				poll(`document.activeElement === document.querySelector('.scan-result')`, chromedp.WithPollingTimeout(5*time.Second)),
				actionFunc(func(context.Context) error { t.Log("result focused"); return nil }),
				poll(`document.querySelectorAll('.scan-candidate').length === 2 && document.querySelectorAll('.scan-candidate')[1].textContent.includes('Basis') && document.querySelectorAll('.scan-candidate')[1].textContent.includes('#58/102') && document.querySelectorAll('.scan-candidate')[1].textContent.includes('DE')`),
				chromedp.ScrollIntoView(".scan-candidate:nth-of-type(2)"),
				chromedp.Click(".scan-candidate:nth-of-type(2)"),
				actionFunc(func(ctx context.Context) error {
					return chromedp.Do(ctx, evaluate(`if (document.querySelector('[data-testid="detected-card-id"]')?.dataset?.cardId !== 'base1-58-de') { document.querySelectorAll('.scan-candidate')[1]?.click(); }`))
				}),
				actionFunc(func(context.Context) error { t.Log("candidate clicked"); return nil }),
				poll(`document.querySelector('[data-testid="detected-card-id"]').dataset.cardId === 'base1-58-de' && document.querySelectorAll('.scan-candidate')[1].getAttribute('aria-pressed') === 'true' && !Alpine.$data(document.querySelector('#scanner-root .scanner-shell')).matchConfirmed`),
				poll(`!document.querySelector('button[\\@click="addToCollection()"]' ).getClientRects().length`),
			); err != nil {
				var diagnostic string
				_ = chromedp.Do(ctx, evaluateInto(`JSON.stringify({active: document.activeElement?.outerHTML?.slice(0,180), visibility: document.visibilityState, focused: document.hasFocus(), result: document.querySelector('.scan-result')?.getBoundingClientRect().toJSON(), display: getComputedStyle(document.querySelector('.scan-result')).display, refMatches: Alpine.$data(document.querySelector('#scanner-root .scanner-shell')).$refs.scanResult === document.querySelector('.scan-result')})`, &diagnostic))
				t.Log(diagnostic)
				t.Fatalf("review uncertain fixture: %v", err)
			}
			if dir := os.Getenv("POKGET_UI_SCREENSHOT_DIR"); dir != "" {
				var shot []byte
				if err := chromedp.Do(ctx, evaluate(`document.querySelector('.scan-result').scrollIntoView({block: 'start'})`), captureScreenshotInto(&shot)); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("scanner-review-%d.png", viewport.width)), shot, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := chromedp.Do(ctx,
				chromedp.ScrollIntoView(`[data-testid="confirm-printing"]`),
				chromedp.Click(`[data-testid="confirm-printing"]`),
				actionFunc(func(ctx context.Context) error {
					return chromedp.Do(ctx, evaluate(`if (!Alpine.$data(document.querySelector('#scanner-root .scanner-shell'))?.matchConfirmed) { document.querySelector('[data-testid="confirm-printing"]')?.click(); }`))
				}),
				chromedp.WaitVisible(`button[\@click="addToCollection()"]`),
				poll(`!document.querySelector('button[\\@click="addToCollection()"]' ).disabled`),
				chromedp.ScrollIntoView(`button[\@click="reviewMatches()"]`),
				chromedp.Click(`button[\@click="reviewMatches()"]`),
				actionFunc(func(ctx context.Context) error {
					return chromedp.Do(ctx, evaluate(`if (Alpine.$data(document.querySelector('#scanner-root .scanner-shell'))?.matchConfirmed) { document.querySelector('button[\\@click="reviewMatches()"]')?.click(); }`))
				}),
				chromedp.WaitVisible(`[data-testid="confirm-printing"]`),
				poll(`!document.querySelector('button[\\@click="addToCollection()"]' ).getClientRects().length`),
			); err != nil {
				t.Fatalf("confirm and reopen printing review: %v", err)
			}
		})
	}
}

func TestServerImageRetryVisibleForReviewAndErrors(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed")
	}
	server := newScannerProgressServer(t)
	defer server.Close()
	ctx, cancel := context.WithTimeout(newHeadlessBrowserContext(t, chromePath), 30*time.Second)
	defer cancel()
	if err := chromedp.Do(ctx, chromedp.Navigate(server.URL),
		poll(`window.Alpine && document.querySelector('#scanner-root .scanner-shell')`)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                             string
		blob, review, scanError, visible bool
	}{
		{"no crop", false, false, true, false},
		{"clean result", true, false, false, false},
		{"review", true, true, false, true},
		{"no match or error", true, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var visible bool
			script := fmt.Sprintf(`(async () => {
				const scanner = Alpine.$data(document.querySelector('#scanner-root .scanner-shell'));
				scanner.lastScanBlob = %t ? new Blob(['photo']) : null;
				scanner.needsReview = %t;
				scanner.scanError = %t ? 'No match' : '';
				await Alpine.nextTick();
				await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
				const button = [...document.querySelectorAll('button')].find(b => b.textContent.trim() === 'Use server image scan');
				return !!button && button.getClientRects().length > 0;
			})()`, tc.blob, tc.review, tc.scanError)
			if err := chromedp.Do(ctx, evaluateInto(script, &visible, chromedp.EvalAwaitPromise)); err != nil {
				t.Fatal(err)
			}
			if visible != tc.visible {
				t.Fatalf("server image action visible = %v, want %v", visible, tc.visible)
			}
		})
	}
}
