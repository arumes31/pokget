package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// Base-font enlargement also exercises rem-based spacing. This is deliberately
// separate from browser zoom and checks descendants because the shell clips
// horizontal overflow without increasing document.scrollWidth.
func TestMobileAdaptationContentReflows(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed")
	}
	server := newMobileShellServer(t)
	defer server.Close()
	browser := newHeadlessBrowserContext(t, chromePath)
	for _, width := range []int64{320, 390} {
		for _, fontSize := range []int{16, 32} {
			t.Run(fmt.Sprintf("width-%d/font-%d", width, fontSize), func(t *testing.T) {
				ctx, cancel := chromedp.NewContext(browser)
				defer cancel()
				ctx, timeout := context.WithTimeout(ctx, 45*time.Second)
				defer timeout()
				if err := chromedp.Run(ctx,
					chromedp.EmulateViewport(width, 844),
					chromedp.Navigate(server.URL+"/?filled=1"),
					chromedp.WaitVisible("#main-content .app-page"),
					chromedp.Evaluate(fmt.Sprintf(`document.documentElement.style.setProperty('font-size', '%dpx', 'important')`, fontSize), nil),
				); err != nil {
					t.Fatal(err)
				}
				for _, page := range []struct{ name, route string }{
					{"vault", "/dashboard"}, {"grails", "/wantlist"},
					{"binders", "/binders"}, {"binder", "/binders/test"},
					{"misprints", "/errors"}, {"trade", "/trade"}, {"settings", "/settings"},
				} {
					t.Run(page.name, func(t *testing.T) {
						var issues []string
						if err := chromedp.Run(ctx, loadReviewRoute(page.route), chromedp.Evaluate(`(() => {
							const issues = [];
							const elements = document.querySelectorAll('#main-content .app-page *');
							for (const element of elements) {
								if (!element.getClientRects().length || element.closest('.sr-only, .material-symbols-outlined, [inert]')) continue;
								const style = getComputedStyle(element);
								if (style.display === 'none' || style.visibility === 'hidden') continue;
								const hasText = [...element.childNodes].some(node => node.nodeType === Node.TEXT_NODE && node.textContent.trim());
								if (!hasText && !element.matches('input:not([type=hidden]), select, textarea, button')) continue;
								const box = element.getBoundingClientRect();
								if (box.width > 1 && (box.left < -1 || box.right > innerWidth + 1)) {
									const name = (element.getAttribute('aria-label') || element.textContent || element.id).trim().replace(/\s+/g, ' ').slice(0, 50);
									issues.push(element.tagName + ' "' + name + '" spans ' + Math.round(box.left) + '..' + Math.round(box.right) + ' in viewport ' + innerWidth);
								}
							}
							return issues;
						})()`, &issues)); err != nil {
							t.Fatal(err)
						}
						if len(issues) != 0 {
							t.Errorf("content is clipped at %dpx base font: %v", fontSize, issues)
						}
					})
				}
			})
		}
	}
}

