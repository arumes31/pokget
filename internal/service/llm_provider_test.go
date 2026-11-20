package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"pokget/internal/models"
)

var binderTestCards = []models.Card{{ID: "pikachu", Name: "Pikachu"}}

func configurePrimaryLLMTest(t *testing.T, primary, fallback *httptest.Server) *LLMService {
	t.Helper()
	t.Setenv("LLM_BASE_URL", primary.URL+"/v1")
	t.Setenv("LLM_MODEL", "primary-model")
	t.Setenv("LLM_API", "test-key-do-not-log")
	t.Setenv("OLLAMA_HOST", fallback.URL)
	t.Setenv("OLLAMA_MODEL", "fallback-model")
	return NewLLMService()
}

func TestLLMPrimaryCancellationDoesNotInvokeFallback(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	primary := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer primary.Close()
	defer close(release)
	var fallbackCalls atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { fallbackCalls.Add(1) }))
	defer fallback.Close()
	service := configurePrimaryLLMTest(t, primary, fallback)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.GenerateBinderNameContext(ctx, binderTestCards)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("primary did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled request did not return")
	}
	if fallbackCalls.Load() != 0 {
		t.Error("cancellation triggered fallback")
	}
}

func writePrimaryLLMResponse(w http.ResponseWriter, content string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
}

func TestLLMPrimaryProviderRunsBeforeOllama(t *testing.T) {
	var primaryCalls, fallbackCalls atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls.Add(1)
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key-do-not-log" {
			t.Error("primary request used the wrong path or authorization")
		}
		var request struct {
			Model    string
			Messages []struct{ Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Model != "primary-model" || len(request.Messages) == 0 {
			t.Error("primary model or prompt missing")
		}
		writePrimaryLLMResponse(w, "Electric collection")
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fallbackCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "Fallback collection"})
	}))
	defer fallback.Close()
	service := configurePrimaryLLMTest(t, primary, fallback)
	name, err := service.GenerateBinderNameContext(context.Background(), binderTestCards)
	if err != nil || name != "Electric collection" || primaryCalls.Load() != 1 || fallbackCalls.Load() != 0 {
		t.Fatalf("primary result=%q error=%v calls=%d/%d", name, err, primaryCalls.Load(), fallbackCalls.Load())
	}
}

func TestLLMPrimaryFailureFallsBackAfterRetries(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		status         int
	}{
		{"unavailable", "upstream body must not be logged", http.StatusServiceUnavailable},
		{"invalid envelope", "broken JSON", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var primaryCalls, fallbackCalls atomic.Int32
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				primaryCalls.Add(1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer primary.Close()
			fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fallbackCalls.Add(1)
				if r.Header.Get("Authorization") != "" {
					t.Error("primary credential leaked to fallback")
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"response": "Fallback collection"})
			}))
			defer fallback.Close()
			service := configurePrimaryLLMTest(t, primary, fallback)
			name, err := service.GenerateBinderNameContext(context.Background(), binderTestCards)
			if err != nil || name != "Fallback collection" {
				t.Fatalf("name=%q error=%v", name, err)
			}
			if primaryCalls.Load() != primaryLLMAttempts || fallbackCalls.Load() != 1 {
				t.Fatalf("provider calls=%d/%d", primaryCalls.Load(), fallbackCalls.Load())
			}
		})
	}
}

func TestLLMPrimaryCallerDeadlineDoesNotRetryOrFallback(t *testing.T) {
	var primaryCalls, fallbackCalls atomic.Int32
	release := make(chan struct{})
	primary := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		primaryCalls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer primary.Close()
	defer close(release)
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fallbackCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "Fallback collection"})
	}))
	defer fallback.Close()
	service := configurePrimaryLLMTest(t, primary, fallback)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	name, err := service.GenerateBinderNameContext(ctx, binderTestCards)
	if !errors.Is(err, context.DeadlineExceeded) || name != "" || primaryCalls.Load() != 1 || fallbackCalls.Load() != 0 {
		t.Fatalf("name=%q error=%v calls=%d/%d", name, err, primaryCalls.Load(), fallbackCalls.Load())
	}
}

func TestLLMRejectsTruncatedOllamaTextResponse(t *testing.T) {
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "Incomplete binder", "done_reason": "length"})
	}))
	defer fallback.Close()
	service, err := NewLLMServiceWithConfig(LLMConfig{BaseURL: fallback.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GenerateBinderNameContext(context.Background(), binderTestCards); err == nil {
		t.Fatal("truncated text was accepted as a completed answer")
	}
}
