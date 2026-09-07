package main

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestScopedComponentButtonsPreserveTouchTargets(t *testing.T) {
	chrome := mobileTestChromePath()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	server := newMobileShellServer(t)
	defer server.Close()
	ctx, cancel := context.WithTimeout(newHeadlessBrowserContext(t, chrome), 30*time.Second)
	defer cancel()
	check := func(selector string) {
		t.Helper()
		var buttons []struct {
			Name   string
			Height float64
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll(`+"`"+selector+"`"+`)).filter(e=>e.getClientRects().length).map(e=>({Name:e.getAttribute('aria-label')||e.textContent.trim(),Height:e.getBoundingClientRect().height}))`, &buttons)); err != nil {
			t.Fatal(err)
		}
		if len(buttons) == 0 {
			t.Fatalf("no visible buttons matched %s", selector)
		}
		for _, button := range buttons {
			if button.Height < 43.5 {
				t.Errorf("%s: button %q height %.2fpx, want at least44px", selector, button.Name, button.Height)
			}
		}
		t.Logf("measured %d visible %s controls", len(buttons), selector)
	}
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(320, 568), chromedp.Navigate(server.URL+"/auth"), chromedp.WaitVisible("#login-email", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	check(".auth-experience button")
	if err := chromedp.Run(ctx, chromedp.Click("#register-tab", chromedp.ByQuery), chromedp.WaitVisible("#register-email", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	check(".auth-experience button")
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL+"/?view=scan"), chromedp.WaitVisible("[data-testid=open-measure]", chromedp.ByQuery), chromedp.Click("[data-testid=open-measure]", chromedp.ByQuery), chromedp.WaitVisible(".measure-text-button", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	check(".card-tools button")
}
