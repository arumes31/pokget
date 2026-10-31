package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

type menuGeometry struct {
	Position   string  `json:"position"`
	Top        float64 `json:"top"`
	Bottom     float64 `json:"bottom"`
	Left       float64 `json:"left"`
	Right      float64 `json:"right"`
	Height     float64 `json:"height"`
	ViewportW  float64 `json:"viewportWidth"`
	ViewportH  float64 `json:"viewportHeight"`
	HitInside  bool    `json:"hitInside"`
	Scrolls    bool    `json:"scrolls"`
	TriggerTop float64 `json:"triggerTop"`
}

func inspectScannerMenu(ctx context.Context, menuTestID, triggerSelector string) (menuGeometry, error) {
	var geometry menuGeometry
	err := chromedp.Do(ctx,
		chromedp.Click(chromedp.CSS(triggerSelector)),
		chromedp.WaitVisible(chromedp.CSS(`[data-testid="`+menuTestID+`"]`)),
		evaluateInto(`(() => {
			const menu = document.querySelector('[data-testid="`+menuTestID+`"]');
			const trigger = document.querySelector('`+triggerSelector+`');
			const rect = menu.getBoundingClientRect();
			const style = getComputedStyle(menu);
			const hit = document.elementFromPoint(rect.left + rect.width / 2, rect.top + Math.min(rect.height / 2, 16));
			return {
				position: style.position,
				top: rect.top,
				bottom: rect.bottom,
				left: rect.left,
				right: rect.right,
				height: rect.height,
				viewportWidth: innerWidth,
				viewportHeight: innerHeight,
				hitInside: Boolean(hit && menu.contains(hit)),
				scrolls: style.overflowY === 'auto' || style.overflowY === 'scroll',
				triggerTop: trigger.getBoundingClientRect().top
			};
		})()`, &geometry),
	)
	return geometry, err
}

func TestScannerGameMenuRendersInsideTheViewport(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed; skipping scanner menu check")
	}
	server := newScannerProgressServer(t)
	defer server.Close()
	browser := newHeadlessBrowserContext(t, chromePath)
	tab, cancelTab := chromedp.NewContext(browser)
	defer cancelTab()
	ctx, cancel := context.WithTimeout(tab, 45*time.Second)
	defer cancel()

	if err := chromedp.Do(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(server.URL),
		chromedp.WaitVisible(chromedp.CSS(`[data-testid="scanner-mode-scan"]`)),
	); err != nil {
		t.Fatalf("open scanner: %v", err)
	}

	geometry, err := inspectScannerMenu(ctx, "scanner-game-menu", `button[aria-label="Select Trading Card Game"]`)
	if err != nil {
		t.Fatalf("open game menu: %v", err)
	}
	if geometry.Position != "fixed" {
		t.Errorf("game menu position = %q, want fixed so the scrolling control pane cannot clip it: %+v", geometry.Position, geometry)
	}
	if geometry.Height < 44 {
		t.Errorf("game menu has no usable height: %+v", geometry)
	}
	if geometry.Top < -1 || geometry.Bottom > geometry.ViewportH+1 || geometry.Left < -1 || geometry.Right > geometry.ViewportW+1 {
		t.Errorf("game menu escapes the viewport: %+v", geometry)
	}
	if !geometry.HitInside {
		t.Errorf("game menu is covered by another element at its centre: %+v", geometry)
	}
	if !geometry.Scrolls {
		t.Errorf("game menu cannot scroll its games: %+v", geometry)
	}
	if geometry.Top >= geometry.TriggerTop {
		t.Errorf("game menu should open above its trigger: %+v", geometry)
	}

	var selected string
	if err := chromedp.Do(ctx,
		evaluate(`(() => {
			const button = [...document.querySelectorAll('[data-testid="scanner-game-menu"] button')]
				.find((entry) => entry.textContent.includes('Magic: The Gathering'));
			button.click();
		})()`),
		poll(`(() => {
			const state = Alpine.$data(document.querySelector('.scanner-shell'));
			window.__selectedGame = state.game;
			return state.game === 'magic' && !state.showGamePopover;
		})()`),
		evaluateInto(`window.__selectedGame`, &selected),
	); err != nil {
		t.Fatalf("select a game from the menu: %v", err)
	}
	if selected != "magic" {
		t.Errorf("selected game = %q, want magic", selected)
	}
}

