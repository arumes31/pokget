//go:build ocrintegration && cgo && linux

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"pokget/internal/models"
)

func TestBrowserJPEGKeepsTitleWhenTextFallbackChoosesEvolution(t *testing.T) {
	image, err := os.ReadFile("testdata/browser-furret.jpg")
	if err != nil {
		t.Fatal(err)
	}
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"response": `{"card_id":"parent-four"}`})
	}))
	defer fallback.Close()
	llm, err := NewLLMServiceWithConfig(LLMConfig{BaseURL: fallback.URL})
	if err != nil {
		t.Fatal(err)
	}
	cards := []models.Card{
		{ID: "target", Name: "Furret", CollectorNumber: "136", Game: "pokemon", Language: "en"},
		{ID: "earlier", Name: "Furret", CollectorNumber: "21", Game: "pokemon", Language: "en"},
		{ID: "parent-four", Name: "Sentret", CollectorNumber: "4", Game: "pokemon", Language: "en"},
		{ID: "parent-seventy-one", Name: "Sentret", CollectorNumber: "71", Game: "pokemon", Language: "en"},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	result, err := NewDetectionPipeline(nil, llm).DetectScoped(ctx, DetectionRequest{Image: image, Cards: cards, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish}})
	if err != nil || result.BestMatchID() != "target" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if !result.BestMatchNeedsReview() || result.BestMatchConfidence() > 40 {
		t.Fatalf("text fallback must remain reviewable: %+v", result)
	}
}
