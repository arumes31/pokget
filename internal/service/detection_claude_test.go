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
	"errors"
	"testing"
	"time"

	"pokget/internal/claudedetect"
	"pokget/internal/models"
)

type mockClaudeDetector struct {
	info *claudedetect.DetectedCardInfo
	err  error
}

func (m *mockClaudeDetector) Detect(ctx context.Context, imageBytes []byte, gameHint, langHint string) (*claudedetect.DetectedCardInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.info, nil
}

func (m *mockClaudeDetector) Model() string {
	return "claude-sonnet-5-5"
}

func (m *mockClaudeDetector) PromptCachingEnabled() bool {
	return true
}

func TestClaudeDetection_ExactMatch(t *testing.T) {
	mock := &mockClaudeDetector{
		info: &claudedetect.DetectedCardInfo{
			Game:                "pokemon",
			Name:                "Rayquaza",
			CollectorNumber:     "16/110",
			Set:                 "EX Holon Phantoms",
			SetCode:             "HP",
			Language:            "deu",
			Variant:             "Holo Delta Species",
			Confidence:          0.98,
			CacheReadTokens:     1774,
			CacheCreationTokens: 0,
		},
	}

	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = mock

	cards := []models.Card{
		{
			ID:              "rayquaza-hp-16",
			Name:            "Rayquaza",
			CollectorNumber: "16/110",
			Set:             "EX Holon Phantoms",
			SetCode:         "HP",
			Language:        "deu",
			Game:            "pokemon",
		},
		{
			ID:              "pikachu-hp-78",
			Name:            "Pikachu",
			CollectorNumber: "78/110",
			Set:             "EX Holon Phantoms",
			Game:            "pokemon",
		},
	}

	req := DetectionRequest{
		Image: []byte{1, 2, 3, 4},
		Cards: cards,
		Scope: ScanScope{
			TCG:      models.TCGPokemon,
			Language: models.LanguageGerman,
		},
	}

	result, err := pipeline.DetectScoped(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != DetectionStatusMatched {
		t.Errorf("expected DetectionStatusMatched, got %s", result.Status)
	}
	if result.BestMatchID() != "rayquaza-hp-16" {
		t.Errorf("expected rayquaza-hp-16, got %s", result.BestMatchID())
	}
	if result.BestMatchConfidence() < 95.0 {
		t.Errorf("expected high confidence, got %f", result.BestMatchConfidence())
	}
	if result.BestMatchNeedsReview() {
		t.Errorf("expected BestMatchNeedsReview false")
	}
}

func TestClaudeDetection_SyntheticFallback(t *testing.T) {
	mock := &mockClaudeDetector{
		info: &claudedetect.DetectedCardInfo{
			Game:            "one_piece",
			Name:            "Monkey.D.Luffy",
			CollectorNumber: "OP01-001",
			Set:             "Romance Dawn",
			Language:        "eng",
			Variant:         "Leader Parallel",
			Confidence:      0.95,
		},
	}

	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = mock

	// Empty catalog: Claude detected card not currently in catalog slice
	cards := []models.Card{}

	result, err := pipeline.detectClaude(context.Background(), DetectionRequest{
		Image: []byte{1, 2, 3},
		Cards: cards,
	}, false, time.Now())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != DetectionStatusNeedsReview {
		t.Errorf("expected NeedsReview, got %s", result.Status)
	}
	if result.BestMatchID() != "" {
		t.Errorf("expected empty BestMatchID for synthetic match, got %s", result.BestMatchID())
	}
	if result.BestMatchName() != "Monkey.D.Luffy" {
		t.Errorf("expected Monkey.D.Luffy, got %s", result.BestMatchName())
	}
	if result.BestMatchCard().CollectorNumber != "OP01-001" {
		t.Errorf("expected OP01-001, got %s", result.BestMatchCard().CollectorNumber)
	}
}

func TestClaudeDetection_SyntheticFallback_LocalMatchesCatalog(t *testing.T) {
	mock := &mockClaudeDetector{
		info: &claudedetect.DetectedCardInfo{
			Game:            "one_piece",
			Name:            "Unknown Secret Card",
			CollectorNumber: "OP99-999",
			Confidence:      0.85,
		},
	}

	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = mock
	// Local OCR successfully matches catalog card
	pipeline.ocrRunner = func(ctx context.Context, img []byte, cards []models.Card, lang string) (string, string, []byte, error) {
		return "Pikachu 25", "card-pika", nil, nil
	}

	cards := []models.Card{
		{ID: "card-pika", Name: "Pikachu", CollectorNumber: "25"},
	}

	result, err := pipeline.detect(context.Background(), DetectionRequest{
		Image: []byte{1, 2, 3},
		Cards: cards,
	}, "eng", false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Local catalog match must take precedence over Claude synthetic card
	if result.BestMatchID() != "card-pika" {
		t.Errorf("expected local catalog match card-pika, got %s", result.BestMatchID())
	}
}

func TestClaudeDetection_SyntheticFallback_LocalFindsNoMatch(t *testing.T) {
	mock := &mockClaudeDetector{
		info: &claudedetect.DetectedCardInfo{
			Game:            "one_piece",
			Name:            "Monkey.D.Luffy",
			CollectorNumber: "OP01-001",
			Set:             "Romance Dawn",
			Confidence:      0.95,
		},
	}

	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = mock
	// Local OCR finds no match
	pipeline.ocrRunner = func(ctx context.Context, img []byte, cards []models.Card, lang string) (string, string, []byte, error) {
		return "unrecognized uncataloged text", "", nil, nil
	}

	cards := []models.Card{
		{ID: "card-pika", Name: "Pikachu", CollectorNumber: "25"},
	}

	result, err := pipeline.detect(context.Background(), DetectionRequest{
		Image: []byte{1, 2, 3},
		Cards: cards,
	}, "eng", false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When local stages find no catalog match, Claude synthetic match is used
	if result.BestMatchID() != "" {
		t.Errorf("expected empty ID for synthetic fallback, got %s", result.BestMatchID())
	}
	if result.BestMatchName() != "Monkey.D.Luffy" {
		t.Errorf("expected Monkey.D.Luffy, got %s", result.BestMatchName())
	}
	if result.Status != DetectionStatusNeedsReview {
		t.Errorf("expected NeedsReview, got %s", result.Status)
	}
}

func TestClaudeDetection_ErrorFallback(t *testing.T) {
	mock := &mockClaudeDetector{
		err: errors.New("claude rate limit or timeout"),
	}

	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = mock
	// Install dummy ocrRunner to verify fallback
	pipeline.ocrRunner = func(ctx context.Context, img []byte, cards []models.Card, lang string) (string, string, []byte, error) {
		return "Pikachu 25", "card-pika", nil, nil
	}

	cards := []models.Card{
		{ID: "card-pika", Name: "Pikachu", CollectorNumber: "25"},
	}

	result, err := pipeline.detect(context.Background(), DetectionRequest{
		Image: []byte{1, 2, 3},
		Cards: cards,
	}, "eng", false)

	if err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}
	if result.BestMatchID() != "card-pika" {
		t.Errorf("expected fallback to match card-pika, got %s", result.BestMatchID())
	}
}
