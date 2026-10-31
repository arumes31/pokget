package main

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
)

func newHeadlessBrowserContext(t *testing.T, chromePath string) context.Context {
	t.Helper()

	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options,
		chromedp.ExecPath(chromePath),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-sandbox", true),
		// The rendered assertions compare English copy and labels. Pin the
		// browser language so a non-English desktop locale cannot translate
		// data-i18n attributes away from the expected selectors.
		chromedp.Flag("lang", "en-US"),
	)
	allocatorContext, cancelAllocator := chromedp.NewExecAllocator(t.Context(), options...)
	t.Cleanup(cancelAllocator)
	browserContext, cancelBrowser := chromedp.NewContext(allocatorContext)
	t.Cleanup(cancelBrowser)
	return browserContext
}
