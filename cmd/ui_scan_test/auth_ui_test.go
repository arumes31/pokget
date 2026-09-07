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
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(server.URL+"/auth"),
		chromedp.WaitVisible("#login-email"),
		chromedp.SendKeys("#login-email", "collector@example.invalid"),
		chromedp.SendKeys("#login-password", "test-password"),
		chromedp.Click("#login-panel .auth-password-toggle"),
		chromedp.Poll(`document.querySelector('#login-password').type === 'text'`, nil),
		chromedp.Click("#login-panel .auth-password-toggle"),
		chromedp.Click("#login-panel [name=remember]"),
		chromedp.Focus("#login-password"),
		chromedp.KeyEvent(kb.Enter),
		chromedp.Poll(`document.querySelector('#login-panel').getAttribute('aria-busy') === 'true' && document.querySelector('#login-panel button[type=submit]').disabled && getComputedStyle(document.querySelector('#login-panel .auth-submit-pending')).display === 'flex'`, nil),
		// Repeated Enter and programmatic submits must not queue a second request.
		chromedp.KeyEvent(kb.Enter),
		chromedp.Evaluate(`document.querySelector('#login-panel').requestSubmit()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	unblock()
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.activeElement.id === 'auth-error' && document.querySelector('#auth-error').textContent.includes('Invalid email or password') && !document.querySelector('#login-panel button[type=submit]').disabled`, nil),
		chromedp.Poll(`!document.querySelector('[data-pack-opening]') && !document.querySelector('.auth-experience').inert`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || !validPayload.Load() {
		t.Fatalf("request contract: count=%d, valid payload=%v", requests.Load(), validPayload.Load())
	}
	// The same form can retry after an error.
	if err := chromedp.Run(ctx,
		chromedp.Click("#login-panel button[type=submit]"),
		chromedp.Poll(`document.activeElement.id === 'auth-error' && document.querySelector('#login-panel').getAttribute('aria-busy') === 'false'`, nil),
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
	run := func(phase string, actions ...chromedp.Action) {
		t.Helper()
		if err := chromedp.Run(ctx, actions...); err != nil {
			t.Fatalf("%s: %v", phase, err)
		}
		t.Log(phase)
	}
	run("normal motion",
		chromedp.EmulateViewport(320, 568),
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "no-preference"}}),
		chromedp.Navigate(server.URL+"/auth"),
		chromedp.WaitVisible("#login-email"),
		chromedp.Poll(`document.getAnimations().some(a => a.animationName === 'auth-float' && a.playState === 'running')`, nil),
	)
	run("reduced motion",
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}),
		chromedp.Poll(`matchMedia('(prefers-reduced-motion: reduce)').matches && document.getAnimations().length === 0`, nil),
		chromedp.Poll(`document.documentElement.scrollWidth === innerWidth && document.querySelector('#login-password').getBoundingClientRect().height >= 52`, nil),
	)
	run("keyboard viewport",
		// Approximate a resized visual viewport when a software keyboard opens.
		chromedp.EmulateViewport(320, 300),
		chromedp.Focus("#login-password"),
		chromedp.ScrollIntoView("#login-panel button[type=submit]"),
	)
	run("button is reachable",
		// Chromium rounds scroll offsets to pixels; the rect can retain a fraction.
		chromedp.Poll(`document.querySelector('#login-panel button[type=submit]').getBoundingClientRect().bottom <= innerHeight + 1 && document.documentElement.scrollWidth === innerWidth`, nil),
	)
	run("keyboard tabs",
		chromedp.Click("#login-tab"),
		chromedp.KeyEvent(kb.ArrowRight),
		chromedp.Poll(`document.activeElement.id === 'register-tab' && document.querySelector('#register-tab').getAttribute('aria-selected') === 'true'`, nil),
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
	run := func(phase string, actions ...chromedp.Action) {
		t.Helper()
		if err := chromedp.Run(ctx, actions...); err != nil {
			var state map[string]any
			_ = chromedp.Run(ctx, chromedp.Evaluate(`({hidden:document.hidden,reduced:matchMedia('(prefers-reduced-motion: reduce)').matches,paused:document.querySelector('.auth-experience')?.getAttribute('data-art-paused'),collection:document.querySelector('.auth-experience')?.getAttribute('data-card-design'),contains:document.querySelector('.auth-experience')?.contains(document.activeElement),focus:document.hasFocus(),active:document.activeElement?.id})`, &state))
			t.Fatalf("%s: %v; state: %+v", phase, err, state)
		}
		t.Log(phase)
	}
	run("initial collection",
		chromedp.EmulateViewport(1440, 1000),
		// Focus events are suppressed when the headless window is inactive.
		emulation.SetFocusEmulationEnabled(true),
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "no-preference"}}),
		chromedp.Navigate(server.URL+"/auth"),
		chromedp.Poll(`Boolean(document.querySelector('.auth-experience')?.dataset.cardDesign)`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.auth-experience').dataset.cardDesign`, &initial),
	)
	run("idle rotation",
		chromedp.PollFunction(`(initial) => document.querySelector('.auth-experience').dataset.cardDesign !== initial`, nil, chromedp.WithPollingArgs(initial), chromedp.WithPollingTimeout(20*time.Second)),
	)
	run("pointer tilt",
		chromedp.Evaluate(`(() => {
			const scene = document.querySelector('[data-foil-scene]');
			const rect = scene.getBoundingClientRect();
			scene.dispatchEvent(new PointerEvent('pointermove', { pointerType: 'mouse', clientX: rect.right - 10, clientY: rect.top + 10 }));
		})()`, nil),
		chromedp.Poll(`parseFloat(document.querySelector('[data-foil-scene]').style.getPropertyValue('--tilt-y')) > 0`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	)
	run("focus input",
		chromedp.Focus("#login-email"),
	)
	run("focus stops artwork",
		chromedp.Poll(`document.querySelector('.auth-experience').dataset.artPaused === 'true'`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Poll(`document.querySelectorAll('.auth-showcase button').length === 0`, nil),
	)
}
