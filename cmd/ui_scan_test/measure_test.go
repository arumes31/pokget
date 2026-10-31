package main

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestMeasureTouchPanPreservesUnsavedPhoto(t *testing.T) {
	ctx := mobileScannerRegressionPage(t)
	var point struct{ X, Y float64 }
	var after struct {
		Pan      float64
		Requests int
		Photo    bool
	}
	if err := chromedp.Do(ctx,
		chromedp.Click(chromedp.CSS(`[data-testid="open-measure"]`)),
		evaluate(`(() => { const s=Alpine.$data(document.querySelector('.measure-shell')); Object.assign(s.current,{image:'/static/img/logo-128.webp',width:128,height:128}); s.setZoom(2); window.measureRefreshRequests=0; document.body.addEventListener('htmx:beforeRequest', e=>{if(e.detail.requestConfig.path==='/centering')window.measureRefreshRequests++}); })()`),
		poll(`document.querySelector('.measure-stage').getClientRects().length > 0`),
		evaluateInto(`(() => { const r=document.querySelector('.measure-viewport').getBoundingClientRect();return {X:r.left+10,Y:r.top+60}; })()`, &point),
		actionFunc(func(ctx context.Context) error {
			if err := dispatchTouchEvent(ctx, touchStart, []*input.TouchPoint{{X: point.X, Y: point.Y}}); err != nil {
				return err
			}
			if err := dispatchTouchEvent(ctx, touchMove, []*input.TouchPoint{{X: point.X, Y: point.Y + 120}}); err != nil {
				return err
			}
			return dispatchTouchEvent(ctx, touchEnd, nil)
		}),
		chromedp.Sleep(500*time.Millisecond),
		evaluateInto(`(() => {const s=Alpine.$data(document.querySelector('.measure-shell'));return {Pan:s.panY,Requests:window.measureRefreshRequests,Photo:!!s.current.image};})()`, &after),
	); err != nil {
		t.Fatal(err)
	}
	if after.Requests != 0 || !after.Photo || after.Pan < 100 {
		t.Fatalf("touch pan lost the photo or refreshed the app: %+v", after)
	}
}

