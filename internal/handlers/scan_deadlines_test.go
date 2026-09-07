package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pokget/internal/models"
	"pokget/internal/service"
)

func TestExecuteScanPrimaryWaitsForCompletion(t *testing.T) {
	h := &Handler{
		ScanTimeout: 5 * time.Millisecond,
		LLM:         &service.LLMService{PrimaryBaseURL: "https://primary.example/v1"},
		scanProcessor: func(ctx context.Context, _ []byte, _ []models.Card, _ string, _ *service.LLMService) (string, string, []byte, error) {
			if deadline, ok := ctx.Deadline(); ok {
				t.Errorf("primary scan inherited an application deadline: %v", deadline)
			}
			select {
			case <-time.After(20 * time.Millisecond):
				return "readable card", "Unknown Card", nil, nil
			case <-ctx.Done():
				return "", "", nil, ctx.Err()
			}
		},
	}
	recorder := httptest.NewRecorder()
	h.executeScan(recorder, scanRequest(t, testCardPNG(t), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("running primary scan ended early: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestExecuteScanPrimaryHonorsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &Handler{
		LLM: &service.LLMService{PrimaryBaseURL: "https://primary.example/v1"},
		scanProcessor: func(scanCtx context.Context, _ []byte, _ []models.Card, _ string, _ *service.LLMService) (string, string, []byte, error) {
			cancel()
			select {
			case <-scanCtx.Done():
				return "", "", nil, scanCtx.Err()
			case <-time.After(time.Second):
				t.Error("caller cancellation did not reach scan work")
				return "", "", nil, context.DeadlineExceeded
			}
		},
	}
	recorder := httptest.NewRecorder()
	h.executeScan(recorder, scanRequest(t, testCardPNG(t), nil).WithContext(ctx))
	if recorder.Code != http.StatusRequestTimeout {
		t.Fatalf("canceled scan status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestExecuteScanWithoutPrimaryRetainsDeadline(t *testing.T) {
	h := &Handler{
		ScanTimeout: time.Millisecond,
		scanProcessor: func(ctx context.Context, _ []byte, _ []models.Card, _ string, _ *service.LLMService) (string, string, []byte, error) {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("local scan lost its work deadline")
			}
			<-ctx.Done()
			return "", "", nil, ctx.Err()
		},
	}
	recorder := httptest.NewRecorder()
	h.executeScan(recorder, scanRequest(t, testCardPNG(t), nil))
	if recorder.Code != http.StatusRequestTimeout {
		t.Fatalf("local scan status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestExecuteScanPrimaryCapacityWaitRemainsBounded(t *testing.T) {
	for range cap(scanDetectionSlots) {
		scanDetectionSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for range cap(scanDetectionSlots) {
			<-scanDetectionSlots
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	primary := &service.LLMService{PrimaryBaseURL: "https://primary.example/v1"}
	h := &Handler{
		ScanTimeout: 10 * time.Millisecond,
		LLM:         primary,
		Detection:   service.NewDetectionPipeline(nil, primary),
	}
	recorder := httptest.NewRecorder()
	h.executeScan(recorder, scanRequest(t, testCardPNG(t), nil).WithContext(ctx))
	if recorder.Code != http.StatusRequestTimeout {
		t.Fatalf("full detector queue status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if ctx.Err() != nil {
		t.Fatal("queue waited for caller deadline instead of its configured capacity budget")
	}
}
