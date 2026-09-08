package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

func TestScannerMissingLanguageKeepsSelectionAndRetry(t *testing.T) {
	ctx := mobileScannerRegressionPage(t)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			const state = Alpine.$data(document.querySelector('.scanner-shell'));
			state.game = 'pokemon'; state.lang = 'deu';
			window.scanRequests = 0;
			window.fetch = async () => {
				window.scanRequests++;
				return new Response('No cards are available for the selected TCG and language', {status: 422});
			};
			state.submitPreparedBlob(new Blob(['image'], {type: 'image/jpeg'}), 'crop.jpg');
		})()`, nil),
		chromedp.Poll(`(() => {
			const state = Alpine.$data(document.querySelector('.scanner-shell'));
			const alert = document.querySelector('[x-text="scanError"]');
			const retry = document.querySelector('button[aria-label="RETRY LAST CROP"]');
			return !state.scanning && state.lang === 'deu' && state.lastScanBlob &&
				window.scanRequests === 1 && alert.getBoundingClientRect().height > 0 &&
				alert.textContent.includes('sync this language') && !retry.disabled;
		})()`, nil),
	); err != nil {
		t.Fatal(err)
	}
}
