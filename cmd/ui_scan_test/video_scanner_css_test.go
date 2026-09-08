package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chromedp/chromedp"
)

func captureVideoCSS(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	directory := os.Getenv("POKGET_MOBILE_SCREENSHOT_DIR")
	if directory == "" {
		return
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	var screenshot []byte
	if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&screenshot)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name+".png"), screenshot, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestVideoScannerFeedbackLayout(t *testing.T) {
	for _, viewport := range []struct {
		name          string
		width, height int64
		font          int
	}{
		{"phone", 360, 748, 16}, {"large-text", 320, 568, 32}, {"landscape", 844, 390, 16}, {"desktop", 1440, 900, 16},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			ctx := mobileScannerRegressionPage(t)
			var issues []string
			if err := chromedp.Run(ctx,
				chromedp.EmulateViewport(viewport.width, viewport.height),
				chromedp.Evaluate(fmt.Sprintf(`(() => {
					document.documentElement.style.fontSize = '%dpx';
					const state = Alpine.$data(document.querySelector('.scanner-shell'));
					state.lastScanBlob = new Blob(['image']);
					state.handleScanError(new Error('The catalog has no cards for this game and language. Ask the administrator to sync this language, then retry your saved image.'));
				})()`, viewport.font), nil),
				chromedp.Poll(`document.querySelector('[x-text="scanError"]').textContent.includes('catalog') && document.querySelector('.scanner-shell').classList.contains('scanner-has-error')`, nil),
				chromedp.Evaluate(`(() => {
					const issues = [];
					const controls = document.querySelector('.scanner-controls');
					const bounds = controls.getBoundingClientRect();
					if (bounds.bottom > innerHeight + 1) issues.push('control pane extends below viewport: ' + JSON.stringify({bottom:bounds.bottom,viewport:innerHeight,documentTop:scrollY,tools:document.querySelector('.card-tools').getBoundingClientRect().toJSON()}));
					if (document.documentElement.scrollWidth > innerWidth + 1) issues.push('horizontal overflow');
					if (document.querySelectorAll('[x-text="toast.msg"]').length) issues.push('duplicate error toast covers navigation');
					if (getComputedStyle(controls).colorScheme !== 'dark') issues.push('native scanner controls use the light color scheme');
					return issues;
				})()`, &issues),
				chromedp.ScrollIntoView(`button[aria-label="RETRY LAST CROP"]`, chromedp.ByQuery),
				chromedp.Poll(`(() => {const pane=document.querySelector('.scanner-controls').getBoundingClientRect();const retry=document.querySelector('button[aria-label="RETRY LAST CROP"]').getBoundingClientRect();return retry.top>=pane.top && retry.bottom<=innerHeight+1 && retry.height>=44})()`, nil),
			); err != nil {
				t.Fatal(err)
			}
			captureVideoCSS(t, ctx, viewport.name+"-feedback")
			for _, issue := range issues {
				t.Error(issue)
			}
		})
	}
}

func TestVideoScannerSpinnerRemainsCircular(t *testing.T) {
	ctx := mobileScannerRegressionPage(t)
	var issues []string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => { const state = Alpine.$data(document.querySelector('.scanner-shell')); state.setStatus('Uploading the crop and running detection…', 2); state.setScanning(true); })()`, nil),
		chromedp.WaitVisible(".scan-progress-overlay"),
		chromedp.Evaluate(`(() => {
			const ring = document.querySelector('.scan-progress-orbit');
			const animation = ring.getAnimations()[0];
			if (animation) { animation.pause(); animation.currentTime = animation.effect.getTiming().duration / 8; }
			const style = getComputedStyle(ring);
			const issues = [];
			if (style.borderTopLeftRadius !== '50%') issues.push('spinner is a rotating rounded square');
			if (getComputedStyle(document.querySelector('.scan-progress-mark')).flexShrink !== '0') issues.push('spinner can squash in a short dialog');
			return issues;
		})()`, &issues),
	); err != nil {
		t.Fatal(err)
	}
	captureVideoCSS(t, ctx, "phone-progress")
	for _, issue := range issues {
		t.Error(issue)
	}
}

func TestVideoScannerShowsPreparedCropWhileWaiting(t *testing.T) {
	ctx := mobileScannerRegressionPage(t)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			const canvas = document.createElement('canvas'); canvas.width = 120; canvas.height = 180;
			const paint = canvas.getContext('2d');
			paint.fillStyle = '#eee2b5'; paint.fillRect(0, 0, 120, 180);
			paint.fillStyle = '#315c56'; paint.fillRect(8, 28, 104, 95);
			paint.fillStyle = '#222'; paint.font = '14px sans-serif'; paint.fillText('Test card', 10, 20);
			canvas.toBlob(blob => {
				window.fetch = (_url, options) => new Promise(resolve => options.signal.addEventListener('abort', () => resolve(new Response('{}'))));
				Alpine.$data(document.querySelector('.scanner-shell')).submitPreparedBlob(blob, 'crop.png');
			}, 'image/png');
		})()`, nil),
		chromedp.Poll(`(() => {const image=document.querySelector('[data-testid="scan-progress-preview"]');return image && image.complete && image.naturalWidth === 120 && image.getBoundingClientRect().height > 0})()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	captureVideoCSS(t, ctx, "phone-card-progress")
	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-testid="scan-progress-cancel"]`, chromedp.ByQuery),
		chromedp.Poll(`(() => {const state=Alpine.$data(document.querySelector('.scanner-shell'));return !state.scanning && !state.scanPreviewURL && !!state.lastScanBlob})()`, nil),
	); err != nil {
		t.Fatal(err)
	}
}
