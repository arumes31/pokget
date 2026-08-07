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
	)
	allocatorContext, cancelAllocator := chromedp.NewExecAllocator(t.Context(), options...)
	t.Cleanup(cancelAllocator)
	browserContext, cancelBrowser := chromedp.NewContext(allocatorContext)
	t.Cleanup(cancelBrowser)
	return browserContext
}
