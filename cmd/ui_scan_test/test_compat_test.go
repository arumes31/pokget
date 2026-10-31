package main

import (
	"context"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

type action = chromedp.Action[chromedp.Void]

func poll(expr string, opts ...chromedp.PollOption) chromedp.Action[chromedp.Void] {
	return chromedp.Poll[chromedp.Void](expr, opts...)
}

func pollInto[T any](expr string, target *T, opts ...chromedp.PollOption) chromedp.Action[chromedp.Void] {
	return func(ctx context.Context, t *chromedp.Target) (chromedp.Void, error) {
		val, err := chromedp.Poll[T](expr, opts...)(ctx, t)
		if err != nil {
			var zero chromedp.Void
			return zero, err
		}
		if target != nil {
			*target = val
		}
		return chromedp.Void{}, nil
	}
}

func evaluate(expr string, opts ...chromedp.EvaluateOption) chromedp.Action[chromedp.Void] {
	return chromedp.Evaluate[chromedp.Void](expr, opts...)
}

func evaluateInto[T any](expr string, target *T, opts ...chromedp.EvaluateOption) chromedp.Action[chromedp.Void] {
	return func(ctx context.Context, t *chromedp.Target) (chromedp.Void, error) {
		val, err := chromedp.Evaluate[T](expr, opts...)(ctx, t)
		if err != nil {
			var zero chromedp.Void
			return zero, err
		}
		if target != nil {
			*target = val
		}
		return chromedp.Void{}, nil
	}
}

func actionFunc(f func(ctx context.Context) error) chromedp.Action[chromedp.Void] {
	return func(ctx context.Context, t *chromedp.Target) (chromedp.Void, error) {
		return chromedp.Void{}, f(ctx)
	}
}

func captureScreenshotInto(target *[]byte) chromedp.Action[chromedp.Void] {
	return func(ctx context.Context, t *chromedp.Target) (chromedp.Void, error) {
		val, err := chromedp.CaptureScreenshot()(ctx, t)
		if err != nil {
			var zero chromedp.Void
			return zero, err
		}
		*target = val
		return chromedp.Void{}, nil
	}
}

func locationInto(target *string) chromedp.Action[chromedp.Void] {
	return func(ctx context.Context, t *chromedp.Target) (chromedp.Void, error) {
		val, err := chromedp.Location()(ctx, t)
		if err != nil {
			var zero chromedp.Void
			return zero, err
		}
		*target = val
		return chromedp.Void{}, nil
	}
}

func textInto(sel string, target *string, opts ...chromedp.QueryOption) chromedp.Action[chromedp.Void] {
	return func(ctx context.Context, t *chromedp.Target) (chromedp.Void, error) {
		val, err := chromedp.Text(chromedp.CSS(sel), opts...)(ctx, t)
		if err != nil {
			var zero chromedp.Void
			return zero, err
		}
		*target = val
		return chromedp.Void{}, nil
	}
}

func setFocusEmulationEnabled(enabled bool) chromedp.Action[chromedp.Void] {
	return func(ctx context.Context, t *chromedp.Target) (chromedp.Void, error) {
		_, err := cdp.Call(ctx, t, emulation.SetFocusEmulationEnabled, emulation.SetFocusEmulationEnabledParams{Enabled: enabled})
		return chromedp.Void{}, err
	}
}

func setEmulatedMedia(features ...*emulation.MediaFeature) chromedp.Action[chromedp.Void] {
	return func(ctx context.Context, t *chromedp.Target) (chromedp.Void, error) {
		_, err := cdp.Call(ctx, t, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Features: features})
		return chromedp.Void{}, err
	}
}

func dispatchTouchEvent(ctx context.Context, typ input.DispatchTouchEventType, points []*input.TouchPoint) error {
	_, err := chromedp.Call(ctx, input.DispatchTouchEvent, input.DispatchTouchEventParams{
		Type:        typ,
		TouchPoints: points,
	})
	return err
}

func getFullAXTree(ctx context.Context) ([]*accessibility.Node, error) {
	res, err := chromedp.Call(ctx, accessibility.GetFullAXTree, accessibility.GetFullAXTreeParams{})
	return res.Nodes, err
}

func emulateNetworkOffline(ctx context.Context, offline bool) error {
	_, err := chromedp.Call(ctx, network.EmulateNetworkConditionsByRule, network.EmulateNetworkConditionsByRuleParams{
		MatchedNetworkConditions: []*network.Conditions{{
			URLPattern:         "",
			Offline:            offline,
			DownloadThroughput: -1,
			UploadThroughput:   -1,
		}},
	})
	return err
}

func setEmulatedMediaFeatures(features []*emulation.MediaFeature) chromedp.Action[chromedp.Void] {
	return func(ctx context.Context, t *chromedp.Target) (chromedp.Void, error) {
		_, err := cdp.Call(ctx, t, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Features: features})
		return chromedp.Void{}, err
	}
}

func pollFunction(expr string, opts ...chromedp.PollOption) chromedp.Action[chromedp.Void] {
	return chromedp.PollFunction[chromedp.Void](expr, opts...)
}

func enableNetwork() chromedp.Action[chromedp.Void] {
	return func(ctx context.Context, t *chromedp.Target) (chromedp.Void, error) {
		_, err := cdp.Call(ctx, t, network.Enable, network.EnableParams{})
		return chromedp.Void{}, err
	}
}

const (
	touchStart = input.DispatchTouchEventTypeTouchStart
	touchMove  = input.DispatchTouchEventTypeTouchMove
	touchEnd   = input.DispatchTouchEventTypeTouchEnd
)