func TestMeasurePhotoGuidesPersistenceAndPerspective(t *testing.T) {
	chromePath := mobileTestChromePath()
	if chromePath == "" {
		t.Skip("Chrome or Edge is not installed")
	}
	server := newScannerProgressServer(t)
	defer server.Close()
	browser := newHeadlessBrowserContext(t, chromePath)
	tab, cancelTab := chromedp.NewContext(browser)
	defer cancelTab()
	ctx, cancel := context.WithTimeout(tab, 60*time.Second)
	defer cancel()
	fixture := filepath.Join(t.TempDir(), "card.png")
	card := image.NewRGBA(image.Rect(0, 0, 200, 280))
	for y := range 280 {
		for x := range 200 {
			pixel := color.RGBA{20, 20, 20, 255}
			if x >= 20 && x < 180 && y >= 20 && y < 260 {
				pixel = color.RGBA{240, 240, 240, 255}
			}
			if x >= 30 && x < 164 && y >= 35 && y < 240 {
				pixel = color.RGBA{70, 70, 70, 255}
			}
			card.SetRGBA(x, y, pixel)
		}
	}
	file, err := os.Create(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, card); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	run := func(actions ...action) {
		t.Helper()
		if err := chromedp.Do(ctx, actions...); err != nil {
			t.Fatal(err)
		}
	}
	assertJS := func(expression, message string) {
		t.Helper()
		var ok bool
		run(evaluateInto(`(() => { const s = Alpine.$data(document.querySelector('.measure-shell')); return (`+expression+`); })()`, &ok))
		if !ok {
			t.Fatal(message)
		}
	}
	const state = `Alpine.$data(document.querySelector('.measure-shell'))`
	run(chromedp.EmulateViewport(1280, 900), chromedp.Navigate(server.URL),
		chromedp.Click(chromedp.CSS(`[data-testid="open-measure"]`)),
		chromedp.WaitVisible(chromedp.CSS(`.measure-empty`)))
	assertJS(`!s.ready && getComputedStyle(document.querySelector('.measure-stage')).display === 'none'`, "empty state exposes guides or a grade")
	run(chromedp.SetUploadFiles(`[aria-label="Choose measurement photo"]`, []string{fixture}),
		poll(state+`.current.image && !`+state+`.busy`),
		chromedp.Click(chromedp.CSS(`button[\@click="autoDetect()"]`)),
		poll(`!`+state+`.busy && `+state+`.notice.includes('suggestions')`))
	assertJS(`!s.ready && Math.abs(s.metrics.lr - 38.46) < 2`, "border detection disagrees with known fixture geometry")
	run(chromedp.Click(chromedp.CSS(`button[\@click="confirm()"]`)))
	assertJS(`s.ready && s.grades.find(g => g.company === 'PSA').display === '8'`, "confirmed geometry did not update grades")
	run(chromedp.Click(chromedp.CSS(`[data-guide="outer-left"]`)),
		chromedp.KeyEvent(kb.ArrowRight))
	assertJS(`s.current.guides.outer.left > 9.5`, "keyboard adjustment did not move the selected guide")
	run(chromedp.SendKeys(chromedp.CSS(`#measure-name`), "Browser test card"),
		chromedp.SendKeys(chromedp.CSS(`#measure-tags`), "grade, test"),
		chromedp.Click(chromedp.CSS(`button[\@click="save()"]`)),
		poll(state+`.saved.length === 1 && !`+state+`.busy`))
	assertJS(`!s.dirty && s.saved[0].tags === 'grade, test'`, "saved card is missing metadata")
	run(chromedp.Click(chromedp.CSS(`button[\@click="setSide('back')"]`)))
	assertJS(`!s.ready && !s.current.image && s.sides.front.confirmed`, "front and back state leaked")
	run(chromedp.SetUploadFiles(`[aria-label="Choose measurement photo"]`, []string{fixture}),
		poll(state+`.current.image && !`+state+`.busy`),
		chromedp.Click(chromedp.CSS(`button[\@click="confirm()"]`)),
		chromedp.Click(chromedp.CSS(`button[\@click="save()"]`)),
		poll(`!`+state+`.busy`), chromedp.Reload(),
		chromedp.Click(chromedp.CSS(`[data-testid="open-measure"]`)),
		poll(state+`.saved.length === 1`),
		chromedp.Click(chromedp.CSS(`button[\@click="tab = 'saved'; refreshSaved()"]`)),
		chromedp.Click(chromedp.CSS(`button[\@click="openSaved(record)"]`)))
	assertJS(`s.sides.front.confirmed && s.sides.back.confirmed && s.title === 'Browser test card'`, "saved images or guides were lost on reload")
	run(evaluate(`window.__measureDownloads = []; window.__measureCreateURL = URL.createObjectURL; window.__measureAnchorClick = HTMLAnchorElement.prototype.click; URL.createObjectURL = function(blob) { window.__measureDownloads.push(blob); return window.__measureCreateURL(blob); }; HTMLAnchorElement.prototype.click = function() {};`),
		chromedp.Click(chromedp.CSS(`button[\@click="exportPNG()"]`)),
		poll(`!`+state+`.busy && window.__measureDownloads.length === 1`),
		chromedp.Click(chromedp.CSS(`button[\@click="exportJSON()"]`)),
		evaluate(`Promise.all([createImageBitmap(window.__measureDownloads[0]), window.__measureDownloads[1].text()]).then(([image, json]) => { const data = JSON.parse(json); window.__measureExportValid = image.width === 1000 && image.height === 1200 && data.records[0].title === 'Browser test card' && data.records[0].sides.back.confirmed; image.close(); });`),
		poll(`window.__measureExportValid === true`),
		evaluate(`URL.createObjectURL = window.__measureCreateURL; HTMLAnchorElement.prototype.click = window.__measureAnchorClick; window.__measureBeforeCrop = {...`+state+`.metrics};`))
	run(chromedp.Click(chromedp.CSS(`details.measure-block summary`)),
		chromedp.Click(chromedp.CSS(`button[\@click="cropToEdges()"]`)),
		poll(`!`+state+`.busy`))
	assertJS(`s.ready && s.current.width < 200 && Math.abs(s.metrics.lr - window.__measureBeforeCrop.lr) < 1e-8 && Math.abs(s.metrics.tb - window.__measureBeforeCrop.tb) < 1e-8`, "crop changed the measured border ratios")
	run(
		chromedp.Click(chromedp.CSS(`button[\@click="startPerspective()"]`)))
	assertJS(`s.perspective && !s.ready && [...document.querySelectorAll('.measure-guide')].every(e => getComputedStyle(e).display === 'none')`, "perspective mode exposes stale grades or guides")
	run(chromedp.Click(chromedp.CSS(`button[\@click="applyPerspective()"]`)),
		poll(`!`+state+`.busy && !`+state+`.perspective`))
	assertJS(`!s.error && !s.ready && s.current.guides.outer.left === 0 && s.current.guides.outer.right === 100`, "perspective correction failed to reset measurement geometry")
	for _, viewport := range []struct{ width, height int64 }{{320, 568}, {390, 844}, {844, 390}, {1280, 900}} {
		run(chromedp.EmulateViewport(viewport.width, viewport.height))
		assertJS(`document.documentElement.scrollWidth <= innerWidth + 1`, "measure workspace overflows the viewport")
		assertJS(`[...document.querySelectorAll('.measure-shell button:not(.measure-guide):not(.measure-corner), .measure-shell summary')].filter(e => e.getClientRects().length).every(e => {const r=e.getBoundingClientRect();return r.width >= 44 && r.height >= 44;})`, "measure controls do not meet the 44px touch target")
	}
}
