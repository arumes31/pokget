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

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"pokget/internal/models"
	"strings"
	"testing"
	"time"
)

// --- SCAN-08: LLM candidate shortlisting tests ---

func TestBuildShortlist(t *testing.T) {
	cards := []models.Card{
		{ID: "1", Name: "Pikachu"},
		{ID: "2", Name: "Pikachu VMAX"},
		{ID: "3", Name: "Charizard"},
		{ID: "4", Name: "Mewtwo"},
		{ID: "5", Name: "Bulbasaur"},
	}

	// With OCR text "Pikachu", the shortlist should rank Pikachu cards first
	shortlist := buildShortlist("Pikachu", cards, 3)
	if len(shortlist) > 3 {
		t.Errorf("Expected at most 3 candidates, got %d", len(shortlist))
	}

	// Pikachu should be in the shortlist
	found := false
	for _, c := range shortlist {
		if c.Name == "Pikachu" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'Pikachu' to be in the shortlist")
	}
}

func TestBuildShortlistFewerCardsThanMax(t *testing.T) {
	cards := []models.Card{
		{ID: "1", Name: "Pikachu"},
		{ID: "2", Name: "Charizard"},
	}

	// If fewer cards than maxCandidates, return all
	shortlist := buildShortlist("test", cards, 30)
	if len(shortlist) != 2 {
		t.Errorf("Expected 2 candidates (all cards), got %d", len(shortlist))
	}
}

func TestBuildShortlistEmptyCards(t *testing.T) {
	shortlist := buildShortlist("test", nil, 30)
	if shortlist != nil {
		t.Errorf("Expected nil for empty cards, got %v", shortlist)
	}
}

func TestBuildShortlistZeroMax(t *testing.T) {
	cards := []models.Card{
		{ID: "1", Name: "Pikachu"},
	}
	shortlist := buildShortlist("test", cards, 0)
	if len(shortlist) != 0 {
		t.Errorf("Expected 0 candidates with maxCandidates=0, got %d", len(shortlist))
	}
}

// --- SCAN-15: LLM response validation tests ---

func TestLLMCardResponseJSONParsing(t *testing.T) {
	// Test valid JSON response
	jsonStr := `{"card_name": "Pikachu", "card_id": "base1-4", "confidence": 0.9}`
	var resp LLMCardResponse
	err := json.Unmarshal([]byte(jsonStr), &resp)
	if err != nil {
		t.Fatalf("Failed to parse valid JSON: %v", err)
	}
	if resp.CardName != "Pikachu" {
		t.Errorf("Expected card_name 'Pikachu', got %q", resp.CardName)
	}
	if resp.CardID != "base1-4" {
		t.Errorf("Expected card_id 'base1-4', got %q", resp.CardID)
	}
	if resp.Confidence != 0.9 {
		t.Errorf("Expected confidence 0.9, got %f", resp.Confidence)
	}
}

func TestLLMCardResponseMissingFields(t *testing.T) {
	// Test JSON with missing card_name
	jsonStr := `{"confidence": 0.5}`
	var resp LLMCardResponse
	err := json.Unmarshal([]byte(jsonStr), &resp)
	if err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}
	if resp.CardName != "" {
		t.Errorf("Expected empty card_name for missing field, got %q", resp.CardName)
	}
}

func TestLLMCardResponseUnknownCard(t *testing.T) {
	jsonStr := `{"card_name": "Unknown Card", "confidence": 0.0}`
	var resp LLMCardResponse
	err := json.Unmarshal([]byte(jsonStr), &resp)
	if err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}
	if resp.CardName != "Unknown Card" {
		t.Errorf("Expected 'Unknown Card', got %q", resp.CardName)
	}
}

func TestValidatePlainTextResponse(t *testing.T) {
	llm := &LLMService{}
	allCards := []models.Card{
		{ID: "1", Name: "Pikachu"},
		{ID: "2", Name: "Charizard"},
	}
	shortlist := []models.Card{
		{ID: "1", Name: "Pikachu"},
	}

	// Test matching against shortlist
	resp, err := llm.validatePlainTextResponse("I think it's Pikachu", allCards, shortlist)
	if !errors.Is(err, ErrInvalidLLMResponse) || resp != nil {
		t.Fatalf("plain-text response = (%+v, %v), want strict rejection", resp, err)
	}
}

