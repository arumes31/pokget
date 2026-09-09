package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pokget/internal/models"
)

func TestDetectTextScopedDoesNotRunImageStages(t *testing.T) {
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.ocrRunner = func(context.Context, []byte, []models.Card, string) (string, string, []byte, error) {
		t.Error("server OCR must not run for device text")
		return "", "", nil, nil
	}
	pipeline.fingerprintRunner = func(context.Context, []byte, []models.Card, *ScanScope) (*MatchResult, error) {
		t.Error("server image hashing must not run for device text")
		return nil, nil
	}
	cards := []models.Card{
		{ID: "furret-en", Name: "Furret", Game: "pokemon", Language: "en", CollectorNumber: "136", SetCode: "sv3"},
		{ID: "furret-de", Name: "Furret", Game: "pokemon", Language: "de", CollectorNumber: "136", SetCode: "sv3"},
	}
	result, err := pipeline.DetectTextScoped(context.Background(), TextDetectionRequest{
		Text: "Furret 136/197 sv3", Cards: cards,
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.BestMatchID() != "furret-en" {
		t.Fatalf("match = %q", result.BestMatchID())
	}
	if !result.BestMatchNeedsReview() || result.BestMatchConfidence() >= 70 {
		t.Fatal("unverified device text must require review")
	}
	if len(result.ProcessedImage) != 0 {
		t.Fatal("text path produced an image")
	}
}

func TestDetectTextScopedRejectsInvalidInput(t *testing.T) {
	pipeline := NewDetectionPipeline(nil, nil)
	for _, text := range []string{"", " \n", strings.Repeat("x", MaxDeviceOCRTextBytes+1), "bad\x00text", string([]byte{0xff})} {
		_, err := pipeline.DetectTextScoped(context.Background(), TextDetectionRequest{Text: text})
		if !errors.Is(err, ErrInvalidDetectionRequest) {
			t.Fatalf("error = %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := pipeline.DetectTextScoped(ctx, TextDetectionRequest{Text: "Furret"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestDeviceTextSkipsUnnecessaryModelCalls(t *testing.T) {
	card := models.Card{ID: "a", Name: "Furret"}
	other := models.Card{ID: "b", Name: "Sentret"}
	for _, tc := range []struct {
		candidates []candidateEvidence
		want       bool
	}{
		{nil, false},
		{[]candidateEvidence{{Card: card}}, false},
		{[]candidateEvidence{{Card: card}, {Card: models.Card{ID: "c", Name: "Furret"}}}, false},
		{[]candidateEvidence{{Card: card}, {Card: other}}, true},
		{[]candidateEvidence{{Card: card, Reasons: []string{"set_and_collector"}}, {Card: other}}, false},
		{[]candidateEvidence{{Card: card, Reasons: []string{"exact_name", "collector_fraction"}}, {Card: other}}, false},
	} {
		if got := deviceTextNeedsLLM(tc.candidates); got != tc.want {
			t.Fatalf("needs LLM = %v, want %v", got, tc.want)
		}
	}
}
