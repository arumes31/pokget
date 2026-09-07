package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// This fixture exercises browser GET submissions and links. Handler tests own
// the database query, filtering and pagination guarantees against real SQL.
func misprintFixturePage(request *http.Request, original map[string]any) map[string]any {
	data := make(map[string]any, len(original)+7)
	for key, value := range original {
		data[key] = value
	}
	reports, _ := original["Errors"].([]map[string]any)
	if cookie, err := request.Cookie("ui-fixture-catalog"); err == nil && cookie.Value == "1" {
		reports = make([]map[string]any, 0, 50)
		for index := range 50 {
			name, kind := fmt.Sprintf("Recent report %02d", index+1), "Miscut"
			if index >= 24 {
				name = fmt.Sprintf("Archive report %02d", index-23)
			}
			if index == 49 {
				name, kind = "Archive target report", "Ink error"
			}
			reports = append(reports, map[string]any{"CardName": name, "SetName": "Base Set", "ErrorType": kind,
				"Description": "A printing error documented by a collector.", "EstimatedValueMultiplier": 1.2, "ImageURL": "/static/img/logo.png"})
		}
	}
	query, kind := strings.TrimSpace(request.URL.Query().Get("q")), request.URL.Query().Get("type")
	if kind == "" {
		kind = "all"
	}
	filtered := make([]map[string]any, 0, len(reports))
	for _, report := range reports {
		text := fmt.Sprint(report["CardName"], " ", report["SetName"], " ", report["ErrorType"], " ", report["Description"])
		if !strings.Contains(strings.ToLower(text), strings.ToLower(query)) || (kind != "all" && !strings.Contains(strings.ToLower(fmt.Sprint(report["ErrorType"])), kind)) {
			continue
		}
		filtered = append(filtered, report)
	}
	page, _ := strconv.Atoi(request.URL.Query().Get("page"))
	page = min(max(1, page), len(filtered)/24+1)
	start := min((page-1)*24, len(filtered))
	end := min(start+24, len(filtered))
	pageURL := func(number int) string {
		values := url.Values{"page": {strconv.Itoa(number)}}
		if query != "" {
			values.Set("q", query)
		}
		if kind != "all" {
			values.Set("type", kind)
		}
		return "/errors?" + values.Encode()
	}
	data["Errors"], data["MisprintQuery"], data["MisprintType"] = filtered[start:end], query, kind
	data["MisprintPage"], data["MisprintFiltered"] = page, query != "" || kind != "all"
	data["MisprintPreviousURL"], data["MisprintNextURL"] = "", ""
	if page > 1 {
		data["MisprintPreviousURL"] = pageURL(page - 1)
	}
	if end < len(filtered) {
		data["MisprintNextURL"] = pageURL(page + 1)
	}
	return data
}

const observeMisprintRequest = `(() => {
			window.misprintSearchComplete = false;
			let request;
			const before = event => { if (event.detail.requestConfig?.verb?.toLowerCase() === 'get') { request = event.detail.xhr; document.body.removeEventListener('htmx:beforeRequest', before); } };
			const complete = event => {
				if (event.detail.xhr !== request || (event.type === 'htmx:afterRequest' && event.detail.successful)) return;
				window.misprintSearchComplete = true;
				window.misprintRequestURL = event.detail.xhr.responseURL;
				document.body.removeEventListener('htmx:afterRequest', complete);
				document.body.removeEventListener('htmx:afterSettle', complete);
			};
			document.body.addEventListener('htmx:afterRequest', complete);
			document.body.addEventListener('htmx:afterSettle', complete);
			document.body.addEventListener('htmx:beforeRequest', before);
		})()`

