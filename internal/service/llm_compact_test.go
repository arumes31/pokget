package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pokget/internal/models"
)

func TestCompactSelectionIDsResolveOnlyToServerCatalog(t *testing.T) {
	for _, selected := range []string{"c1", "c99", ""} {
		t.Run(selected, func(t *testing.T) {
			stableID := "card_" + strings.Repeat("f", 64)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Prompt string         `json:"prompt"`
					Format map[string]any `json:"format"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if strings.Contains(payload.Prompt, stableID) || !strings.Contains(payload.Prompt, `"card_id":"c1"`) {
					t.Errorf("not a compact prompt: %s", payload.Prompt)
				}
				response, _ := json.Marshal(map[string]string{"card_id": selected})
				_ = json.NewEncoder(w).Encode(map[string]any{"response": string(response), "done": true, "done_reason": "stop"})
			}))
			defer server.Close()
			llm := &LLMService{BaseURL: server.URL, Model: "test", HTTPClient: server.Client(), compactSelectionIDs: true}
			result, err := llm.FuzzyMatchCardScopedContext(context.Background(), "Furret 136", []models.Card{
				{ID: stableID, Name: "Furret", Game: "pokemon", Language: "en", CollectorNumber: "136"},
			}, ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish})
			if selected == "c99" {
				if !errors.Is(err, ErrInvalidLLMResponse) {
					t.Fatalf("forged alias error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if selected == "" {
				if !result.Abstained {
					t.Fatal("empty alias must abstain")
				}
			} else if result.CardID != stableID {
				t.Fatalf("API returned alias instead of canonical ID: %q", result.CardID)
			}
		})
	}
}
