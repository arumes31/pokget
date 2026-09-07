package service

import (
	"context"
	"time"
)

type detectionStageTimeoutKey struct{}

// WithDetectionStageTimeout limits local image analysis without placing a
// deadline on the subsequent primary-provider request. Caller cancellation
// still propagates through the returned context.
func WithDetectionStageTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, detectionStageTimeoutKey{}, timeout)
}

func detectionStageTimeoutFromContext(ctx context.Context) time.Duration {
	timeout, _ := ctx.Value(detectionStageTimeoutKey{}).(time.Duration)
	return timeout
}