func submitMisprintSearch(ctx context.Context, query, kind string) error {
	return chromedp.Run(ctx,
		chromedp.Evaluate(observeMisprintRequest, nil),
		chromedp.Evaluate(fmt.Sprintf(`(() => {
			const form = document.querySelector('#misprint-search');
			if (!form) throw new Error('server search form is missing');
			form.elements.q.value = %q; form.elements.type.value = %q;
			form.requestSubmit();
		})()`, query, kind), nil),
		chromedp.Poll(`window.misprintSearchComplete`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	)
}

func clickMisprintLink(ctx context.Context, selector string) error {
	return chromedp.Run(ctx,
		chromedp.Evaluate(observeMisprintRequest, nil),
		chromedp.Click(selector, chromedp.ByQuery),
		chromedp.Poll(`window.misprintSearchComplete`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	)
}

func TestMobileOptimizationMisprintSearchAndPagination(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1&view=errors&catalog=1")
	var initialCount int
	var firstPage string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('[data-misprint]').length`, &initialCount), chromedp.Text("#main-content", &firstPage)); err != nil {
		t.Fatal(err)
	}
	if initialCount != 24 || strings.Contains(firstPage, "Archive target report") {
		t.Fatalf("fixture must start with 24 recent reports, without the older target: count=%d", initialCount)
	}
	if err := submitMisprintSearch(ctx, "Archive target", "ink"); err != nil {
		t.Fatal(err)
	}
	var found bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('[data-misprint]').length === 1 && document.querySelector('[data-misprint]').textContent.includes('Archive target report')`, &found)); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("server search did not find the report beyond the initial page")
	}
	if err := submitMisprintSearch(ctx, "Archive", "miscut"); err != nil {
		t.Fatal(err)
	}
	for _, navigation := range []struct {
		name, selector string
		page, count    int
	}{{"next", `a[rel="next"]`, 2, 1}, {"previous", `a[rel="prev"]`, 1, 24}} {
		t.Run(navigation.name, func(t *testing.T) {
			var href string
			if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`document.querySelector(%q)?.getAttribute('href') || ''`, navigation.selector), &href)); err != nil {
				t.Fatal(err)
			}
			if href == "" {
				t.Fatal("pagination link is missing")
			}
			link, err := url.Parse(href)
			if err != nil {
				t.Fatal(err)
			}
			if link.Query().Get("q") != "Archive" || link.Query().Get("type") != "miscut" {
				t.Fatalf("paging link drops selected filters: %s", href)
			}
			if err := clickMisprintLink(ctx, navigation.selector); err != nil {
				t.Fatal(err)
			}
			if err := chromedp.Run(ctx,
				chromedp.Poll(fmt.Sprintf(`document.querySelectorAll('[data-misprint]').length === %d && document.querySelector('#misprint-search [name=q]').value === 'Archive' && document.querySelector('#misprint-search [name=type]').value === 'miscut'`, navigation.count), nil, chromedp.WithPollingTimeout(3*time.Second)),
			); err != nil {
				t.Fatalf("page %d did not retain filter results: %v", navigation.page, err)
			}
		})
	}
}

func TestMobileOptimizationLocalIconsRenderAsGlyphs(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1")
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.optimizationFontReady = false; document.fonts.load('24px "Material Symbols Outlined"').then(() => { window.optimizationFontReady = true })`, nil),
		chromedp.Poll(`window.optimizationFontReady`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
	var failures []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const failures = [];
		const icons = ['home', 'favorite', 'center_focus_strong', 'style', 'more_horiz', 'warning', 'search', 'check_circle', 'error', 'upload_file', 'flashlight_on', 'flashlight_off'];
		for (const name of icons) {
			const icon = document.createElement('span'); icon.className = 'material-symbols-outlined'; icon.textContent = name;
			icon.style.cssText = 'position:absolute;visibility:hidden;display:inline-block;font-size:24px;line-height:1;white-space:nowrap';
			document.body.append(icon);
			const width = icon.getBoundingClientRect().width;
			if (width < 23 || width > 25) failures.push(name + ' rendered at ' + width + 'px instead of one 24px glyph');
			icon.remove();
		}
		return failures;
	})()`, &failures)); err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		t.Errorf("local icon font ligatures: %v", failures)
	}
}

func TestMobileOptimizationFailedMisprintSearchCanRecover(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1&view=errors&catalog=1")
	if err := submitMisprintSearch(ctx, "fail-request", "all"); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Poll(`document.querySelectorAll('[data-misprint]').length === 24 && [...document.querySelectorAll('[role="alert"]')].some(element => element.getClientRects().length && element.textContent.trim()) && !document.querySelector('#misprint-search button[type="submit"]').disabled`, nil, chromedp.WithPollingTimeout(3*time.Second))); err != nil {
		t.Fatalf("failed search does not preserve results, announce the failure, and allow retry: %v", err)
	}
	if err := submitMisprintSearch(ctx, "Archive target", "ink"); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Poll(`document.querySelectorAll('[data-misprint]').length === 1 && document.querySelector('[data-misprint]').textContent.includes('Archive target report') && ![...document.querySelectorAll('[role="alert"]')].some(element => element.getClientRects().length)`, nil, chromedp.WithPollingTimeout(3*time.Second))); err != nil {
		t.Errorf("corrected search did not recover from the request failure: %v", err)
	}
}

