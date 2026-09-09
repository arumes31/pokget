package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"pokget/internal/models"
)

func TestDeviceOCRAmbiguityUsesLocalSelectorAndRemainsReviewable(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/generate" {
			t.Error("device OCR contacted the primary provider")
		}
		var payload struct {
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		_, input, ok := strings.Cut(payload.Prompt, "Input: ")
		if !ok {
			t.Error("missing prompt input")
			return
		}
		var shortlist struct {
			Candidates []struct {
				ID   string `json:"card_id"`
				Name string `json:"name"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(input), &shortlist); err != nil {
			t.Error(err)
			return
		}
		selected := ""
		for _, candidate := range shortlist.Candidates {
			if candidate.Name == "Aggron ex" {
				selected = candidate.ID
			}
		}
		if selected == "" || !strings.HasPrefix(selected, "c") {
			t.Error("expected a compact, eligible candidate")
		}
		response, _ := json.Marshal(map[string]string{"card_id": selected})
		_ = json.NewEncoder(w).Encode(map[string]any{"response": string(response), "done": true, "done_reason": "stop"})
	}))
	defer server.Close()
	llm := &LLMService{BaseURL: server.URL, PrimaryBaseURL: server.URL + "/forbidden-primary", Model: "test", HTTPClient: server.Client()}
	cards := []models.Card{
		{ID: "printing-ex95", Name: "Aggron ex", Game: "pokemon", Language: "en", CollectorNumber: "95"},
		{ID: "printing-regular1", Name: "Aggron", Game: "pokemon", Language: "en", CollectorNumber: "1"},
	}
	result, err := NewDetectionPipeline(nil, llm).DetectTextScoped(context.Background(), TextDetectionRequest{
		Text: "Aggron ex 150 HP\n1\nRend 30", Cards: cards, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || result.BestMatchID() != "printing-ex95" || !result.BestMatchNeedsReview() {
		t.Fatalf("calls=%d match=%q review=%v", calls.Load(), result.BestMatchID(), result.BestMatchNeedsReview())
	}
	if llm.PrimaryBaseURL != server.URL+"/forbidden-primary" || llm.compactSelectionIDs {
		t.Fatal("request mutated the shared model configuration")
	}
}
