package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func mobileScannerRegressionPage(t *testing.T) context.Context {
	t.Helper()
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed")
	}
	server := newMobileShellServer(t)
	t.Cleanup(server.Close)
	browser := newHeadlessBrowserContext(t, chromePath)
	ctx, cancel := context.WithTimeout(browser, 25*time.Second)
	t.Cleanup(cancel)
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(server.URL+"/?view=scan"),
		chromedp.WaitVisible(".scanner-controls", chromedp.ByQuery),
		chromedp.Poll(`window.Alpine && Alpine.$data(document.querySelector('.scanner-shell')).$refs.video`, nil),
		chromedp.Poll(`document.body.classList.contains('scanner-active')`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestMobileCropDragPreservesSelectedPhoto(t *testing.T) {
	ctx := mobileScannerRegressionPage(t)
	var point struct{ X, Y float64 }
	var during struct {
		Dragging string  `json:"dragging"`
		Top      float64 `json:"top"`
	}
	var after struct {
		Preview  string  `json:"preview"`
		Top      float64 `json:"top"`
		Requests int     `json:"requests"`
		Target   string  `json:"target"`
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => { const state=Alpine.$data(document.querySelector('.scanner-shell')); state.previewURL='/static/img/logo-128.webp'; window.cropRefreshRequests=0; document.body.addEventListener('htmx:beforeRequest', e=>{if(e.detail.requestConfig.path==='/centering') window.cropRefreshRequests++}); document.body.addEventListener('touchstart',e=>window.cropTouchTarget=e.target.closest('[aria-label]')?.getAttribute('aria-label'),{once:true}); })()`, nil),
		chromedp.Poll(`document.querySelector('[aria-label="Top crop guide"]').getClientRects().length>0`, nil),
		chromedp.Evaluate(`(() => {const guide=document.querySelector('[aria-label="Top crop guide"]').getBoundingClientRect(); const x=guide.right-24; const y=[guide.top+guide.height/2,guide.bottom-2].find(y=>document.elementFromPoint(x,y)?.closest('[role="slider"]')?.getAttribute('aria-label')==='Top crop guide'); if(y===undefined) throw new Error('Top crop guide is covered'); return {X:x,Y:y}})()`, &point),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if err := input.DispatchTouchEvent(input.TouchStart, []*input.TouchPoint{{X: point.X, Y: point.Y}}).Do(ctx); err != nil {
				return err
			}
			return input.DispatchTouchEvent(input.TouchMove, []*input.TouchPoint{{X: point.X, Y: point.Y + 120}}).Do(ctx)
		}),
		chromedp.Evaluate(`({dragging:Alpine.$data(document.querySelector('.scanner-shell')).dragging,top:Alpine.$data(document.querySelector('.scanner-shell')).lines.top})`, &during),
		chromedp.ActionFunc(func(ctx context.Context) error { return input.DispatchTouchEvent(input.TouchEnd, nil).Do(ctx) }),
		// A local unintended refresh finishes well within this observation window.
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`({preview:Alpine.$data(document.querySelector('.scanner-shell')).previewURL,top:Alpine.$data(document.querySelector('.scanner-shell')).lines.top,requests:window.cropRefreshRequests,target:window.cropTouchTarget})`, &after),
	); err != nil {
		t.Fatal(err)
	}
	if after.Target != "Top crop guide" || during.Dragging != "top" || during.Top <= 10 {
		t.Fatalf("fixture did not perform an actual crop adjustment: during=%+v after=%+v", during, after)
	}
	if after.Requests != 0 || after.Preview == "" || after.Top != during.Top {
		t.Errorf("crop adjustment refreshed the scanner or lost the selected photo: during=%+v after=%+v", during, after)
	}
}

func TestMobileFailedScanNavigationRemainsRecoverable(t *testing.T) {
	for _, failure := range []string{"offline", "http-500"} {
		t.Run(failure, func(t *testing.T) {
			ctx := mobileHardeningPage(t, "/?filled=1")
			setOffline := func(offline bool) chromedp.Action {
				return chromedp.ActionFunc(func(ctx context.Context) error {
					_, err := network.EmulateNetworkConditionsByRule([]*network.Conditions{{URLPattern: "", Offline: offline, DownloadThroughput: -1, UploadThroughput: -1}}).Do(ctx)
					return err
				})
			}
			if failure == "offline" {
				if err := chromedp.Run(ctx, setOffline(true)); err != nil {
					t.Fatal(err)
				}
			} else if err := chromedp.Run(ctx, chromedp.Evaluate(`document.cookie='ui-fixture-navigation-fail=1; path=/'`, nil)); err != nil {
				t.Fatal(err)
			}
			var state struct{ Header, Nav, OldPage, Alert bool }
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`window.scanNavigationFailed=false; for(const event of ['htmx:sendError','htmx:responseError']) document.body.addEventListener(event,()=>window.scanNavigationFailed=true,{once:true})`, nil),
				chromedp.Click(`.app-bottom-nav [hx-get="/centering"]`, chromedp.ByQuery),
				chromedp.Poll(`window.scanNavigationFailed`, nil, chromedp.WithPollingTimeout(3*time.Second)),
				chromedp.Poll(`[...document.querySelectorAll('[role=alert]')].some(e=>e.getClientRects().length&&e.textContent.trim())`, nil, chromedp.WithPollingTimeout(2*time.Second)),
				chromedp.Evaluate(`(() => {const visible=e=>e&&e.getClientRects().length>0&&getComputedStyle(e).visibility!=='hidden';return {Header:visible(document.querySelector('.app-header')),Nav:visible(document.querySelector('.app-bottom-nav')),OldPage:!!document.querySelector('#main-content .app-page')&&!document.querySelector('.scanner-shell'),Alert:[...document.querySelectorAll('[role=alert]')].some(e=>visible(e)&&e.textContent.trim())}})()`, &state),
			); err != nil {
				t.Fatal(err)
			}
			if !state.Header || !state.Nav || !state.OldPage || !state.Alert {
				t.Fatalf("failed Scan request removed navigation or omitted error feedback: %+v", state)
			}
			retrySelector := `.app-bottom-nav [hx-get="/centering"]`
			if failure == "http-500" {
				retrySelector = `[data-testid="navigation-retry"]`
			}
			if err := chromedp.Run(ctx, setOffline(false),
				chromedp.Evaluate(`document.cookie='ui-fixture-navigation-fail=0; path=/'`, nil),
				chromedp.Click(retrySelector, chromedp.ByQuery),
				chromedp.WaitVisible(".scanner-controls", chromedp.ByQuery),
				chromedp.Poll(`new URLSearchParams(location.search).get('view')==='scan'`, nil, chromedp.WithPollingTimeout(2*time.Second)),
			); err != nil {
				t.Fatalf("Scan did not recover after connection/service returned: %v", err)
			}
		})
	}
}

func TestMobileEnlargedLandscapePreviewControlsStayInsidePane(t *testing.T) {
	ctx := mobileScannerRegressionPage(t)
	var buttons []struct {
		Label                                             string
		Left, Right, FrameLeft, FrameRight, Width, Height float64
		Collisions                                        []string
	}
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(568, 320),
		chromedp.Evaluate(`document.documentElement.style.setProperty('font-size','32px','important'); Alpine.$data(document.querySelector('.scanner-shell')).previewURL='/static/img/logo-128.webp'`, nil),
		chromedp.Poll(`document.querySelector('.scanner-frame button').getClientRects().length>0`, nil),
		chromedp.Evaluate(`(() => {const frame=document.querySelector('.scanner-frame').getBoundingClientRect();const notes=[...document.querySelectorAll('.scanner-frame [data-testid="scanner-privacy-note"],.scanner-frame [x-text="metrics.lr"],.scanner-frame [x-text="metrics.tb"]')].filter(e=>e.getClientRects().length);return [...document.querySelectorAll('.scanner-frame button')].filter(e=>e.getClientRects().length&&e.getAttribute('role')!=='slider').map(e=>{const box=e.getBoundingClientRect();const collisions=notes.filter(e=>{const note=e.getBoundingClientRect();return Math.min(box.right,note.right)>Math.max(box.left,note.left)&&Math.min(box.bottom,note.bottom)>Math.max(box.top,note.top)}).map(e=>e.textContent.trim());return {Label:e.textContent.trim(),Left:box.left,Right:box.right,FrameLeft:frame.left,FrameRight:frame.right,Width:box.width,Height:box.height,Collisions:collisions}})})()`, &buttons),
	); err != nil {
		t.Fatal(err)
	}
	if len(buttons) != 2 {
		t.Fatalf("expected both photo-preview controls, got%+v", buttons)
	}
	for _, button := range buttons {
		if button.Left < button.FrameLeft-1 || button.Right > button.FrameRight+1 || button.Width < 44 || button.Height < 44 {
			t.Errorf("preview control is clipped or lacks44px touch size at200%% base text: %+v", button)
		}
		if len(button.Collisions) != 0 {
			t.Errorf("preview button text overlaps informational overlays: %+v", button)
		}
	}
	if directory := os.Getenv("POKGET_MOBILE_SCREENSHOT_DIR"); directory != "" {
		var screenshot []byte
		if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&screenshot)); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "scanner-landscape-text200.png"), screenshot, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMobileEditorRefreshesMetadataBetweenOpens(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1")
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.editorRuntimeErrors=[]; window.addEventListener('error', e=>window.editorRuntimeErrors.push(e.message))`, nil),
		chromedp.Evaluate(`document.querySelector('#main-content button[aria-label^="Edit "]').click()`, nil),
		chromedp.Poll(`document.querySelector('#edit-modal').getClientRects().length>0`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('#edit-modal button')].find(e=>e.textContent.trim()==='Cancel').click()`, nil),
		chromedp.Poll(`!document.querySelector('#edit-modal').getClientRects().length`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Poll(`document.activeElement===document.querySelector('#main-content button[aria-label^="Edit "]')`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		// Simulate a binder created and a currency changed while the shell persists.
		chromedp.Evaluate(`document.cookie='ui-fixture-editor=updated; path=/'`, nil),
		chromedp.Evaluate(`document.querySelector('#main-content button[aria-label^="Edit "]').click()`, nil),
		chromedp.Poll(`document.querySelector('#edit-binder option[value="new"]') && document.querySelector('label[for="edit-custom-price"]').textContent.includes('$')`, nil, chromedp.WithPollingTimeout(2*time.Second)),
	); err != nil {
		var diagnostic string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({modal:Alpine.$data(document.body).showEditModal, active:document.activeElement?.outerHTML,options:[...document.querySelector('#edit-binder').options].map(o=>({value:o.value,label:o.text})),currency:document.querySelector('label[for="edit-custom-price"]').textContent})`, &diagnostic))
		t.Fatalf("reopened editor did not fetch current binders and currency: %v; %s", err, diagnostic)
	}
	var state struct{ Name, Focus, Selection bool }
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.activeElement===document.querySelector('#edit-custom-price')`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Evaluate(`(() => {const dialog=document.querySelector('#edit-modal'); const label=document.getElementById(dialog.getAttribute('aria-labelledby')); return {Name:!!label?.textContent.trim(),Focus:dialog.contains(document.activeElement),Selection:document.querySelector('#edit-binder').value==='test'}})()`, &state),
	); err != nil {
		var diagnostic string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({modalVisible:!!document.querySelector('#edit-modal').getClientRects().length,inputVisible:!!document.querySelector('#edit-custom-price').getClientRects().length,show:Alpine.$data(document.body).showEditModal,errors:window.editorRuntimeErrors,active:document.activeElement?.outerHTML,selection:document.querySelector('#edit-binder').value,model:Alpine.$data(document.body).editBinderId,ready:Alpine.$data(document.body).editReady})`, &diagnostic))
		t.Fatalf("editor did not restore its initial focus: %v; %s", err, diagnostic)
	}
	if !state.Name || !state.Focus || !state.Selection {
		t.Errorf("refreshed editor lost accessible name, focus, or current binder: %+v", state)
	}
}

