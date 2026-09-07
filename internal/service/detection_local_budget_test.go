package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pokget/internal/models"
)

func TestDetectionLocalBudgetStopsImageStages(t *testing.T) {
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.fingerprintRunner = func(ctx context.Context, _ []byte, _ []models.Card, _ *ScanScope) (*MatchResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	pipeline.ocrRunner = func(ctx context.Context, _ []byte, _ []models.Card, _ string) (string, string, []byte, error) {
		<-ctx.Done()
		return "", "", nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := pipeline.DetectScoped(WithDetectionStageTimeout(ctx, 10*time.Millisecond), DetectionRequest{Image: []byte("image"), Cards: []models.Card{{ID: "target", Name: "Furret", Game: "pokemon", Language: "en"}}, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish}})
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		t.Fatalf("local stage must expire without canceling caller: err=%v caller=%v", err, ctx.Err())
	}
}

func TestDetectionLocalBudgetDoesNotExpirePrimary(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(350 * time.Millisecond)
		writePrimaryLLMResponse(w, `{"card_id":"target"}`)
	}))
	defer primary.Close()
	llm, err := NewLLMServiceWithConfig(LLMConfig{PrimaryBaseURL: primary.URL, PrimaryModel: "test", PrimaryAPIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	pipeline := NewDetectionPipeline(nil, llm)
	pipeline.fingerprintRunner = func(context.Context, []byte, []models.Card, *ScanScope) (*MatchResult, error) { return nil, nil }
	pipeline.ocrRunner = func(ctx context.Context, _ []byte, _ []models.Card, _ string) (string, string, []byte, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("local OCR lacks its deadline")
		}
		return "Furret", "Unknown Card", nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := pipeline.DetectScoped(WithDetectionStageTimeout(ctx, 200*time.Millisecond), DetectionRequest{Image: []byte("image"), Cards: []models.Card{{ID: "target", Name: "Furret", Game: "pokemon", Language: "en"}}, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish}})
	if err != nil || result.BestMatchID() != "target" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	for _, stage := range result.Metrics.Stages {
		if stage.Name == "llm" && stage.Error != nil {
			t.Fatal(stage.Error)
		}
	}
}
