//go:build integration

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestAuthFormPendingAndRecovery(t *testing.T) {
	chrome := mobileTestChromePath()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	pages := newMobileShellServer(t)
	defer pages.Close()
	var requests atomic.Int32
	var validPayload atomic.Bool
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/login" {
			pages.Config.Handler.ServeHTTP(w, r)
			return
		}
		requests.Add(1)
		validPayload.Store(r.Method == http.MethodPost && r.FormValue("email") == "collector@example.invalid" &&
			r.FormValue("password") == "test-password" && r.FormValue("gorilla.csrf.Token") == "test" && r.FormValue("remember") == "on")
		select {
		case <-release:
			http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer unblock()
	ctx, cancel := context.WithTimeout(newHeadlessBrowserContext(t, chrome), 40*time.Second)
	defer cancel()
	if err := chromedp.Do(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(server.URL+"/auth"),
		chromedp.WaitVisible(chromedp.CSS("#login-email")),
		chromedp.SendKeys(chromedp.CSS("#login-email"), "collector@example.invalid"),
		chromedp.SendKeys(chromedp.CSS("#login-password"), "test-password"),
		chromedp.Click(chromedp.CSS("#login-panel .auth-password-toggle")),
		poll(`document.querySelector('#login-password').type === 'text'`),
		chromedp.Click(chromedp.CSS("#login-panel .auth-password-toggle")),
		chromedp.Click(chromedp.CSS("#login-panel [name=remember]")),
		chromedp.Focus(chromedp.CSS("#login-password")),
		chromedp.KeyEvent(kb.Enter),
		poll(`document.querySelector('#login-panel').getAttribute('aria-busy') === 'true' && document.querySelector('#login-panel button[type=submit]').disabled && getComputedStyle(document.querySelector('#login-panel .auth-submit-pending')).display === 'flex'`),
		// Repeated Enter and programmatic submits must not queue a second request.
		chromedp.KeyEvent(kb.Enter),
		evaluate(`document.querySelector('#login-panel').requestSubmit()`),
	); err != nil {
		t.Fatal(err)
	}
	unblock()
	if err := chromedp.Do(ctx,
		poll(`document.activeElement.id === 'auth-error' && document.querySelector('#auth-error').textContent.includes('Invalid email or password') && !document.querySelector('#login-panel button[type=submit]').disabled`),
		poll(`!document.querySelector('[data-pack-opening]') && !document.querySelector('.auth-experience').inert`),
	); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || !validPayload.Load() {
		t.Fatalf("request contract: count=%d, valid payload=%v", requests.Load(), validPayload.Load())
	}
	// The same form can retry after an error.
	if err := chromedp.Do(ctx,
		chromedp.Click(chromedp.CSS("#login-panel button[type=submit]")),
		poll(`document.activeElement.id === 'auth-error' && document.querySelector('#login-panel').getAttribute('aria-busy') === 'false'`),
	); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("retry sent %d total requests; want 2", requests.Load())
	}
}

