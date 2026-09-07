package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func mobileHardeningPage(t *testing.T, path string) context.Context {
	t.Helper()
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed")
	}
	server := newMobileShellServer(t)
	t.Cleanup(server.Close)
	browser := newHeadlessBrowserContext(t, chromePath)
	ctx, cancel := context.WithTimeout(browser, 40*time.Second)
	t.Cleanup(cancel)
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(server.URL+path),
		chromedp.WaitVisible("#main-content .app-page"),
		chromedp.Poll(`document.readyState === 'complete' && window.Alpine`, nil),
	); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestMobileHardeningHelperTextContrast(t *testing.T) {
	for _, target := range []struct{ name, route, selector string }{
		{"binder-optional", "/binders", `label[for="new-binder-description"] span`},
		{"settings-default", "/settings", `.currency-options label:first-child p:last-child`},
	} {
		t.Run(target.name, func(t *testing.T) {
			ctx := mobileHardeningPage(t, "/?filled=1")
			if err := chromedp.Run(ctx, loadReviewRoute(target.route)); err != nil {
				t.Fatal(err)
			}
			if target.name == "binder-optional" {
				if err := chromedp.Run(ctx, chromedp.Click(`[x-ref="newBinderTrigger"]`, chromedp.ByQuery), chromedp.WaitVisible("#new-binder-name")); err != nil {
					t.Fatal(err)
				}
			}
			var ratio float64
			if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`(() => {
				const element = document.querySelector(%q);
				const canvas = document.createElement('canvas'); canvas.width = canvas.height = 1;
				const context = canvas.getContext('2d', {willReadFrequently: true});
				const rgba = value => { context.clearRect(0,0,1,1); context.fillStyle = value; context.fillRect(0,0,1,1); return [...context.getImageData(0,0,1,1).data]; };
				const layers = []; for (let parent = element; parent; parent = parent.parentElement) layers.push(rgba(getComputedStyle(parent).backgroundColor));
				let background = [255,255,255];
				for (const layer of layers.reverse()) background = background.map((value, index) => layer[index] * layer[3] / 255 + value * (1 - layer[3] / 255));
				const text = rgba(getComputedStyle(element).color);
				const foreground = background.map((value, index) => text[index] * text[3] / 255 + value * (1 - text[3] / 255));
				const luminance = color => color.map(value => value / 255).map(value => value <= .04045 ? value / 12.92 : ((value + .055) / 1.055) ** 2.4).reduce((sum, value, index) => sum + value * [.2126,.7152,.0722][index], 0);
				const values = [luminance(foreground), luminance(background)].sort((a,b) => b-a);
				return (values[0] + .05) / (values[1] + .05);
			})()`, target.selector), &ratio)); err != nil {
				t.Fatal(err)
			}
			if ratio < 4.5 {
				t.Errorf("helper text contrast %.2f:1 is below 4.5:1", ratio)
			}
		})
	}
}

func TestMobileHardeningNavigationAccessibleNames(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1")
	var nodes []*accessibility.Node
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		nodes, err = accessibility.GetFullAXTree().Do(ctx)
		return err
	})); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, node := range nodes {
		if node.Ignored || node.Role == nil || node.Name == nil {
			continue
		}
		var role, name string
		if err := json.Unmarshal(node.Role.Value, &role); err != nil {
			t.Fatal(err)
		}
		if role != "button" {
			continue
		}
		if err := json.Unmarshal(node.Name.Value, &name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	for _, want := range []string{"Vault", "Grails", "Scan", "Binders", "More"} {
		found := false
		for _, name := range names {
			found = found || name == want
		}
		if !found {
			t.Errorf("expected clean accessible name %q; browser button names: %v", want, names)
		}
	}
}

func TestMobileHardeningEmptyMisprintFocusReturn(t *testing.T) {
	for _, closeMethod := range []string{"button", "escape"} {
		t.Run(closeMethod, func(t *testing.T) {
			ctx := mobileHardeningPage(t, "/?filled=0&view=errors")
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`window.hardeningOpener = document.querySelector('[x-ref="submitErrorTrigger"]')`, nil),
				chromedp.Click(`[x-ref="submitErrorTrigger"]`, chromedp.ByQuery),
				chromedp.Poll(`document.activeElement.id === 'error-card-id'`, nil),
			); err != nil {
				t.Fatal(err)
			}
			var closeAction chromedp.Action = chromedp.KeyEvent(kb.Escape)
			if closeMethod == "button" {
				closeAction = chromedp.Click(`[aria-label="Close submission dialog"]`, chromedp.ByQuery)
			}
			if err := chromedp.Run(ctx, closeAction,
				chromedp.Poll(`document.activeElement === window.hardeningOpener`, nil, chromedp.WithPollingTimeout(2*time.Second)),
			); err != nil {
				t.Errorf("closing empty-state dialog did not restore its opener: %v", err)
			}
		})
	}
}