func TestMobileOptimizationColdShellUsesSmallAssets(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=0")
	var paths []string
	if err := chromedp.Run(ctx,
		chromedp.Poll(`performance.getEntriesByType('resource').some(entry => new URL(entry.name).pathname === '/static/fonts/material-symbols-ui.woff2')`, nil, chromedp.WithPollingTimeout(3*time.Second)),
		chromedp.Evaluate(`performance.getEntriesByType('resource').map(entry => new URL(entry.name).pathname)`, &paths),
	); err != nil {
		t.Fatal(err)
	}
	if !containsString(paths, "/static/img/logo-128.webp") {
		t.Errorf("cold shell did not request the small logo: %v", paths)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, ".ttf") || path == "/static/img/logo.png" {
			t.Errorf("cold shell still requests a legacy large asset: %s", path)
		}
	}
}

func TestMobileOptimizationPullRefreshRetainsFilters(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1&view=errors&catalog=1")
	if err := submitMisprintSearch(ctx, "Archive", "miscut"); err != nil {
		t.Fatal(err)
	}
	if err := clickMisprintLink(ctx, `a[rel="next"]`); err != nil {
		t.Fatal(err)
	}
	var startY float64
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(observeMisprintRequest, nil),
		chromedp.Evaluate(`window.gestureAudit = []; for (const type of ['touchstart','touchmove','touchend','touchcancel']) document.querySelector('#main-content').addEventListener(type, event => window.gestureAudit.push({type, y: event.touches[0]?.clientY, top: event.currentTarget.scrollTop}), {passive:true})`, nil),
		chromedp.Evaluate(`document.scrollingElement.scrollTop = 0; document.querySelector('#main-content').scrollTop = 0`, nil),
		chromedp.Evaluate(`Math.max(document.querySelector('#main-content').getBoundingClientRect().top, document.querySelector('.app-header').getBoundingClientRect().bottom) + 24`, &startY),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if err := input.DispatchTouchEvent(input.TouchStart, []*input.TouchPoint{{X: 24, Y: startY}}).Do(ctx); err != nil {
				return err
			}
			if err := input.DispatchTouchEvent(input.TouchMove, []*input.TouchPoint{{X: 24, Y: startY + 120}}).Do(ctx); err != nil {
				return err
			}
			return input.DispatchTouchEvent(input.TouchEnd, nil).Do(ctx)
		}),
		chromedp.Poll(`window.misprintSearchComplete`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		var diagnostic string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({events: window.gestureAudit, main: document.querySelector('#main-content').getBoundingClientRect().toJSON(), indicator: !!document.querySelector('.ptr-indicator'), initialized: vaultInitialized})`, &diagnostic))
		t.Log(diagnostic)
		t.Fatalf("pull gesture did not complete its refresh request: %v", err)
	}
	assertMisprintPageContext(t, ctx)
}

func TestMobileOptimizationPublicationRetainsFilters(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1&view=errors&catalog=1")
	if err := submitMisprintSearch(ctx, "Archive", "miscut"); err != nil {
		t.Fatal(err)
	}
	if err := clickMisprintLink(ctx, `a[rel="next"]`); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`[x-ref="submitErrorTrigger"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#error-card-id"),
		chromedp.Evaluate(`document.querySelector('#error-card-id').value = 'test-card'; document.querySelector('#error-type').value = 'Miscut'; document.querySelector('#error-description').value = 'Synthetic fixture report'`, nil),
		chromedp.Evaluate(observeMisprintRequest, nil),
		chromedp.Click(`[role="dialog"] button[type="submit"]`, chromedp.ByQuery),
		chromedp.Poll(`window.misprintSearchComplete`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatalf("synthetic report publication did not refresh the list: %v", err)
	}
	assertMisprintPageContext(t, ctx)
}

func assertMisprintPageContext(t *testing.T, ctx context.Context) {
	t.Helper()
	var state struct {
		RequestURL string `json:"requestURL"`
		Query      string `json:"query"`
		Kind       string `json:"kind"`
		Count      int    `json:"count"`
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`({requestURL: window.misprintRequestURL, query: document.querySelector('#misprint-query').value, kind: document.querySelector('#misprint-search [name=type]').value, count: document.querySelectorAll('[data-misprint]').length})`, &state)); err != nil {
		t.Fatal(err)
	}
	request, err := url.Parse(state.RequestURL)
	if err != nil {
		t.Fatal(err)
	}
	if request.Query().Get("page") != "2" || request.Query().Get("q") != "Archive" || request.Query().Get("type") != "miscut" || state.Query != "Archive" || state.Kind != "miscut" || state.Count != 1 {
		t.Errorf("refresh lost filtered page 2: %+v", state)
	}
}