func TestValidatePlainTextResponseAllCards(t *testing.T) {
	llm := &LLMService{}
	allCards := []models.Card{
		{ID: "1", Name: "Pikachu"},
		{ID: "2", Name: "Charizard"},
	}
	shortlist := []models.Card{
		{ID: "1", Name: "Pikachu"},
	}

	// Test matching against all cards (not in shortlist)
	resp, err := llm.validatePlainTextResponse("I think it's Charizard", allCards, shortlist)
	if !errors.Is(err, ErrInvalidLLMResponse) || resp != nil {
		t.Fatalf("full-corpus plain-text response = (%+v, %v), want strict rejection", resp, err)
	}
}

func TestValidatePlainTextResponseNoMatch(t *testing.T) {
	llm := &LLMService{}
	allCards := []models.Card{
		{ID: "1", Name: "Pikachu"},
	}
	shortlist := []models.Card{
		{ID: "1", Name: "Pikachu"},
	}

	resp, err := llm.validatePlainTextResponse("Some random text with no card name", allCards, shortlist)
	if !errors.Is(err, ErrInvalidLLMResponse) || resp != nil {
		t.Fatalf("arbitrary plain-text response = (%+v, %v), want strict rejection", resp, err)
	}
}

func TestFuzzyMatchCardWithValidationJSONExtraction(t *testing.T) {
	// Test that JSON can be extracted from a response with extra text
	response := `Here is my analysis: {"card_name": "Pikachu", "confidence": 0.85}. Hope this helps!`

	// Find JSON boundaries
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")

	if jsonStart == -1 || jsonEnd == -1 {
		t.Fatal("Expected to find JSON boundaries in response")
	}

	jsonStr := response[jsonStart : jsonEnd+1]
	var resp LLMCardResponse
	err := json.Unmarshal([]byte(jsonStr), &resp)
	if err != nil {
		t.Fatalf("Failed to parse extracted JSON: %v", err)
	}
	if resp.CardName != "Pikachu" {
		t.Errorf("Expected 'Pikachu', got %q", resp.CardName)
	}
}

func TestLLMCardResponseConfidenceClamping(t *testing.T) {
	// Test that FuzzyMatchCardWithValidation clamps out-of-range confidence
	// values returned by the LLM, rather than manually replicating the clamping
	// logic here. Uses a test HTTP server that returns JSON with invalid
	// confidence values.

	knownCards := []models.Card{
		{ID: "test-1", Name: "Pikachu"},
	}

	tests := []struct {
		name          string
		llmConfidence float64
		wantClamped   float64
	}{
		{"negative confidence clamped to 0", -0.5, 0},
		{"confidence > 1 clamped to 1", 1.5, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up a test HTTP server that returns an LLM response with
			// the specified out-of-range confidence value.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				llmJSON := fmt.Sprintf(`{"card_id": "test-1", "confidence": %f, "abstain": false}`, tt.llmConfidence)
				resp := map[string]string{"response": llmJSON}
				json.NewEncoder(w).Encode(resp)
			}))
			defer srv.Close()

			svc := &LLMService{
				BaseURL:    srv.URL,
				Model:      "test-model",
				HTTPClient: srv.Client(),
			}

			result, err := svc.FuzzyMatchCardWithValidation("Pikachu", knownCards)
			if err != nil {
				t.Fatalf("FuzzyMatchCardWithValidation returned error: %v", err)
			}
			if result.Confidence != tt.wantClamped {
				t.Errorf("Expected clamped confidence %f, got %f", tt.wantClamped, result.Confidence)
			}
		})
	}
}

func TestFuzzyMatchCardWithValidationContextCancelsRequest(t *testing.T) {
	t.Parallel()

	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		select {
		case <-r.Context().Done():
		case <-releaseRequest:
		}
	}))
	defer server.Close()
	defer close(releaseRequest)

	service := &LLMService{BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	startedAt := time.Now()
	_, err := service.FuzzyMatchCardWithValidationContext(
		ctx,
		"Pikachu",
		[]models.Card{{ID: "pikachu", Name: "Pikachu"}},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("canceled LLM request returned after %s", elapsed)
	}
	select {
	case <-requestStarted:
	default:
		t.Fatal("LLM request never reached test server")
	}
}

func TestLLMAutoSetupContextCancelsRequest(t *testing.T) {
	t.Parallel()

	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
	}))
	defer server.Close()

	service := &LLMService{
		BaseURL:    server.URL,
		Model:      "test-model",
		HTTPClient: server.Client(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	startedAt := time.Now()
	service.AutoSetupContext(ctx)
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("canceled model setup returned after %s", elapsed)
	}
	select {
	case <-requestStarted:
	default:
		t.Fatal("model setup request never reached test server")
	}
}

