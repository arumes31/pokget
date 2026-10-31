package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

func mobileScannerRegressionPage(t *testing.T) context.Context {
	t.Helper()
	return mobileScannerRegressionPageAtViewport(t, 390, 844)
}

func mobileScannerRegressionPageAtViewport(t *testing.T, width, height int64) context.Context {
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
	if err := chromedp.Do(ctx, chromedp.EmulateViewport(width, height),
		chromedp.Navigate(server.URL+"/?view=scan"),
		chromedp.WaitVisible(chromedp.CSS(".scanner-controls")),
		poll(`window.Alpine && Alpine.$data(document.querySelector('.scanner-shell')).$refs.video`),
		poll(`document.body.classList.contains('scanner-active')`, chromedp.WithPollingTimeout(3*time.Second)),
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
	if err := chromedp.Do(ctx,
		evaluate(`(() => { const state=Alpine.$data(document.querySelector('.scanner-shell')); state.previewURL='/static/img/logo-128.webp'; window.cropRefreshRequests=0; document.body.addEventListener('htmx:beforeRequest', e=>{if(e.detail.requestConfig.path==='/centering') window.cropRefreshRequests++}); document.body.addEventListener('touchstart',e=>window.cropTouchTarget=e.target.closest('[aria-label]')?.getAttribute('aria-label'),{once:true}); })()`),
		poll(`document.querySelector('[aria-label="Top crop guide"]').getClientRects().length>0`),
		evaluateInto(`(() => {const guide=document.querySelector('[aria-label="Top crop guide"]').getBoundingClientRect(); const x=guide.right-24; const y=[guide.top+guide.height/2,guide.bottom-2].find(y=>document.elementFromPoint(x,y)?.closest('[role="slider"]')?.getAttribute('aria-label')==='Top crop guide'); if(y===undefined) throw new Error('Top crop guide is covered'); return {X:x,Y:y}})()`, &point),
		actionFunc(func(ctx context.Context) error {
			if err := dispatchTouchEvent(ctx, touchStart, []*input.TouchPoint{{X: point.X, Y: point.Y}}); err != nil {
				return err
			}
			return dispatchTouchEvent(ctx, touchMove, []*input.TouchPoint{{X: point.X, Y: point.Y + 120}})
		}),
		evaluateInto(`({dragging:Alpine.$data(document.querySelector('.scanner-shell')).dragging,top:Alpine.$data(document.querySelector('.scanner-shell')).lines.top})`, &during),
		actionFunc(func(ctx context.Context) error { return dispatchTouchEvent(ctx, touchEnd, nil) }),
		// A local unintended refresh finishes well within this observation window.
		chromedp.Sleep(500*time.Millisecond),
		evaluateInto(`({preview:Alpine.$data(document.querySelector('.scanner-shell')).previewURL,top:Alpine.$data(document.querySelector('.scanner-shell')).lines.top,requests:window.cropRefreshRequests,target:window.cropTouchTarget})`, &after),
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
			setOffline := func(offline bool) action {
				return actionFunc(func(ctx context.Context) error {
					err := emulateNetworkOffline(ctx, offline)
					return err
				})
			}
			if failure == "offline" {
				if err := chromedp.Do(ctx, setOffline(true)); err != nil {
					t.Fatal(err)
				}
			} else if err := chromedp.Do(ctx, evaluate(`document.cookie='ui-fixture-navigation-fail=1; path=/'`)); err != nil {
				t.Fatal(err)
			}
			var state struct{ Header, Nav, OldPage, Alert bool }
			if err := chromedp.Do(ctx,
				evaluate(`window.scanNavigationFailed=false; for(const event of ['htmx:sendError','htmx:responseError']) document.body.addEventListener(event,()=>window.scanNavigationFailed=true,{once:true})`),
				chromedp.Click(chromedp.CSS(`.app-bottom-nav [hx-get="/centering"]`)),
				poll(`window.scanNavigationFailed`, chromedp.WithPollingTimeout(3*time.Second)),
				poll(`[...document.querySelectorAll('[role=alert]')].some(e=>e.getClientRects().length&&e.textContent.trim())`, chromedp.WithPollingTimeout(2*time.Second)),
				evaluateInto(`(() => {const visible=e=>e&&e.getClientRects().length>0&&getComputedStyle(e).visibility!=='hidden';return {Header:visible(document.querySelector('.app-header')),Nav:visible(document.querySelector('.app-bottom-nav')),OldPage:!!document.querySelector('#main-content .app-page')&&!document.querySelector('.scanner-shell'),Alert:[...document.querySelectorAll('[role=alert]')].some(e=>visible(e)&&e.textContent.trim())}})()`, &state),
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
			if err := chromedp.Do(ctx, setOffline(false),
				evaluate(`document.cookie='ui-fixture-navigation-fail=0; path=/'`),
				chromedp.Click(chromedp.CSS(retrySelector)),
				chromedp.WaitVisible(chromedp.CSS(".scanner-controls")),
				poll(`new URLSearchParams(location.search).get('view')==='scan'`, chromedp.WithPollingTimeout(2*time.Second)),
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
	if err := chromedp.Do(ctx,
		chromedp.EmulateViewport(568, 320),
		evaluate(`document.documentElement.style.setProperty('font-size','32px','important'); Alpine.$data(document.querySelector('.scanner-shell')).previewURL='/static/img/logo-128.webp'`),
		poll(`document.querySelector('.scanner-frame button').getClientRects().length>0`),
		evaluateInto(`(() => {const frame=document.querySelector('.scanner-frame').getBoundingClientRect();const notes=[...document.querySelectorAll('.scanner-frame [data-testid="scanner-privacy-note"],.scanner-frame [x-text="metrics.lr"],.scanner-frame [x-text="metrics.tb"]')].filter(e=>e.getClientRects().length);return [...document.querySelectorAll('.scanner-frame button')].filter(e=>e.getClientRects().length&&e.getAttribute('role')!=='slider').map(e=>{const box=e.getBoundingClientRect();const collisions=notes.filter(e=>{const note=e.getBoundingClientRect();return Math.min(box.right,note.right)>Math.max(box.left,note.left)&&Math.min(box.bottom,note.bottom)>Math.max(box.top,note.top)}).map(e=>e.textContent.trim());return {Label:e.textContent.trim(),Left:box.left,Right:box.right,FrameLeft:frame.left,FrameRight:frame.right,Width:box.width,Height:box.height,Collisions:collisions}})})()`, &buttons),
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
		if err := chromedp.Do(ctx, captureScreenshotInto(&screenshot)); err != nil {
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
	if err := chromedp.Do(ctx,
		evaluate(`window.editorRuntimeErrors=[]; window.addEventListener('error', e=>window.editorRuntimeErrors.push(e.message))`),
		evaluate(`document.querySelector('#main-content button[aria-label^="Edit "]').click()`),
		poll(`document.querySelector('#edit-modal').getClientRects().length>0`, chromedp.WithPollingTimeout(2*time.Second)),
		evaluate(`[...document.querySelectorAll('#edit-modal button')].find(e=>e.textContent.trim()==='Cancel').click()`),
		poll(`!document.querySelector('#edit-modal').getClientRects().length`, chromedp.WithPollingTimeout(2*time.Second)),
		poll(`document.activeElement===document.querySelector('#main-content button[aria-label^="Edit "]')`, chromedp.WithPollingTimeout(2*time.Second)),
		// Simulate a binder created and a currency changed while the shell persists.
		evaluate(`document.cookie='ui-fixture-editor=updated; path=/'`),
		evaluate(`document.querySelector('#main-content button[aria-label^="Edit "]').click()`),
		poll(`document.querySelector('#edit-binder option[value="new"]') && document.querySelector('label[for="edit-custom-price"]').textContent.includes('$')`, chromedp.WithPollingTimeout(2*time.Second)),
	); err != nil {
		var diagnostic string
		_ = chromedp.Do(ctx, evaluateInto(`JSON.stringify({modal:Alpine.$data(document.body).showEditModal, active:document.activeElement?.outerHTML,options:[...document.querySelector('#edit-binder').options].map(o=>({value:o.value,label:o.text})),currency:document.querySelector('label[for="edit-custom-price"]').textContent})`, &diagnostic))
		t.Fatalf("reopened editor did not fetch current binders and currency: %v; %s", err, diagnostic)
	}
	var state struct{ Name, Focus, Selection bool }
	if err := chromedp.Do(ctx,
		poll(`document.activeElement===document.querySelector('#edit-custom-price')`, chromedp.WithPollingTimeout(2*time.Second)),
		evaluateInto(`(() => {const dialog=document.querySelector('#edit-modal'); const label=document.getElementById(dialog.getAttribute('aria-labelledby')); return {Name:!!label?.textContent.trim(),Focus:dialog.contains(document.activeElement),Selection:document.querySelector('#edit-binder').value==='test'}})()`, &state),
	); err != nil {
		var diagnostic string
		_ = chromedp.Do(ctx, evaluateInto(`JSON.stringify({modalVisible:!!document.querySelector('#edit-modal').getClientRects().length,inputVisible:!!document.querySelector('#edit-custom-price').getClientRects().length,show:Alpine.$data(document.body).showEditModal,errors:window.editorRuntimeErrors,active:document.activeElement?.outerHTML,selection:document.querySelector('#edit-binder').value,model:Alpine.$data(document.body).editBinderId,ready:Alpine.$data(document.body).editReady})`, &diagnostic))
		t.Fatalf("editor did not restore its initial focus: %v; %s", err, diagnostic)
	}
	if !state.Name || !state.Focus || !state.Selection {
		t.Errorf("refreshed editor lost accessible name, focus, or current binder: %+v", state)
	}
}

func TestMobileEditorMetadataFailureCanRetry(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1")
	if err := chromedp.Do(ctx,
		evaluate(`document.cookie='ui-fixture-editor=fail; path=/'`),
		evaluate(`document.querySelector('#main-content button[aria-label^="Edit "]').click()`),
		poll(`[...document.querySelectorAll('#edit-modal [role=alert]')].some(e=>e.getClientRects().length&&e.textContent.trim()) && document.querySelector('[data-testid="edit-metadata-retry"]')?.getClientRects().length`, chromedp.WithPollingTimeout(2*time.Second)),
	); err != nil {
		t.Fatalf("metadata failure did not show an actionable error: %v", err)
	}
	var disabled bool
	if err := chromedp.Do(ctx, evaluateInto(`[...document.querySelectorAll('#edit-modal button[type=submit]')].every(e=>e.disabled)`, &disabled)); err != nil {
		t.Fatal(err)
	}
	if !disabled {
		t.Error("metadata-dependent edits remain enabled with unknown currency and binders")
	}
	if err := chromedp.Do(ctx,
		evaluate(`document.cookie='ui-fixture-editor=updated; path=/'`),
		chromedp.Click(chromedp.CSS(`[data-testid="edit-metadata-retry"]`)),
		poll(`document.querySelector('#edit-binder option[value="new"]') && document.querySelector('label[for="edit-custom-price"]').textContent.includes('$') && [...document.querySelectorAll('#edit-modal button[type=submit]')].every(e=>!e.disabled)`, chromedp.WithPollingTimeout(2*time.Second)),
	); err != nil {
		t.Fatalf("metadata retry did not restore editable current data: %v", err)
	}
}

func TestMobileEditorSaveRefreshCannotReplaceNewerNavigation(t *testing.T) {
	ctx := mobileHardeningPage(t, "/?filled=1")
	phase := "open editor"
	markPhase := func(name string) action {
		return actionFunc(func(context.Context) error {
			phase = name
			return nil
		})
	}
	if err := chromedp.Do(ctx,
		evaluate(`document.querySelector('#main-content button[aria-label^="Edit "]').click()`),
		poll(`document.querySelector('#edit-binder option[value="test"]') && !document.querySelector('#edit-form button[type=submit]').disabled`, chromedp.WithPollingTimeout(2*time.Second)),
		evaluate(`document.cookie='ui-fixture-delay-dashboard=1; path=/'; window.editorRefreshStarted=false; document.body.addEventListener('htmx:beforeRequest',e=>{if(e.detail.requestConfig.path==='/dashboard') window.editorRefreshStarted=true})`),
		markPhase("click save"),
		chromedp.Click(chromedp.CSS(`#edit-form button[type=submit]`)),
		markPhase("wait refresh request"),
		poll(`window.editorRefreshStarted`, chromedp.WithPollingTimeout(2*time.Second)),
		markPhase("click navigation"),
		evaluate(`document.querySelector(".app-bottom-nav [hx-get=\"/wantlist\"]").click()`),
		markPhase("wait Grails"),
		poll(`document.querySelector('#main-content .page-title')?.textContent.trim()==='Grails'`, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Sleep(700*time.Millisecond),
	); err != nil {
		t.Fatalf("%s: %v", phase, err)
	}
	var title string
	if err := chromedp.Do(ctx, textInto(`#main-content .page-title`, &title)); err != nil {
		t.Fatal(err)
	}
	if title != "Grails" {
		t.Errorf("delayed editor refresh replaced the user's newer navigation: %q", title)
	}
}
