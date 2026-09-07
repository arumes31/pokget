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
	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-testid="open-measure"]`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => { const s=Alpine.$data(document.querySelector('.measure-shell')); Object.assign(s.current,{image:'/static/img/logo-128.webp',width:128,height:128}); s.setZoom(2); window.measureRefreshRequests=0; document.body.addEventListener('htmx:beforeRequest', e=>{if(e.detail.requestConfig.path==='/centering')window.measureRefreshRequests++}); })()`, nil),
		chromedp.Poll(`document.querySelector('.measure-stage').getClientRects().length > 0`, nil),
		chromedp.Evaluate(`(() => { const r=document.querySelector('.measure-viewport').getBoundingClientRect();return {X:r.left+10,Y:r.top+60}; })()`, &point),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if err := input.DispatchTouchEvent(input.TouchStart, []*input.TouchPoint{{X: point.X, Y: point.Y}}).Do(ctx); err != nil {
				return err
			}
			if err := input.DispatchTouchEvent(input.TouchMove, []*input.TouchPoint{{X: point.X, Y: point.Y + 120}}).Do(ctx); err != nil {
				return err
			}
			return input.DispatchTouchEvent(input.TouchEnd, nil).Do(ctx)
		}),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`(() => {const s=Alpine.$data(document.querySelector('.measure-shell'));return {Pan:s.panY,Requests:window.measureRefreshRequests,Photo:!!s.current.image};})()`, &after),
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

	run := func(actions ...chromedp.Action) {
		t.Helper()
		if err := chromedp.Run(ctx, actions...); err != nil {
			t.Fatal(err)
		}
	}
	assertJS := func(expression, message string) {
		t.Helper()
		var ok bool
		run(chromedp.Evaluate(`(() => { const s = Alpine.$data(document.querySelector('.measure-shell')); return (`+expression+`); })()`, &ok))
		if !ok {
			t.Fatal(message)
		}
	}
	const state = `Alpine.$data(document.querySelector('.measure-shell'))`
	run(chromedp.EmulateViewport(1280, 900), chromedp.Navigate(server.URL),
		chromedp.Click(`[data-testid="open-measure"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.measure-empty`, chromedp.ByQuery))
	assertJS(`!s.ready && getComputedStyle(document.querySelector('.measure-stage')).display === 'none'`, "empty state exposes guides or a grade")
	run(chromedp.SetUploadFiles(`[aria-label="Choose measurement photo"]`, []string{fixture}, chromedp.ByQuery),
		chromedp.Poll(state+`.current.image && !`+state+`.busy`, nil),
		chromedp.Click(`button[\@click="autoDetect()"]`, chromedp.ByQuery),
		chromedp.Poll(`!`+state+`.busy && `+state+`.notice.includes('suggestions')`, nil))
	assertJS(`!s.ready && Math.abs(s.metrics.lr - 38.46) < 2`, "border detection disagrees with known fixture geometry")
	run(chromedp.Click(`button[\@click="confirm()"]`, chromedp.ByQuery))
	assertJS(`s.ready && s.grades.find(g => g.company === 'PSA').display === '8'`, "confirmed geometry did not update grades")
	run(chromedp.Click(`[data-guide="outer-left"]`, chromedp.ByQuery),
		chromedp.KeyEvent(kb.ArrowRight))
	assertJS(`s.current.guides.outer.left > 9.5`, "keyboard adjustment did not move the selected guide")
	run(chromedp.SendKeys(`#measure-name`, "Browser test card", chromedp.ByQuery),
		chromedp.SendKeys(`#measure-tags`, "grade, test", chromedp.ByQuery),
		chromedp.Click(`button[\@click="save()"]`, chromedp.ByQuery),
		chromedp.Poll(state+`.saved.length === 1 && !`+state+`.busy`, nil))
	assertJS(`!s.dirty && s.saved[0].tags === 'grade, test'`, "saved card is missing metadata")
	run(chromedp.Click(`button[\@click="setSide('back')"]`, chromedp.ByQuery))
	assertJS(`!s.ready && !s.current.image && s.sides.front.confirmed`, "front and back state leaked")
	run(chromedp.SetUploadFiles(`[aria-label="Choose measurement photo"]`, []string{fixture}, chromedp.ByQuery),
		chromedp.Poll(state+`.current.image && !`+state+`.busy`, nil),
		chromedp.Click(`button[\@click="confirm()"]`, chromedp.ByQuery),
		chromedp.Click(`button[\@click="save()"]`, chromedp.ByQuery),
		chromedp.Poll(`!`+state+`.busy`, nil), chromedp.Reload(),
		chromedp.Click(`[data-testid="open-measure"]`, chromedp.ByQuery),
		chromedp.Poll(state+`.saved.length === 1`, nil),
		chromedp.Click(`button[\@click="tab = 'saved'; refreshSaved()"]`, chromedp.ByQuery),
		chromedp.Click(`button[\@click="openSaved(record)"]`, chromedp.ByQuery))
	assertJS(`s.sides.front.confirmed && s.sides.back.confirmed && s.title === 'Browser test card'`, "saved images or guides were lost on reload")
	run(chromedp.Evaluate(`window.__measureDownloads = []; window.__measureCreateURL = URL.createObjectURL; window.__measureAnchorClick = HTMLAnchorElement.prototype.click; URL.createObjectURL = function(blob) { window.__measureDownloads.push(blob); return window.__measureCreateURL(blob); }; HTMLAnchorElement.prototype.click = function() {};`, nil),
		chromedp.Click(`button[\@click="exportPNG()"]`, chromedp.ByQuery),
		chromedp.Poll(`!`+state+`.busy && window.__measureDownloads.length === 1`, nil),
		chromedp.Click(`button[\@click="exportJSON()"]`, chromedp.ByQuery),
		chromedp.Evaluate(`Promise.all([createImageBitmap(window.__measureDownloads[0]), window.__measureDownloads[1].text()]).then(([image, json]) => { const data = JSON.parse(json); window.__measureExportValid = image.width === 1000 && image.height === 1200 && data.records[0].title === 'Browser test card' && data.records[0].sides.back.confirmed; image.close(); });`, nil),
		chromedp.Poll(`window.__measureExportValid === true`, nil),
		chromedp.Evaluate(`URL.createObjectURL = window.__measureCreateURL; HTMLAnchorElement.prototype.click = window.__measureAnchorClick; window.__measureBeforeCrop = {...`+state+`.metrics};`, nil))
	run(chromedp.Click(`details.measure-block summary`, chromedp.ByQuery),
		chromedp.Click(`button[\@click="cropToEdges()"]`, chromedp.ByQuery),
		chromedp.Poll(`!`+state+`.busy`, nil))
	assertJS(`s.ready && s.current.width < 200 && Math.abs(s.metrics.lr - window.__measureBeforeCrop.lr) < 1e-8 && Math.abs(s.metrics.tb - window.__measureBeforeCrop.tb) < 1e-8`, "crop changed the measured border ratios")
	run(
		chromedp.Click(`button[\@click="startPerspective()"]`, chromedp.ByQuery))
	assertJS(`s.perspective && !s.ready && [...document.querySelectorAll('.measure-guide')].every(e => getComputedStyle(e).display === 'none')`, "perspective mode exposes stale grades or guides")
	run(chromedp.Click(`button[\@click="applyPerspective()"]`, chromedp.ByQuery),
		chromedp.Poll(`!`+state+`.busy && !`+state+`.perspective`, nil))
	assertJS(`!s.error && !s.ready && s.current.guides.outer.left === 0 && s.current.guides.outer.right === 100`, "perspective correction failed to reset measurement geometry")
	for _, viewport := range []struct{ width, height int64 }{{320, 568}, {390, 844}, {844, 390}, {1280, 900}} {
		run(chromedp.EmulateViewport(viewport.width, viewport.height))
		assertJS(`document.documentElement.scrollWidth <= innerWidth + 1`, "measure workspace overflows the viewport")
		assertJS(`[...document.querySelectorAll('.measure-shell button:not(.measure-guide):not(.measure-corner), .measure-shell summary')].filter(e => e.getClientRects().length).every(e => {const r=e.getBoundingClientRect();return r.width >= 44 && r.height >= 44;})`, "measure controls do not meet the 44px touch target")
	}
}
