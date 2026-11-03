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
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"pokget/internal/claudedetect"
	"pokget/internal/models"
)

func pipelineCardPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 120, 168))
	for y := 0; y < 168; y++ {
		for x := 0; x < 120; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return buffer.Bytes()
}

func TestDetectionPipelineRequiresAVisionDetector(t *testing.T) {
	// Without a configured detector the server must refuse the scan. Guessing a
	// card locally would put the wrong card into a collection.
	pipeline := NewDetectionPipeline(nil, nil)
	result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{
		Image: pipelineCardPNG(t),
		Cards: []models.Card{{ID: "pika", Name: "Pikachu", Game: "pokemon", Language: "en", CollectorNumber: "025"}},
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish},
	})
	if !errors.Is(err, ErrDetectionUnavailable) {
		t.Fatalf("expected ErrDetectionUnavailable, got %v", err)
	}
	if result == nil || result.Status != DetectionStatusFailed {
		t.Fatalf("expected failed status, got %+v", result)
	}
}

func TestDetectionPipelineRejectsEmptyImageBeforeDetection(t *testing.T) {
	pipeline := NewDetectionPipeline(nil, nil)
	if _, err := pipeline.DetectScoped(context.Background(), DetectionRequest{
		Cards: []models.Card{{ID: "pika", Name: "Pikachu", Game: "pokemon", Language: "en"}},
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish},
	}); !errors.Is(err, ErrInvalidDetectionRequest) {
		t.Fatalf("expected ErrInvalidDetectionRequest, got %v", err)
	}
}

func TestCardsForScopeKeepsOnlyMatchingActivePrintings(t *testing.T) {
	inactive := false
	cards := []models.Card{
		{ID: "keep", Name: "Pikachu", Game: "pokemon", Language: "en", CollectorNumber: "025"},
		{ID: "other-game", Name: "Pikachu", Game: "magic", Language: "en", CollectorNumber: "025"},
		{ID: "other-language", Name: "Pikachu", Game: "pokemon", Language: "ja", CollectorNumber: "025"},
		{ID: "inactive", Name: "Pikachu", Game: "pokemon", Language: "en", CollectorNumber: "025", CatalogActive: &inactive},
		{ID: "", Name: "Pikachu", Game: "pokemon", Language: "en", CollectorNumber: "025"},
	}
	filtered := cardsForScope(cards, ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish})
	if len(filtered) != 1 || filtered[0].ID != "keep" {
		t.Fatalf("scope filter kept %+v", filtered)
	}
}

func TestDetectScopedRejectsMismatchedScope(t *testing.T) {
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = &stubClaudeDetector{}
	_, err := pipeline.DetectScoped(context.Background(), DetectionRequest{
		Image: pipelineCardPNG(t),
		Cards: []models.Card{{ID: "pika", Name: "Pikachu", Game: "magic", Language: "en"}},
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish},
	})
	if !errors.Is(err, ErrNoEligibleCards) {
		t.Fatalf("expected ErrNoEligibleCards, got %v", err)
	}
}

func TestDetectScopedHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pipeline := NewDetectionPipeline(nil, nil)
	result, err := pipeline.DetectScoped(ctx, DetectionRequest{
		Image: pipelineCardPNG(t),
		Cards: []models.Card{{ID: "pika", Name: "Pikachu", Game: "pokemon", Language: "en"}},
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if result == nil || result.Status != DetectionStatusCanceled {
		t.Fatalf("expected canceled status, got %+v", result)
	}
}

// stubClaudeDetector answers with a catalog hit and supports magnified crops.
type stubClaudeDetector struct{}

func (d *stubClaudeDetector) Detect(context.Context, []byte, string, string) (*claudedetect.DetectedCardInfo, error) {
	return &claudedetect.DetectedCardInfo{Name: "Pikachu", CollectorNumber: "025", Confidence: 0.99}, nil
}

func (d *stubClaudeDetector) DetectImages(context.Context, [][]byte, string, string) (*claudedetect.DetectedCardInfo, error) {
	return d.Detect(context.Background(), nil, "", "")
}

func (d *stubClaudeDetector) Model() string             { return "claude-sonnet-5-5" }
func (d *stubClaudeDetector) PromptCachingEnabled() bool { return true }