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

// multiImageClaudeDetector records the crops the pipeline hands to a detector
// that supports magnified bands.
type multiImageClaudeDetector struct {
	info      *claudedetect.DetectedCardInfo
	images    [][]byte
	game      string
	lang      string
	imagesErr error
	detectErr error
}

func (m *multiImageClaudeDetector) Detect(context.Context, []byte, string, string) (*claudedetect.DetectedCardInfo, error) {
	return m.info, m.detectErr
}

func (m *multiImageClaudeDetector) DetectImages(_ context.Context, images [][]byte, gameHint, langHint string) (*claudedetect.DetectedCardInfo, error) {
	m.images = images
	m.game = gameHint
	m.lang = langHint
	if m.imagesErr != nil {
		return nil, m.imagesErr
	}
	return m.info, nil
}

func (m *multiImageClaudeDetector) Model() string             { return "claude-sonnet-5-5" }
func (m *multiImageClaudeDetector) PromptCachingEnabled() bool { return true }

func testCardImage(t *testing.T) []byte {
	t.Helper()
	return testCardImageSize(t, 120, 168)
}

func testCardImageSize(t *testing.T, width, height int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("encode test card: %v", err)
	}
	return buffer.Bytes()
}

func TestClaudeDetectionSendsMagnifiedCropsAndScopeHints(t *testing.T) {
	detector := &multiImageClaudeDetector{info: &claudedetect.DetectedCardInfo{
		Name:            "pikachu",
		CollectorNumber: "018",
		SetCode:         "SV2a",
		Confidence:      0.95,
	}}
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = detector

	cards := []models.Card{{
		ID: "pika", Name: "Pikachu", CollectorNumber: "018", SetCode: "SV2a",
		Language: "ja", Game: "pokemon",
	}}
	result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{
		Image: testCardImage(t),
		Cards: cards,
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageJapanese},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(detector.images) != 3 {
		t.Fatalf("expected full card plus two magnified bands, got %d images", len(detector.images))
	}
	for index, image := range detector.images {
		if len(image) == 0 {
			t.Fatalf("image %d is empty", index)
		}
	}
	if detector.game != "pokemon" {
		t.Errorf("expected game hint pokemon, got %q", detector.game)
	}
	if detector.lang != "Japanese (ja, jpn)" {
		t.Errorf("expected readable language hint, got %q", detector.lang)
	}
	if result.BestMatchID() != "pika" {
		t.Errorf("expected pika, got %q", result.BestMatchID())
	}
}

func TestClaudeDetectionFallsBackToFullImageWhenCropsAreRejected(t *testing.T) {
	detector := &multiImageClaudeDetector{
		info:      &claudedetect.DetectedCardInfo{Name: "Pikachu", CollectorNumber: "018", Confidence: 0.95},
		imagesErr: errors.New("claude detect: too many images for one request"),
	}
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = detector

	result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{
		Image: testCardImage(t),
		Cards: []models.Card{{ID: "pika", Name: "Pikachu", CollectorNumber: "018", Language: "ja", Game: "pokemon"}},
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageJapanese},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BestMatchID() != "pika" {
		t.Errorf("expected the full-card fallback to still match, got %q", result.BestMatchID())
	}

	// A failing full-card retry must surface as a controlled detection error.
	detector.detectErr = errors.New("claude detect: API status 529")
	if _, err := pipeline.DetectScoped(context.Background(), DetectionRequest{
		Image: testCardImage(t),
		Cards: []models.Card{{ID: "pika", Name: "Pikachu", CollectorNumber: "018", Language: "ja", Game: "pokemon"}},
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageJapanese},
	}); err == nil {
		t.Fatal("expected the detector error to surface")
	}
}

func TestClaudeDetectionRecoversVoicingOnlyDifference(t *testing.T) {
	// The live scanner read ピジョットex as ビジョットex on half of the scans of
	// one photo. The voicing-folded fallback still has to surface the real card.
	detector := &mockClaudeDetector{info: &claudedetect.DetectedCardInfo{
		Name:            "ビジョットex",
		CollectorNumber: "136",
		SetCode:         "SV3",
		Confidence:      0.8,
	}}
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = detector

	cards := []models.Card{
		{ID: "pidgeot-136", Name: "ピジョットex", CollectorNumber: "136", SetCode: "SV3", Language: "ja", Game: "pokemon"},
		{ID: "pidgeot-089", Name: "ピジョットex", CollectorNumber: "089", SetCode: "SV3", Language: "ja", Game: "pokemon"},
		{ID: "pidgeot-018", Name: "ピジョット", CollectorNumber: "018", SetCode: "SV2a", Language: "ja", Game: "pokemon"},
	}
	result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{
		Image: testCardImage(t),
		Cards: cards,
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageJapanese},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BestMatchID() != "pidgeot-136" {
		t.Fatalf("expected the voicing-folded printing pidgeot-136, got %q", result.BestMatchID())
	}
	if result.Status != DetectionStatusNeedsReview {
		t.Errorf("a voicing-folded match must stay reviewable, got %s", result.Status)
	}
}