func TestMobileClarificationMisprintPublicationNotice(t *testing.T) {
	for _, fontSize := range []int{16, 32} {
		t.Run(fmt.Sprintf("font-%d", fontSize), func(t *testing.T) {
			ctx := mobileHardeningPage(t, "/?filled=0&view=errors")
			var issues []string
			if err := chromedp.Run(ctx,
				chromedp.EmulateViewport(320, 844),
				chromedp.Evaluate(fmt.Sprintf(`document.documentElement.style.setProperty('font-size', '%dpx', 'important')`, fontSize), nil),
				chromedp.Click(`[x-ref="submitErrorTrigger"]`, chromedp.ByQuery),
				chromedp.WaitVisible("#error-card-id"),
				chromedp.Evaluate(`(() => {
					const issues = [], field = document.querySelector('#error-card-id');
					const form = field.closest('form'), button = form.querySelector('button[type="submit"]');
					const help = document.getElementById(field.getAttribute('aria-describedby'));
					if (field.type === 'search' || /search/i.test(field.placeholder + [...field.labels].map(label => label.textContent).join(' '))) issues.push('catalog ID field implies a search capability');
					if (!help || !help.textContent.includes('scan result')) issues.push('catalog ID guidance is not associated with its field');
					if (button.textContent.trim() !== 'Publish report' || /submit for review/i.test(form.innerText)) issues.push('publish action implies a review queue');
					const notice = [...form.querySelectorAll('p')].find(element => element.textContent.includes('becomes public immediately'));
					if (!notice || !notice.textContent.includes('not reviewed before publication')) return [...issues, 'immediate public publication and lack of review are not disclosed'];
					if (!(notice.compareDocumentPosition(button) & Node.DOCUMENT_POSITION_FOLLOWING)) issues.push('publication disclosure follows the publish button');
					if (!notice.getClientRects().length || notice.closest('[aria-hidden="true"], [inert]') || getComputedStyle(notice).visibility === 'hidden') issues.push('publication disclosure is hidden from readers');
					const box = notice.getBoundingClientRect();
					if (box.left < -1 || box.right > innerWidth + 1 || notice.scrollHeight > notice.clientHeight + 1) issues.push('publication disclosure is clipped');
					return issues;
				})()`, &issues),
			); err != nil {
				t.Fatal(err)
			}
			if len(issues) != 0 {
				t.Errorf("report form copy: %v", issues)
			}
		})
	}
}

func TestMobileHardeningStandaloneMisprintFocusTrap(t *testing.T) {
	ctx := mobileHardeningPage(t, "/errors?hardening=1")
	if err := chromedp.Run(ctx,
		chromedp.Click(`[x-ref="submitErrorTrigger"]`, chromedp.ByQuery),
		chromedp.Poll(`document.activeElement.id === 'error-card-id'`, nil),
	); err != nil {
		t.Fatal(err)
	}
	for _, direction := range []struct {
		name, selector string
		modifiers      input.Modifier
	}{
		{"forward", `[role="dialog"] button[type="submit"]`, input.ModifierNone},
		{"backward", `[aria-label="Close submission dialog"]`, input.ModifierShift},
	} {
		t.Run(direction.name, func(t *testing.T) {
			var contained bool
			if err := chromedp.Run(ctx, chromedp.Focus(direction.selector), chromedp.KeyEvent(kb.Tab, chromedp.KeyModifiers(direction.modifiers)), chromedp.Evaluate(`Boolean(document.activeElement.closest('[role="dialog"]'))`, &contained)); err != nil {
				t.Fatal(err)
			}
			if !contained {
				t.Error("Tab escaped the standalone misprint dialog")
			}
		})
	}
}

