package service

import (
	"context"
	"testing"
	"time"
)

func TestDetectionStageTimeoutPreservesCallerContext(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := WithDetectionStageTimeout(parent, 75*time.Second)
	if got := detectionStageTimeoutFromContext(ctx); got != 75*time.Second {
		t.Fatalf("stage timeout=%v", got)
	}
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("local stage budget imposed a primary request deadline")
	}
	cancel()
	if ctx.Err() != context.Canceled {
		t.Fatalf("caller cancellation=%v", ctx.Err())
	}
	if got := detectionStageTimeoutFromContext(context.Background()); got != 0 {
		t.Fatalf("unexpected default stage timeout=%v", got)
	}
}
