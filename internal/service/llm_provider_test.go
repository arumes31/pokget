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
		_, err := service.FuzzyMatchCardWithValidationContext(ctx, "Pikachu", []models.Card{{ID: "pikachu", Name: "Pikachu"}})
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
	name, err := service.GenerateBinderName([]models.Card{{ID: "pikachu", Name: "Pikachu"}})
	if err != nil || name != "Electric collection" || primaryCalls.Load() != 1 || fallbackCalls.Load() != 0 {
		t.Fatalf("primary result=%q error=%v calls=%d/%d", name, err, primaryCalls.Load(), fallbackCalls.Load())
	}
}

func TestLLMPrimaryFallbackAndAbstention(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		status         int
		fallback       bool
	}{
		{"unavailable", "upstream body must not be logged", http.StatusServiceUnavailable, true},
		{"invalid envelope", "broken JSON", http.StatusOK, true},
		{"invalid card JSON", "not a card selection", http.StatusOK, true},
		{"outside shortlist", `{"card_id":"unknown-private-id"}`, http.StatusOK, true},
		{"missing selection", `{}`, http.StatusOK, true},
		{"abstention", `{"card_id":""}`, http.StatusOK, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var primaryCalls, fallbackCalls atomic.Int32
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				primaryCalls.Add(1)
				w.WriteHeader(tc.status)
				if tc.name == "invalid envelope" || tc.status != http.StatusOK {
					_, _ = w.Write([]byte(tc.response))
					return
				}
				writePrimaryLLMResponse(w, tc.response)
			}))
			defer primary.Close()
			fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fallbackCalls.Add(1)
				if r.Header.Get("Authorization") != "" {
					t.Error("primary credential leaked to fallback")
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"response": `{"card_id":"pikachu-025"}`})
			}))
			defer fallback.Close()
			service := configurePrimaryLLMTest(t, primary, fallback)
			result, err := service.FuzzyMatchCardWithValidation("Pikachu 025", []models.Card{{ID: "pikachu-025", Name: "Pikachu", CollectorNumber: "025"}})
			if err != nil || result == nil {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			wantPrimaryCalls := int32(1)
			if tc.fallback {
				wantPrimaryCalls = 3
			}
			if primaryCalls.Load() != wantPrimaryCalls || (fallbackCalls.Load() == 1) != tc.fallback {
				t.Fatalf("provider calls=%d/%d", primaryCalls.Load(), fallbackCalls.Load())
			}
			if tc.fallback && result.CardID != "pikachu-025" || !tc.fallback && !result.Abstained {
				t.Fatalf("result=%+v", result)
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
		_ = json.NewEncoder(w).Encode(map[string]string{"response": `{"card_id":"pikachu"}`})
	}))
	defer fallback.Close()
	service := configurePrimaryLLMTest(t, primary, fallback)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	result, err := service.FuzzyMatchCardWithValidationContext(ctx, "Pikachu", []models.Card{{ID: "pikachu", Name: "Pikachu"}})
	if !errors.Is(err, context.DeadlineExceeded) || result != nil || primaryCalls.Load() != 1 || fallbackCalls.Load() != 0 {
		t.Fatalf("result=%+v error=%v calls=%d/%d", result, err, primaryCalls.Load(), fallbackCalls.Load())
	}
}

func TestLLMPrimaryFallbackBudgetSupportsCanonicalPrintingIDs(t *testing.T) {
	const cardID = "card_a7cc167d64e03cae6e78e99b2b6dad0b7d2550b92b0e2fb37935a3dca974150d"
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Options struct {
				NumPredict int `json:"num_predict"`
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Options.NumPredict < 128 {
			_ = json.NewEncoder(w).Encode(map[string]string{"response": `{"card_id":"card_a7cc167d64e03cae6e78e99b2b6`, "done_reason": "length"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"response": `{"card_id":"` + cardID + `"}`, "done_reason": "stop"})
	}))
	defer fallback.Close()
	service := configurePrimaryLLMTest(t, primary, fallback)
	result, err := service.FuzzyMatchCardWithValidation("Furret", []models.Card{{ID: cardID, Name: "Furret"}})
	if err != nil || result == nil || result.CardID != cardID {
		t.Fatalf("canonical printing result=%+v error=%v", result, err)
	}
	legacy, err := NewLLMServiceWithConfig(LLMConfig{BaseURL: fallback.URL})
	if err != nil || legacy.effectiveNumPredict() != 128 {
		t.Fatal("Ollama-only defaults must also support complete canonical IDs")
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
	if _, err := service.GenerateBinderName([]models.Card{{ID: "pikachu", Name: "Pikachu"}}); err == nil {
		t.Fatal("truncated text was accepted as a completed answer")
	}
}
