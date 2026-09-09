package service

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type scanLoggerKey struct{}

// WithScanLogger carries one request's correlation fields through scan stages.
func WithScanLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, scanLoggerKey{}, logger)
}

// ScanLogger falls back to the application logger for non-HTTP callers.
func ScanLogger(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if logger, ok := ctx.Value(scanLoggerKey{}).(*slog.Logger); ok && logger != nil {
			return logger
		}
	}
	return slog.Default()
}

// LogScanStage reports bounded stage metadata, never OCR text or provider output.
func LogScanStage(ctx context.Context, stage string) func(error) {
	started := time.Now()
	logger := ScanLogger(ctx).With("stage", stage)
	logger.Info("Scan stage started")
	return func(err error) {
		outcome := "complete"
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			outcome = "timeout"
		case errors.Is(err, context.Canceled):
			outcome = "cancelled"
		case err != nil:
			outcome = "failed"
		}
		logger.Info("Scan stage finished", "outcome", outcome, "duration_ms", time.Since(started).Milliseconds())
	}
}
