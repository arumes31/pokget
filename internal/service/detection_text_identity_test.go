package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pokget/internal/models"
)

func TestCollectorFractionOutranksEvolutionAndBodyNumbers(t *testing.T) {
	cards := []models.Card{
		{ID: "parent-four", Name: "Sentret", CollectorNumber: "4", Game: "pokemon"},
		{ID: "target", Name: "Furret", CollectorNumber: "136", Game: "pokemon"},
		{ID: "other-print", Name: "Furret", CollectorNumber: "90", Game: "pokemon"},
	}
	text := "Furret HP110 Evolves from Sentret Draw 3 cards 4 WT 71.6 lbs 136/189"
	ranked := rankCandidates(text, cards, len(cards))
	if len(ranked) == 0 || ranked[0].Card.ID != "target" {
		t.Fatalf("printed collector/title lost to a body number: %+v", ranked)
	}
}

func TestLocalOCRIgnoresOnlyEvolutionName(t *testing.T) {
	for _, text := range []string{"Furret Evolves from Sentret", "Furret Evolvesfrom Sentret", "Furret Evolves from Sen tret"} {
		cards := []models.Card{{ID: "parent", Name: "Sentret"}, {ID: "target", Name: "Furret"}}
		matches := rankLocalMatches([]ocrEvidence{{Text: text, Role: "name"}}, cards, "eng")
		if len(matches) != 1 || matches[0].Card.ID != "target" {
			t.Fatalf("%q: %+v", text, matches)
		}
	}
}

func TestTextFallbackPreservesIdentityAndDoesNotDoubleCountOCR(t *testing.T) {
	for _, test := range []struct {
		name, text, responseID, wantID string
		cards                          []models.Card
	}{
		{name: "evolution parent is not the title", text: "Furret HP110 Evolves from Sentret Draw 3 cards 4 WT 71.6 lbs 136/189", responseID: "parent", wantID: "target", cards: []models.Card{{ID: "parent", Name: "Sentret", CollectorNumber: "4"}, {ID: "target", Name: "Furret", CollectorNumber: "136"}, {ID: "other", Name: "Furret", CollectorNumber: "90"}}},
		{name: "body name cannot override collector", text: "Furret and Sentret 4 136/189", responseID: "parent", wantID: "target", cards: []models.Card{{ID: "parent", Name: "Sentret", CollectorNumber: "4"}, {ID: "target", Name: "Furret", CollectorNumber: "136"}}},
		{name: "matching OCR is not independent evidence", text: "Furret", responseID: "target", wantID: "target", cards: []models.Card{{ID: "target", Name: "Furret"}}},
		{name: "fallback can rank a reviewable same-name printing", text: "Furret", responseID: "b", wantID: "b", cards: []models.Card{{ID: "a", Name: "Furret", Set: "Earlier"}, {ID: "b", Name: "Furret", Set: "Later"}}},
		{name: "actual parent title remains eligible", text: "Sentret HP50 Basic 4/30", responseID: "parent", wantID: "parent", cards: []models.Card{{ID: "parent", Name: "Sentret", CollectorNumber: "4"}, {ID: "other", Name: "Furret", CollectorNumber: "136"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				selection, _ := json.Marshal(map[string]string{"card_id": test.responseID})
				_ = json.NewEncoder(w).Encode(map[string]string{"response": string(selection)})
			}))
			defer fallback.Close()
			llm, err := NewLLMServiceWithConfig(LLMConfig{BaseURL: fallback.URL})
			if err != nil {
				t.Fatal(err)
			}
			pipeline := NewDetectionPipeline(nil, llm)
			pipeline.fingerprintRunner = func(context.Context, []byte, []models.Card, *ScanScope) (*MatchResult, error) {
				return &MatchResult{}, nil
			}
			pipeline.ocrRunner = func(context.Context, []byte, []models.Card, string) (string, string, []byte, error) {
				return test.text, "Unknown Card", nil, nil
			}
			for index := range test.cards {
				test.cards[index].Game, test.cards[index].Language = "pokemon", "en"
			}
			result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{Image: []byte("test image"), Cards: test.cards, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish}})
			if err != nil || result.BestMatchID() != test.wantID {
				t.Fatalf("result=%+v err=%v want=%s", result, err, test.wantID)
			}
			if result.BestMatchConfidence() > 40 || !result.BestMatchNeedsReview() {
				t.Fatalf("text fallback inflated correlated OCR evidence: confidence=%v review=%v", result.BestMatchConfidence(), result.BestMatchNeedsReview())
			}
		})
	}
}

func TestTextSelectionCannotOverrideFingerprintEvidence(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writePrimaryLLMResponse(w, `{"card_id":"c"}`)
	}))
	defer primary.Close()
	llm, err := NewLLMServiceWithConfig(LLMConfig{PrimaryBaseURL: primary.URL, PrimaryModel: "test", PrimaryAPIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	cards := []models.Card{{ID: "a", Name: "Furret", Game: "pokemon", Language: "en"}, {ID: "b", Name: "Furret", Game: "pokemon", Language: "en"}, {ID: "c", Name: "Furret", Game: "pokemon", Language: "en"}}
	pipeline := NewDetectionPipeline(nil, llm)
	pipeline.fingerprintRunner = func(context.Context, []byte, []models.Card, *ScanScope) (*MatchResult, error) {
		return &MatchResult{Potential: []FingerprintMatch{{Card: &cards[0], Distance: 2}, {Card: &cards[1], Distance: 3}}}, nil
	}
	pipeline.ocrRunner = func(context.Context, []byte, []models.Card, string) (string, string, []byte, error) {
		return "Furret", "Unknown Card", nil, nil
	}
	result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{Image: []byte("invalid image makes selection text-only"), Cards: cards, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish}})
	if err != nil || result.BestMatchID() != "a" {
		t.Fatalf("text choice displaced independent visual evidence: %+v, %v", result, err)
	}
}
