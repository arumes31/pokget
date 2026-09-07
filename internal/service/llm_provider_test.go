package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestDetectScopedPrimaryVisionResolvesReviewablePrinting(t *testing.T) {
	var primaryCalls, fallbackCalls atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls.Add(1)
		var request struct {
			Messages []struct {
				Content []struct {
					Type, Text string
					ImageURL   struct{ URL string } `json:"image_url"`
				}
			}
			ResponseFormat struct{ Type string } `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.Messages) != 1 || len(request.Messages[0].Content) != 2 {
			t.Error("vision request must contain the prompt and cropped image")
			writePrimaryLLMResponse(w, `{"card_id":"printing-b"}`)
			return
		}
		if request.ResponseFormat.Type != "json_schema" {
			t.Error("strict response schema missing")
		}
		prompt := request.Messages[0].Content[0].Text
		if !strings.Contains(prompt, "printing-a") || !strings.Contains(prompt, "printing-b") || strings.Contains(prompt, "german-printing") {
			t.Error("vision shortlist did not preserve scope")
		}
		imageURL := request.Messages[0].Content[1].ImageURL.URL
		encoded := strings.TrimPrefix(imageURL, "data:image/jpeg;base64,")
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Error("vision crop is not a JPEG data URI")
		} else {
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Error(err)
			} else {
				red, green, _, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
				if img.Bounds().Dx() != 100 || img.Bounds().Dy() != 300 || red <= green {
					t.Errorf("vision must use the color guide crop; bounds=%v red=%d green=%d", img.Bounds(), red, green)
				}
			}
		}
		writePrimaryLLMResponse(w, `{"card_id":"printing-b"}`)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fallbackCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"response": `{"card_id":"printing-a"}`})
	}))
	defer fallback.Close()
	service := configurePrimaryLLMTest(t, primary, fallback)
	pipeline := NewDetectionPipeline(nil, service)
	cards := []models.Card{
		{ID: "printing-a", Name: "Pikachu", Set: "Older set", Game: "pokemon", Language: "en"},
		{ID: "printing-b", Name: "Pikachu", Set: "Newer set", Game: "pokemon", Language: "en"},
		{ID: "german-printing", Name: "Pikachu", Game: "pokemon", Language: "de"},
	}
	pipeline.fingerprintRunner = func(_ context.Context, _ []byte, eligible []models.Card, _ *ScanScope) (*MatchResult, error) {
		return &MatchResult{Potential: []FingerprintMatch{{Card: &eligible[0], Distance: 2}, {Card: &eligible[1], Distance: 3}}}, nil
	}
	pipeline.ocrRunner = func(context.Context, []byte, []models.Card, string) (string, string, []byte, error) {
		return "Pikachu", "printing-a", []byte("gray preview should not be sent"), nil
	}
	img := image.NewRGBA(image.Rect(0, 0, 200, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 200; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 210, G: 30, B: 30, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	ctx := WithOCRScanConfig(context.Background(), OCRScanConfig{GuideCrop: &OCRNormalizedRect{MinX: 0.25, MinY: 0, MaxX: 0.75, MaxY: 1}})
	result, err := pipeline.DetectScoped(ctx, DetectionRequest{Image: encoded.Bytes(), Cards: cards, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish}})
	if err != nil || result == nil {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if primaryCalls.Load() != 1 || fallbackCalls.Load() != 0 || result.BestMatchID() != "printing-b" || !result.BestMatchNeedsReview() {
		t.Fatalf("vision result ID=%s review=%t calls=%d/%d", result.BestMatchID(), result.BestMatchNeedsReview(), primaryCalls.Load(), fallbackCalls.Load())
	}
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
