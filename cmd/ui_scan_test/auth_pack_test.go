//go:build integration

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestAuthPackSuccessfulLogin(t *testing.T) {
	chrome := mobileTestChromePath()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	pages := newMobileShellServer(t)
	defer pages.Close()
	var requests atomic.Int32
	var sessionAtDestination atomic.Bool
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/login" {
			requests.Add(1)
			<-release
			http.SetCookie(w, &http.Cookie{Name: "pack-test-session", Value: "authenticated", Path: "/", HttpOnly: true})
			w.Header().Set("HX-Redirect", "/?entered=collection#cards")
			w.Header().Set("HX-Replace-Url", "/?entered=collection#cards")
			return
		}
		if r.URL.Path == "/session-check" {
			cookie, err := r.Cookie("pack-test-session")
			if err != nil || cookie.Value != "authenticated" {
				w.WriteHeader(401)
			}
			return
		}
		if r.URL.Path == "/" {
			cookie, err := r.Cookie("pack-test-session")
			sessionAtDestination.Store(err == nil && cookie.Value == "authenticated")
		}
		pages.Config.Handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	defer unblock()
	ctx, cancel := context.WithTimeout(newHeadlessBrowserContext(t, chrome), 30*time.Second)
	defer cancel()
	if err := chromedp.Do(ctx,
		chromedp.EmulateViewport(320, 568), setFocusEmulationEnabled(true),
		setEmulatedMediaFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "no-preference"}}),
		chromedp.Navigate(server.URL+"/auth"),
		chromedp.SendKeys("#login-email", "collector@example.invalid"),
		chromedp.SendKeys("#login-password", "test-password"),
		chromedp.Click("#login-panel button[type=submit]"),
		poll(`document.querySelector('#login-panel').getAttribute('aria-busy') === 'true' && !document.querySelector('[data-pack-opening]')`),
	); err != nil {
		t.Fatal(err)
	}
	unblock()
	started := time.Now()
	if err := chromedp.Do(ctx,
		poll(`!!document.querySelector('[data-pack-opening]')`, chromedp.WithPollingTimeout(2*time.Second)),
		poll(`document.querySelector('.auth-experience').inert && document.documentElement.scrollWidth === innerWidth`),
		pollFunction(`async () => (await fetch('/session-check')).ok`, nil),
		chromedp.KeyEvent(kb.Enter),
		evaluate(`document.querySelector('#login-panel').requestSubmit()`),
		poll(`document.querySelector('[data-pack-opening]')?.dataset.stage === 'pack-tear'`),
		poll(`new DOMMatrix(getComputedStyle(document.querySelector('.pack-flipper')).transform).m11 > .99`),
		poll(`document.querySelector('[data-pack-opening]')?.dataset.stage === 'card-reveal'`),
		poll(`new DOMMatrix(getComputedStyle(document.querySelector('.pack-flipper')).transform).m11 < -.99 && Number(getComputedStyle(document.querySelector('.pack-top')).opacity) === 0`),
		waitPackDestination("?entered=collection#cards"),
		chromedp.WaitReady("body"),
		poll(`!document.querySelector('[data-pack-opening]')`),
	); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 4500*time.Millisecond || elapsed > 7*time.Second {
		t.Fatalf("transition duration: %v", elapsed)
	}
	if requests.Load() != 1 || !sessionAtDestination.Load() {
		t.Fatalf("requests=%d, authenticated destination=%v", requests.Load(), sessionAtDestination.Load())
	}
	if err := chromedp.Do(ctx, chromedp.Reload(), poll(`!document.querySelector('[data-pack-opening]')`)); err != nil {
		t.Fatal(err)
	}
}

// A full document navigation destroys a Runtime polling context; query the URL
// from CDP afresh until the real navigation commits instead.
func waitPackDestination(suffix string) action {
	return actionFunc(func(ctx context.Context) error {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			var url string
			if err := chromedp.Do(ctx, locationInto(&url)); err == nil && strings.HasSuffix(url, suffix) {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	})
}

func TestAuthPackShortPaths(t *testing.T) {
	chrome := mobileTestChromePath()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	for _, mode := range []string{"reduced", "escape", "hidden", "cleanup", "missing-css"} {
		t.Run(mode, func(t *testing.T) {
			pages := newMobileShellServer(t)
			defer pages.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "missing-css" && r.URL.Path == "/static/css/auth-pack.css" {
					http.NotFound(w, r)
					return
				}
				if r.URL.Path == "/auth/login" {
					w.Header().Set("HX-Redirect", "/?short="+mode)
					return
				}
				pages.Config.Handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(newHeadlessBrowserContext(t, chrome), 20*time.Second)
			defer cancel()
			preference := "no-preference"
			if mode == "reduced" {
				preference = "reduce"
			}
			if err := chromedp.Do(ctx,
				chromedp.EmulateViewport(390, 844), setFocusEmulationEnabled(true),
				setEmulatedMediaFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: preference}}),
				chromedp.Navigate(server.URL+"/auth"),
				chromedp.SendKeys("#login-email", "collector@example.invalid"),
				chromedp.SendKeys("#login-password", "test-password"),
			); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			if err := chromedp.Do(ctx, chromedp.Click("#login-panel button[type=submit]")); err != nil {
				t.Fatal(err)
			}
			if mode != "missing-css" {
				if err := chromedp.Do(ctx, poll(`!!document.querySelector('[data-pack-opening]')`, chromedp.WithPollingTimeout(time.Second))); err != nil {
					t.Fatal(err)
				}
			}
			var action action
			switch mode {
			case "reduced":
				action = poll(`document.querySelector('[data-pack-opening]').classList.contains('pack-reduced') && document.querySelector('[data-pack-opening]').getAnimations({subtree:true}).every(a => !a.animationName)`)
			case "escape":
				action = chromedp.KeyEvent(kb.Escape)
			case "hidden":
				action = evaluate(`Object.defineProperty(document,'hidden',{value:true,configurable:true});document.dispatchEvent(new Event('visibilitychange'));`)
			case "cleanup":
				action = evaluate(`window.dispatchEvent(new PageTransitionEvent('pagehide'));`)
			}
			if action != nil {
				if err := chromedp.Do(ctx, action); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "cleanup" {
				if err := chromedp.Do(ctx, poll(`!document.querySelector('[data-pack-opening]') && !document.querySelector('.auth-experience').inert && !document.documentElement.classList.contains('pack-playing')`)); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := chromedp.Do(ctx, waitPackDestination("?short="+mode), chromedp.WaitReady("body")); err != nil {
					t.Fatal(err)
				}
				if elapsed := time.Since(started); elapsed > 3*time.Second {
					t.Fatalf("short path took %v", elapsed)
				}
			}
		})
	}
}