func TestMobileEditorMetadataFailureCanRetry(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1")
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.cookie='ui-fixture-editor=fail; path=/'`, nil),
		chromedp.Evaluate(`document.querySelector('#main-content button[aria-label^="Edit "]').click()`, nil),
		chromedp.Poll(`[...document.querySelectorAll('#edit-modal [role=alert]')].some(e=>e.getClientRects().length&&e.textContent.trim()) && document.querySelector('[data-testid="edit-metadata-retry"]')?.getClientRects().length`, nil, chromedp.WithPollingTimeout(2*time.Second)),
	); err != nil {
		t.Fatalf("metadata failure did not show an actionable error: %v", err)
	}
	var disabled bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll('#edit-modal button[type=submit]')].every(e=>e.disabled)`, &disabled)); err != nil {
		t.Fatal(err)
	}
	if !disabled {
		t.Error("metadata-dependent edits remain enabled with unknown currency and binders")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.cookie='ui-fixture-editor=updated; path=/'`, nil),
		chromedp.Click(`[data-testid="edit-metadata-retry"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('#edit-binder option[value="new"]') && document.querySelector('label[for="edit-custom-price"]').textContent.includes('$') && [...document.querySelectorAll('#edit-modal button[type=submit]')].every(e=>!e.disabled)`, nil, chromedp.WithPollingTimeout(2*time.Second)),
	); err != nil {
		t.Fatalf("metadata retry did not restore editable current data: %v", err)
	}
}

func TestMobileEditorSaveRefreshCannotReplaceNewerNavigation(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1")
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('#main-content button[aria-label^="Edit "]').click()`, nil),
		chromedp.Poll(`document.querySelector('#edit-binder option[value="test"]') && !document.querySelector('#edit-form button[type=submit]').disabled`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Evaluate(`document.cookie='ui-fixture-delay-dashboard=1; path=/'; window.editorRefreshStarted=false; document.body.addEventListener('htmx:beforeRequest',e=>{if(e.detail.requestConfig.path==='/dashboard') window.editorRefreshStarted=true})`, nil),
		chromedp.Click(`#edit-form button[type=submit]`, chromedp.ByQuery),
		chromedp.Poll(`window.editorRefreshStarted`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Click(`.app-bottom-nav [hx-get="/wantlist"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('#main-content .page-title')?.textContent.trim()==='Grails'`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Sleep(700*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	var title string
	if err := chromedp.Run(ctx, chromedp.Text(`#main-content .page-title`, &title, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if title != "Grails" {
		t.Errorf("delayed editor refresh replaced the user's newer navigation: %q", title)
	}
}
