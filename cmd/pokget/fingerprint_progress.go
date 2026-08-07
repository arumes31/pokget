// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"

	"pokget/internal/catalog"
)

const fingerprintProgressTimeout = 5 * time.Second

type fingerprintProgressSource interface {
	ImageProgress(context.Context) (catalog.ImageProgress, error)
}

type fingerprintProgressReporter struct {
	source    fingerprintProgressSource
	logger    *slog.Logger
	now       func() time.Time
	lastAt    time.Time
	lastReady int64
	reported  bool
}

func newFingerprintProgressReporter(source fingerprintProgressSource, logger *slog.Logger) *fingerprintProgressReporter {
	return &fingerprintProgressReporter{source: source, logger: logger, now: time.Now}
}

func (r *fingerprintProgressReporter) Report(ctx context.Context) error {
	if r == nil || r.source == nil {
		return errors.New("fingerprint progress source is required")
	}
	logger := r.logger
	if logger == nil {
		logger = slog.Default()
	}
	queryCtx, cancel := context.WithTimeout(ctx, fingerprintProgressTimeout)
	defer cancel()
	progress, err := r.source.ImageProgress(queryCtx)
	if err != nil {
		return err
	}

	now := r.now()
	remaining := progress.Remaining()
	status := "running"
	if progress.Total == 0 {
		status = "idle"
	} else if remaining == 0 {
		status = "complete"
	}
	attributes := []any{
		"status", status,
		"total_images", progress.Total,
		"ready_images", progress.Ready,
		"remaining_images", remaining,
		"processing_images", progress.Processing,
		"pending_images", progress.Pending,
		"retryable_images", progress.Retryable,
		"failed_images", progress.Failed,
		"unavailable_images", progress.Unavailable,
		"fingerprints", progress.Fingerprints,
		"ready_percent", roundOneDecimal(progress.ReadyPercent()),
	}
	if r.reported {
		generated := max(progress.Ready-r.lastReady, 0)
		attributes = append(attributes, "generated_since_last", generated)
		elapsed := now.Sub(r.lastAt)
		if generated > 0 && elapsed > 0 {
			ratePerMinute := float64(generated) / elapsed.Minutes()
			attributes = append(attributes, "rate_per_minute", roundOneDecimal(ratePerMinute))
			if remaining > 0 {
				eta := time.Duration(float64(time.Minute) * float64(remaining) / ratePerMinute).Round(time.Second)
				attributes = append(attributes, "eta", eta)
			}
		}
	}

	logger.Info("Catalog fingerprint generation progress", attributes...)
	r.lastAt = now
	r.lastReady = progress.Ready
	r.reported = true
	return nil
}

func roundOneDecimal(value float64) float64 {
	return math.Round(value*10) / 10
}