func TestMobileAdaptationScannerActionsRemainReachable(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed")
	}
	server := newMobileShellServer(t)
	defer server.Close()
	browser := newHeadlessBrowserContext(t, chromePath)
	for _, viewport := range []struct {
		name          string
		width, height int64
	}{{"portrait", 390, 844}, {"short-landscape", 568, 320}} {
		for _, fontSize := range []int{16, 32} {
			t.Run(fmt.Sprintf("%s/font-%d", viewport.name, fontSize), func(t *testing.T) {
				ctx, cancel := chromedp.NewContext(browser)
				defer cancel()
				ctx, timeout := context.WithTimeout(ctx, 30*time.Second)
				defer timeout()
				if err := chromedp.Run(ctx,
					chromedp.EmulateViewport(viewport.width, viewport.height),
					chromedp.Navigate(server.URL+"/?view=scan"),
					chromedp.WaitVisible(".scanner-controls"),
					chromedp.Poll(`window.Alpine && Alpine.$data(document.querySelector('.scanner-shell')).$refs.video`, nil),
					chromedp.Evaluate(fmt.Sprintf(`document.documentElement.style.setProperty('font-size', '%dpx', 'important')`, fontSize), nil),
				); err != nil {
					t.Fatal(err)
				}
				for _, action := range []struct{ name, selector string }{
					{"capture", `.scanner-controls button[\@click="capture()"]`},
					{"upload", `label[for="scanner-file"]`},
				} {
					t.Run(action.name, func(t *testing.T) {
						var measurement struct {
							VisibleWidth  float64 `json:"visibleWidth"`
							VisibleHeight float64 `json:"visibleHeight"`
							HitTarget     bool    `json:"hitTarget"`
							ControlTop    float64 `json:"controlTop"`
							PaneTop       float64 `json:"paneTop"`
							PaneHeight    float64 `json:"paneHeight"`
							PaneLeft      float64 `json:"paneLeft"`
							PaneRight     float64 `json:"paneRight"`
							ViewportWidth float64 `json:"viewportWidth"`
						}
						if err := chromedp.Run(ctx,
							chromedp.Evaluate(fmt.Sprintf(`(() => {
								const element = document.querySelector(%q);
								// scrollIntoView also scrolls overflow:hidden ancestors, which
								// cannot be scrolled by touch. Move only user-scrollable panes.
								for (let parent = element.parentElement; parent; parent = parent.parentElement) {
									if (!/auto|scroll/.test(getComputedStyle(parent).overflowY)) continue;
									const target = element.getBoundingClientRect(), bounds = parent.getBoundingClientRect();
									parent.scrollTop += target.top + target.height / 2 - bounds.top - parent.clientHeight / 2;
								}
								const box = element.getBoundingClientRect();
								let left = Math.max(0, box.left), right = Math.min(innerWidth, box.right);
								let top = Math.max(0, box.top), bottom = Math.min(innerHeight, box.bottom);
								for (let parent = element.parentElement; parent; parent = parent.parentElement) {
									const style = getComputedStyle(parent), bounds = parent.getBoundingClientRect();
									if (/hidden|clip|auto|scroll/.test(style.overflowX)) { left = Math.max(left, bounds.left); right = Math.min(right, bounds.right); }
									if (/hidden|clip|auto|scroll/.test(style.overflowY)) { top = Math.max(top, bounds.top); bottom = Math.min(bottom, bounds.bottom); }
								}
								const hit = document.elementFromPoint((left + right) / 2, (top + bottom) / 2);
								const pane = document.querySelector('.scanner-controls').getBoundingClientRect();
								return {visibleWidth: Math.max(0, right - left), visibleHeight: Math.max(0, bottom - top), hitTarget: !!hit && element.contains(hit), controlTop: box.top, paneTop: pane.top, paneHeight: pane.height, paneLeft: pane.left, paneRight: pane.right, viewportWidth: innerWidth};
							})()`, action.selector), &measurement),
						); err != nil {
							t.Fatal(err)
						}
						if measurement.VisibleWidth < 44 || measurement.VisibleHeight < 44 || !measurement.HitTarget {
							t.Errorf("%s lacks a reachable 44px touch area after scrolling: %+v", action.name, measurement)
						}
						if measurement.PaneLeft < -1 || measurement.PaneRight > measurement.ViewportWidth+1 {
							t.Errorf("scanner controls extend outside viewport: %+v", measurement)
						}
					})
				}
				var instructionEndReachable bool
				if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
					const pane = document.querySelector('.scanner-empty');
					if (pane.scrollHeight > pane.clientHeight + 1 && !/auto|scroll/.test(getComputedStyle(pane).overflowY)) return false;
					pane.scrollTop = pane.scrollHeight;
					const paragraph = pane.querySelector('p:last-child'), text = paragraph.firstChild;
					const range = document.createRange(); range.setStart(text, Math.max(0, text.length - 12)); range.setEnd(text, text.length);
					const lastLine = [...range.getClientRects()].at(-1), bounds = pane.getBoundingClientRect();
					const hit = document.elementFromPoint((lastLine.left + lastLine.right) / 2, (lastLine.top + lastLine.bottom) / 2);
					return lastLine.left >= bounds.left - 1 && lastLine.right <= bounds.right + 1 && lastLine.top >= bounds.top - 1 && lastLine.bottom <= bounds.bottom + 1 && !!hit && paragraph.contains(hit);
				})()`, &instructionEndReachable)); err != nil {
					t.Fatal(err)
				}
				if !instructionEndReachable {
					t.Error("empty-camera instructions cannot be scrolled to their final line without clipping or occlusion")
				}
			})
		}
	}
}

func TestMobileAdaptationOfflineRetryRemainsReachable(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed")
	}
	server := newMobileShellServer(t)
	defer server.Close()
	browser := newHeadlessBrowserContext(t, chromePath)
	for _, width := range []int64{320, 390} {
		for _, font := range []int{16, 32} {
			t.Run(fmt.Sprintf("width-%d/font-%d", width, font), func(t *testing.T) {
				ctx, cancel := chromedp.NewContext(browser)
				defer cancel()
				ctx, timeout := context.WithTimeout(ctx, 20*time.Second)
				defer timeout()
				if err := chromedp.Run(ctx,
					chromedp.EmulateViewport(width, 844),
					chromedp.Navigate(server.URL+"/static/offline.html"),
					chromedp.WaitVisible("main", chromedp.ByQuery),
					chromedp.Evaluate(fmt.Sprintf(`document.documentElement.style.setProperty('font-size','%dpx','important')`, font), nil),
					chromedp.ActionFunc(func(ctx context.Context) error {
						if err := input.DispatchTouchEvent(input.TouchStart, []*input.TouchPoint{{X: 100, Y: 620}}).Do(ctx); err != nil {
							return err
						}
						if err := input.DispatchTouchEvent(input.TouchMove, []*input.TouchPoint{{X: 100, Y: 220}}).Do(ctx); err != nil {
							return err
						}
						return input.DispatchTouchEvent(input.TouchEnd, nil).Do(ctx)
					}),
					chromedp.Poll(`(() => {const button=document.querySelector('main button'), box=button.getBoundingClientRect(); const hit=document.elementFromPoint((box.left+box.right)/2,(box.top+box.bottom)/2); return box.top >= 0 && box.bottom <= innerHeight && box.left >= 0 && box.right <= innerWidth && box.width >= 44 && box.height >= 44 && !!hit && button.contains(hit)})()`, nil, chromedp.WithPollingTimeout(3*time.Second)),
				); err != nil {
					t.Fatalf("offline Retry is not reachable after page scrolling: %v", err)
				}
			})
		}
	}
}