func TestClaudeDetectionWithoutCropSupportKeepsSingleImage(t *testing.T) {
	detector := &mockClaudeDetector{info: &claudedetect.DetectedCardInfo{Name: "Pikachu", CollectorNumber: "018", Confidence: 0.95}}
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = detector

	result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{
		Image: testCardImage(t),
		Cards: []models.Card{{ID: "pika", Name: "Pikachu", CollectorNumber: "018", Language: "ja", Game: "pokemon"}},
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageJapanese},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BestMatchID() != "pika" {
		t.Errorf("expected pika, got %q", result.BestMatchID())
	}
}

func TestClaudeImageVariantsAreReadableJPEG(t *testing.T) {
	variants, err := claudeImageVariants(context.Background(), testCardImage(t))
	if err != nil {
		t.Fatalf("prepare image: %v", err)
	}
	if len(variants) != 3 {
		t.Fatalf("expected three variants, got %d", len(variants))
	}
	for index, variant := range variants {
		if len(variant) < 3 || variant[0] != 0xFF || variant[1] != 0xD8 {
			t.Fatalf("variant %d is not a JPEG", index)
		}
	}
	if got := claudeLanguageHint(models.LanguageJapanese); got != "Japanese (ja, jpn)" {
		t.Errorf("unexpected language hint %q", got)
	}
	if got := claudeLanguageHint(models.LanguageAny); got != "" {
		t.Errorf("auto language must not add a hint, got %q", got)
	}
}

func TestClaudeImageVariantsSkipBandsForSidewaysPhotos(t *testing.T) {
	variants, err := claudeImageVariants(context.Background(), testCardImageSize(t, 400, 220))
	if err != nil {
		t.Fatalf("prepare image: %v", err)
	}
	if len(variants) != 1 {
		t.Fatalf("a landscape photo must not get portrait bands, got %d images", len(variants))
	}
}

func TestClaudeBandsFollowTheCardInsteadOfTheFrame(t *testing.T) {
	// A phone photo with a wide dark border: the card starts well below the
	// frame edge, so frame-relative bands would cut the printed name away.
	photo := image.NewRGBA(image.Rect(0, 0, 400, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 400; x++ {
			shade := uint8(8)
			if x >= 80 && x < 320 && y >= 90 && y < 520 {
				shade = 220
			}
			photo.Set(x, y, color.RGBA{R: shade, G: shade, B: shade, A: 255})
		}
	}
	card := cardBounds(photo)
	if card.Empty() {
		t.Fatal("expected the bright card rectangle to be located")
	}
	if card.Min.X < 70 || card.Min.X > 90 || card.Max.X < 310 || card.Max.X > 330 {
		t.Errorf("card x bounds off: %v", card)
	}
	if card.Min.Y < 80 || card.Min.Y > 100 || card.Max.Y < 510 || card.Max.Y > 530 {
		t.Errorf("card y bounds off: %v", card)
	}
	bands := claudeBands(photo)
	if len(bands) != 2 {
		t.Fatalf("expected two bands, got %d", len(bands))
	}
	title, info := bands[0], bands[1]
	if title.MinY != 0 || title.MaxY < 0.33 || title.MaxY > 0.40 {
		t.Errorf("title band must cover the printed name, got %+v", title)
	}
	if info.MinY < 0.60 || info.MinY > 0.68 || info.MaxY != 1 {
		t.Errorf("information band must cover the bottom line, got %+v", info)
	}
	if title.MinX < 0.15 || title.MinX > 0.22 || title.MaxX < 0.78 || title.MaxX > 0.86 {
		t.Errorf("bands must stay on the card with a small margin, got %+v", title)
	}
}

func TestClaudeBandsFallBackToGenerousStrips(t *testing.T) {
	uniform := image.NewRGBA(image.Rect(0, 0, 200, 300))
	bands := claudeBands(uniform)
	if len(bands) != 2 {
		t.Fatalf("expected two bands, got %d", len(bands))
	}
	if bands[0].MinY != 0 || bands[0].MaxY != claudeFallbackTitleEnd {
		t.Errorf("unexpected fallback title band %+v", bands[0])
	}
	if bands[1].MinY != claudeFallbackInfoStart || bands[1].MaxY != 1 {
		t.Errorf("unexpected fallback information band %+v", bands[1])
	}
}