func TestLLMStrictMatchSendsOnlyEvidenceBackedShortlist(t *testing.T) {
	t.Parallel()

	var requestPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&requestPayload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"response": `{"card_id":"pikachu-025","confidence":0.92,"abstain":false}`,
		})
	}))
	defer server.Close()

	service := &LLMService{
		BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client(),
		Seed: 7, NumPredict: 48, MaxCandidates: 5, MinEvidence: 180, MinConfidence: 0.6,
	}
	inactive := false
	response, err := service.FuzzyMatchCardScopedContext(context.Background(), "Pikachu 025", []models.Card{
		{ID: "pikachu-025", Name: "Pikachu", CollectorNumber: "025", Game: "pokemon", Language: "en"},
		{ID: "charizard-006", Name: "Charizard", CollectorNumber: "006", Game: "pokemon", Language: "en"},
		{ID: "inactive-pikachu", Name: "Pikachu", CollectorNumber: "025", CatalogActive: &inactive},
		{ID: "magic-pikachu", Name: "Pikachu", CollectorNumber: "025", Game: "magic", Language: "en"},
		{ID: "german-pikachu", Name: "Pikachu", CollectorNumber: "025", Game: "pokemon", Language: "de"},
	}, ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish})
	if err != nil {
		t.Fatal(err)
	}
	if response.CardID != "pikachu-025" || response.CardName != "Pikachu" || response.Abstained {
		t.Fatalf("strict response = %+v", response)
	}
	if requestPayload["format"] != "json" {
		t.Fatalf("format = %#v, want json", requestPayload["format"])
	}
	options, ok := requestPayload["options"].(map[string]any)
	if !ok || options["seed"] != float64(7) || options["num_predict"] != float64(48) {
		t.Fatalf("options = %#v", requestPayload["options"])
	}
	prompt, _ := requestPayload["prompt"].(string)
	if !strings.Contains(prompt, "pikachu-025") {
		t.Fatalf("prompt omitted shortlisted printing ID: %s", prompt)
	}
	if strings.Contains(prompt, "charizard-006") || strings.Contains(prompt, "inactive-pikachu") ||
		strings.Contains(prompt, "magic-pikachu") || strings.Contains(prompt, "german-pikachu") {
		t.Fatalf("prompt leaked non-evidence or inactive corpus entries: %s", prompt)
	}
}

func TestLLMStrictMatchRejectsArbitraryAndOutsideShortlistOutput(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"card name instead of ID": `{"card_name":"Pikachu","confidence":0.9}`,
		"ID outside shortlist":    `{"card_id":"charizard-006","confidence":0.9,"abstain":false}`,
		"plain text ID":           `pikachu-025`,
	}
	for name, modelResponse := range tests {
		modelResponse := modelResponse
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]string{"response": modelResponse})
			}))
			defer server.Close()
			service := &LLMService{BaseURL: server.URL, Model: "test", HTTPClient: server.Client()}
			response, err := service.FuzzyMatchCardWithValidation("Pikachu 025", []models.Card{
				{ID: "pikachu-025", Name: "Pikachu", CollectorNumber: "025"},
				{ID: "charizard-006", Name: "Charizard", CollectorNumber: "006"},
			})
			if !errors.Is(err, ErrInvalidLLMResponse) || response != nil {
				t.Fatalf("response = (%+v, %v), want strict rejection", response, err)
			}
		})
	}
}

func TestLLMStrictMatchAbstains(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"response": `{"card_id":"","confidence":0,"abstain":true}`,
		})
	}))
	defer server.Close()
	service := &LLMService{BaseURL: server.URL, Model: "test", HTTPClient: server.Client()}
	response, err := service.FuzzyMatchCardWithValidation("Pikachu", []models.Card{{ID: "pikachu", Name: "Pikachu"}})
	if err != nil {
		t.Fatal(err)
	}
	if !response.Abstained || response.CardID != "" || response.CardName != "Unknown Card" {
		t.Fatalf("abstention = %+v", response)
	}
}

func TestNewLLMServiceWithConfigNormalizesHostAndOptions(t *testing.T) {
	t.Parallel()

	service, err := NewLLMServiceWithConfig(LLMConfig{
		BaseURL: "ollama.internal", Model: "qwen-test", Temperature: 0.2,
		Seed: 99, NumPredict: 64, MaxCandidates: 12, MinEvidence: 250, MinConfidence: 0.7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if service.BaseURL != "http://ollama.internal:11434" || service.Model != "qwen-test" {
		t.Fatalf("service endpoint/model = %q/%q", service.BaseURL, service.Model)
	}
	if service.Seed != 99 || service.NumPredict != 64 || service.MaxCandidates != 12 || service.MinEvidence != 250 || service.MinConfidence != 0.7 {
		t.Fatalf("service options = %+v", service)
	}
}
