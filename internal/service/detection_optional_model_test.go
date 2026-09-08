package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pokget/internal/models"
)

func TestDetectionSlowOptionalModelPreservesLocalCandidates(t *testing.T) {
	called := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		called <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	llm, err := NewLLMServiceWithConfig(LLMConfig{BaseURL: server.URL, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	pipeline := NewDetectionPipeline(nil, llm)
	pipeline.fingerprintRunner = func(context.Context, []byte, []models.Card, *ScanScope) (*MatchResult, error) { return nil, nil }
	pipeline.ocrRunner = func(context.Context, []byte, []models.Card, string) (string, string, []byte, error) {
		return "Furret", "Unknown Card", nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := pipeline.DetectScoped(ctx, DetectionRequest{Image: []byte("image"), Cards: []models.Card{{ID: "target", Name: "Furret", Game: "pokemon", Language: "en"}}, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish}})
	if err != nil || ctx.Err() != nil || result.BestMatchID() != "target" || !result.BestMatchNeedsReview() {
		t.Fatalf("local candidates must survive optional model timeout: result=%+v err=%v caller=%v", result, err, ctx.Err())
	}
	select {
	case <-called:
	default:
		t.Fatal("optional model was not exercised")
	}
	for _, stage := range result.Metrics.Stages {
		if stage.Name == "llm" && !errors.Is(stage.Error, context.DeadlineExceeded) {
			t.Fatalf("unexpected model error: %v", stage.Error)
		}
	}
}
