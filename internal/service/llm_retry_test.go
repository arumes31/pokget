package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestPrimaryRetryWaitsForCompletionAndRetriesThreeTimes(t *testing.T) {
	for _, succeeds := range []bool{true, false} {
		t.Run(map[bool]string{true: "third attempt succeeds", false: "exhaustion uses fallback"}[succeeds], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var primaryCalls, fallbackCalls int
				var previousEnd time.Time
				client := &http.Client{Timeout: time.Second, Transport: primarySecurityTransport(func(r *http.Request) (*http.Response, error) {
					status, body := http.StatusOK, `{"response":"fallback"}`
					if r.URL.Host == "primary.invalid" {
						primaryCalls++
						if !previousEnd.IsZero() && time.Since(previousEnd) != 5*time.Second {
							t.Errorf("retry pause = %s, want 5s", time.Since(previousEnd))
						}
						// A live completion may take longer than the former provider,
						// total request, and shared HTTP-client time limits.
						select {
						case <-time.After(2 * time.Minute):
						case <-r.Context().Done():
							return nil, r.Context().Err()
						}
						previousEnd = time.Now()
						status, body = http.StatusServiceUnavailable, "unavailable"
						if succeeds && primaryCalls == 3 {
							status, body = http.StatusOK, `{"choices":[{"message":{"content":"primary"}}]}`
						}
					} else {
						fallbackCalls++
					}
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})}
				service := primarySecurityService(t, "https://primary.invalid/v1", "http://fallback.invalid:11434", client)
				result, err := service.queryLLMContext(context.Background(), "select")
				want, wantFallback := "fallback", 1
				if succeeds {
					want, wantFallback = "primary", 0
				}
				if err != nil || result != want || primaryCalls != 3 || fallbackCalls != wantFallback {
					t.Fatalf("result=%q err=%v calls=%d/%d", result, err, primaryCalls, fallbackCalls)
				}
				if client.Timeout != time.Second {
					t.Fatal("shared client was mutated")
				}
			})
		})
	}
}

func TestPrimaryRetryPauseHonorsCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		client := &http.Client{Transport: primarySecurityTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("failed")
		})}
		service := primarySecurityService(t, "https://primary.invalid/v1", "http://fallback.invalid:11434", client)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { time.Sleep(time.Second); cancel() }()
		_, err := service.queryLLMContext(ctx, "select")
		if !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatalf("err=%v calls=%d", err, calls)
		}
	})
}