func TestMobileHardeningMisprintFilterRecovery(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1&view=errors")
	if err := submitMisprintSearch(ctx, "No matching fixture", "ink"); err != nil {
		t.Fatal(err)
	}
	var canClear bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('[data-misprint]').length === 0 && [...document.querySelectorAll('a[href="/errors"]')].some(link => link.getClientRects().length && link.textContent.includes('Clear filters'))`, &canClear)); err != nil {
		t.Fatal(err)
	}
	if !canClear {
		t.Fatal("zero matching misprints have no visible Clear filters action")
	}
	if err := clickMisprintLink(ctx, `a[href="/errors"]`); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.querySelector('#misprint-search [name=q]').value === '' && document.querySelector('#misprint-search [name=type]').value === 'all' && document.querySelector('[data-misprint]').getClientRects().length > 0 && document.activeElement === document.querySelector('#misprint-query')`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		var state string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({query: document.querySelector('#misprint-query')?.value, type: document.querySelector('#misprint-search [name=type]')?.value, count: document.querySelectorAll('[data-misprint]').length, active: document.activeElement?.outerHTML?.slice(0,150)})`, &state))
		t.Log(state)
		t.Fatalf("clear filters did not restore results: %v", err)
	}
}

func TestMobileHardeningMisprintDescriptionDisclosure(t *testing.T) {
	ctx := mobileHardeningPage(t, "/errors?hardening=1")
	var hasDisclosure bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll('summary')].some(summary => summary.textContent.trim() === 'Read description')`, &hasDisclosure)); err != nil {
		t.Fatal(err)
	}
	if !hasDisclosure {
		t.Fatal("long misprint description has no Read description disclosure")
	}
	var readable bool
	if err := chromedp.Run(ctx,
		chromedp.Click(`//summary[normalize-space(.)='Read description']`, chromedp.BySearch),
		chromedp.Evaluate(`(() => { const details = document.querySelector('[data-misprint] details'); const text = [...details.querySelectorAll('p')].find(element => element.textContent.includes('Final diagnostic detail.')); return details.open && !!text && text.getClientRects().length > 0 && text.scrollHeight <= text.clientHeight + 1; })()`, &readable),
	); err != nil {
		t.Fatal(err)
	}
	if !readable {
		t.Error("opening description still clips the final diagnostic detail")
	}
}

func TestMobileHardeningUnpricedTradeCanRecover(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1")
	if err := chromedp.Run(ctx, loadReviewRoute("/trade?hardening=1"), chromedp.Evaluate(`(() => {
		const card = document.querySelector('#my-trade-card'); card.value = 'unpriced-item'; card.dispatchEvent(new Event('change', {bubbles:true}));
		const offer = document.querySelector('#their-offer'); offer.value = '10'; offer.dispatchEvent(new Event('input', {bubbles:true}));
	})()`, nil)); err != nil {
		t.Fatal(err)
	}
	var editable bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Boolean(document.querySelector('#my-offer')?.getClientRects().length)`, &editable)); err != nil {
		t.Fatal(err)
	}
	if !editable {
		t.Fatal("selected card without market price has no editable own-value input")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`const offer = document.querySelector('#my-offer'); offer.value = '12.50'; offer.dispatchEvent(new Event('input', {bubbles:true}))`, nil),
		chromedp.Poll(`document.querySelectorAll('[data-testid="trade-verdict"] button').length === 2 && [...document.querySelectorAll('[data-testid="trade-verdict"] button')].every(button => !button.disabled)`, nil),
	); err != nil {
		t.Fatalf("entering own value did not enable exports: %v", err)
	}
	var summary string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Alpine.$data(document.querySelector('#main-content .app-page')).summary()`, &summary)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "12.50") || !strings.Contains(summary, "unpriced-item") {
		t.Errorf("recovered export lacks selected card or entered value: %s", summary)
	}
}

func TestMobileHardeningTradeRejectsInvalidValues(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1&view=trade")
	for _, field := range []string{"myOffer", "theirOffer"} {
		for _, value := range []string{"-1", "NaN", "Infinity"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				var invalid bool
				if err := chromedp.Run(ctx,
					chromedp.Evaluate(fmt.Sprintf(`(() => { const state = Alpine.$data(document.querySelector('#main-content .app-page')); state.myPortfolioId = 'test-item'; state.myOffer = 12.5; state.theirOffer = 10; state[%q] = %s; })()`, field, value), nil),
					chromedp.Evaluate(`!Alpine.$data(document.querySelector('#main-content .app-page')).ready && document.querySelectorAll('[data-testid="trade-verdict"] button').length === 2 && [...document.querySelectorAll('[data-testid="trade-verdict"] button')].every(button => button.disabled)`, &invalid),
				); err != nil {
					t.Fatal(err)
				}
				if !invalid {
					t.Error("invalid trade value leaves comparison or exports enabled")
				}
			})
		}
	}
}

func TestMobileHardeningBinderInitialOrderMatchesNameSort(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1")
	if err := chromedp.Run(ctx, loadReviewRoute("/binders?hardening=1")); err != nil {
		t.Fatal(err)
	}
	err := chromedp.Run(ctx, chromedp.Poll(`[...document.querySelectorAll('[data-binder-card]')].map(card => card.dataset.name).join(',') === 'Alpha,Moon,Zeta'`, nil, chromedp.WithPollingTimeout(2*time.Second)))
	if err != nil {
		var names []string
		if readErr := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll('[data-binder-card]')].map(card => card.dataset.name)`, &names)); readErr != nil {
			t.Fatal(readErr)
		}
		t.Errorf("initial Name sorting does not match rendered order: %v", names)
	}
}