func TestScannerLanguageMenuRendersInsideTheViewport(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed; skipping scanner menu check")
	}
	server := newScannerProgressServer(t)
	defer server.Close()
	browser := newHeadlessBrowserContext(t, chromePath)
	tab, cancelTab := chromedp.NewContext(browser)
	defer cancelTab()
	ctx, cancel := context.WithTimeout(tab, 45*time.Second)
	defer cancel()

	if err := chromedp.Do(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(server.URL),
		chromedp.WaitVisible(chromedp.CSS(`[data-testid="scanner-mode-scan"]`)),
		chromedp.WaitVisible(chromedp.CSS(`button[aria-label="Select card language"]`)),
	); err != nil {
		t.Fatalf("open scanner: %v", err)
	}

	geometry, err := inspectScannerMenu(ctx, "scanner-language-menu", `button[aria-label="Select card language"]`)
	if err != nil {
		t.Fatalf("open language menu: %v", err)
	}
	if geometry.Position != "fixed" {
		t.Errorf("language menu position = %q, want fixed: %+v", geometry.Position, geometry)
	}
	if geometry.Top < -1 || geometry.Bottom > geometry.ViewportH+1 || geometry.Left < -1 || geometry.Right > geometry.ViewportW+1 {
		t.Errorf("language menu escapes the viewport: %+v", geometry)
	}
	if !geometry.HitInside || geometry.Height < 44 {
		t.Errorf("language menu is not interactable: %+v", geometry)
	}

	if err := chromedp.Do(ctx,
		evaluate(`document.querySelector('[data-testid="scanner-language-menu"] button').click()`),
		poll(`!Alpine.$data(document.querySelector('.scanner-shell')).showLangPopover`),
	); err != nil {
		t.Fatalf("select a language from the menu: %v", err)
	}
}

func TestScannerAccentColourFollowsSelectedGame(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed; skipping scanner accent check")
	}
	server := newScannerProgressServer(t)
	defer server.Close()
	browser := newHeadlessBrowserContext(t, chromePath)
	tab, cancelTab := chromedp.NewContext(browser)
	defer cancelTab()
	ctx, cancel := context.WithTimeout(tab, 40*time.Second)
	defer cancel()

	var accents = map[string]string{}
	expected := map[string]string{"pokemon": "#ef4444", "magic": "#f59e0b", "digimon": "#22d3ee"}
	if err := chromedp.Do(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(server.URL),
		chromedp.WaitVisible(chromedp.CSS(`[data-testid="scanner-mode-scan"]`)),
	); err != nil {
		t.Fatalf("open scanner: %v", err)
	}
	for game, accent := range expected {
		var current struct {
			Value  string `json:"value"`
			Border string `json:"border"`
		}
		if err := chromedp.Do(ctx,
			evaluate(fmt.Sprintf(`Alpine.$data(document.querySelector('.scanner-shell')).setGame(%q)`, game)),
			poll(fmt.Sprintf(`getComputedStyle(document.querySelector('.scanner-shell')).getPropertyValue('--tcg-accent').trim() === %q`, accent)),
			evaluateInto(`(() => {
				const shell = document.querySelector('.scanner-shell');
				const line = document.querySelector('.scanner-accent-line');
				return {
					value: getComputedStyle(shell).getPropertyValue('--tcg-accent').trim(),
					border: getComputedStyle(line).backgroundColor
				};
			})()`, &current),
		); err != nil {
			t.Fatalf("apply %s accent: %v", game, err)
		}
		accents[game] = current.Value + "|" + current.Border
	}

	if accents["pokemon"] == "" || accents["magic"] == "" || accents["digimon"] == "" {
		t.Fatalf("scanner accents missing: %+v", accents)
	}
	if accents["pokemon"] == accents["magic"] || accents["magic"] == accents["digimon"] || accents["pokemon"] == accents["digimon"] {
		t.Errorf("each TCG must get its own scanner colour: %+v", accents)
	}
	if accents["pokemon"] != "#ef4444|rgb(239, 68, 68)" {
		t.Errorf("pokemon accent = %q, want the red default applied to the shutter", accents["pokemon"])
	}
}