func TestAuthMotionAndCompactViewport(t *testing.T) {
	chrome := mobileTestChromePath()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	server := newMobileShellServer(t)
	defer server.Close()
	ctx, cancel := context.WithTimeout(newHeadlessBrowserContext(t, chrome), 30*time.Second)
	defer cancel()
	run := func(phase string, actions ...action) {
		t.Helper()
		if err := chromedp.Do(ctx, actions...); err != nil {
			t.Fatalf("%s: %v", phase, err)
		}
		t.Log(phase)
	}
	run("normal motion",
		chromedp.EmulateViewport(320, 568),
		setEmulatedMediaFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "no-preference"}}),
		chromedp.Navigate(server.URL+"/auth"),
		chromedp.WaitVisible(chromedp.CSS("#login-email")),
		poll(`document.getAnimations().some(a => a.animationName === 'auth-float' && a.playState === 'running')`),
	)
	run("reduced motion",
		setEmulatedMediaFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}),
		poll(`matchMedia('(prefers-reduced-motion: reduce)').matches && document.getAnimations().length === 0`),
		poll(`document.documentElement.scrollWidth === innerWidth && document.querySelector('#login-password').getBoundingClientRect().height >= 52`),
	)
	run("keyboard viewport",
		// Approximate a resized visual viewport when a software keyboard opens.
		chromedp.EmulateViewport(320, 300),
		chromedp.Focus(chromedp.CSS("#login-password")),
		chromedp.ScrollIntoView("#login-panel button[type=submit]"),
	)
	run("button is reachable",
		// Chromium rounds scroll offsets to pixels; the rect can retain a fraction.
		poll(`document.querySelector('#login-panel button[type=submit]').getBoundingClientRect().bottom <= innerHeight + 1 && document.documentElement.scrollWidth === innerWidth`),
	)
	run("keyboard tabs",
		chromedp.Click(chromedp.CSS("#login-tab")),
		chromedp.KeyEvent(kb.ArrowRight),
		poll(`document.activeElement.id === 'register-tab' && document.querySelector('#register-tab').getAttribute('aria-selected') === 'true'`),
	)
}

func TestAuthArtworkChangesWhileIdle(t *testing.T) {
	chrome := mobileTestChromePath()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	server := newMobileShellServer(t)
	defer server.Close()
	ctx, cancel := context.WithTimeout(newHeadlessBrowserContext(t, chrome), 40*time.Second)
	defer cancel()
	var initial string
	run := func(phase string, actions ...action) {
		t.Helper()
		if err := chromedp.Do(ctx, actions...); err != nil {
			var state map[string]any
			_ = chromedp.Do(ctx, evaluateInto(`({hidden:document.hidden,reduced:matchMedia('(prefers-reduced-motion: reduce)').matches,paused:document.querySelector('.auth-experience')?.getAttribute('data-art-paused'),collection:document.querySelector('.auth-experience')?.getAttribute('data-card-design'),contains:document.querySelector('.auth-experience')?.contains(document.activeElement),focus:document.hasFocus(),active:document.activeElement?.id})`, &state))
			t.Fatalf("%s: %v; state: %+v", phase, err, state)
		}
		t.Log(phase)
	}
	run("initial collection",
		chromedp.EmulateViewport(1440, 1000),
		// Focus events are suppressed when the headless window is inactive.
		setFocusEmulationEnabled(true),
		setEmulatedMediaFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "no-preference"}}),
		chromedp.Navigate(server.URL+"/auth"),
		poll(`Boolean(document.querySelector('.auth-experience')?.dataset.cardDesign)`, chromedp.WithPollingTimeout(5*time.Second)),
		evaluateInto(`document.querySelector('.auth-experience').dataset.cardDesign`, &initial),
	)
	run("idle rotation",
		pollFunction(`(initial) => document.querySelector('.auth-experience').dataset.cardDesign !== initial`, chromedp.WithPollingArgs(initial), chromedp.WithPollingTimeout(20*time.Second)),
	)
	run("pointer tilt",
		evaluate(`(() => {
			const scene = document.querySelector('[data-foil-scene]');
			const rect = scene.getBoundingClientRect();
			scene.dispatchEvent(new PointerEvent('pointermove', { pointerType: 'mouse', clientX: rect.right - 10, clientY: rect.top + 10 }));
		})()`),
		poll(`parseFloat(document.querySelector('[data-foil-scene]').style.getPropertyValue('--tilt-y')) > 0`, chromedp.WithPollingTimeout(3*time.Second)),
	)
	run("focus input",
		chromedp.Focus(chromedp.CSS("#login-email")),
	)
	run("focus stops artwork",
		poll(`document.querySelector('.auth-experience').dataset.artPaused === 'true'`, chromedp.WithPollingTimeout(2*time.Second)),
		poll(`document.querySelectorAll('.auth-showcase button').length === 0`),
	)
}
