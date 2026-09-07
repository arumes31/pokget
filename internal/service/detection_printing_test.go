package service

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"testing"

	"pokget/internal/models"
)

func TestResolveOCRCandidatesKeepsFuzzyNameHintReviewable(t *testing.T) {
	t.Parallel()
	cards := []models.Card{{ID: "printing-a", Name: "Pikachu"}, {ID: "printing-b", Name: "Pikachu"}}
	candidates := resolveOCRCandidates("printing-a", "Pikacnu\nHP 60 Thunder Shock", cards)
	if len(candidates) != 2 || candidates[0].Score != candidates[1].Score || uniqueOCRPrintingID(candidates) != "" {
		t.Fatalf("fuzzy OCR lost its candidate family or invented a printing identifier: %+v", candidates)
	}
}

func TestFingerprintStageHonorsEXIFAndGuideCrop(t *testing.T) {
	t.Parallel()
	source := image.NewRGBA(image.Rect(0, 0, 100, 140))
	draw.Draw(source, source.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(source, image.Rect(8, 15, 65, 56), &image.Uniform{C: color.Black}, image.Point{}, draw.Src)
	draw.Draw(source, image.Rect(35, 77, 90, 133), &image.Uniform{C: color.RGBA{R: 180, A: 255}}, image.Point{}, draw.Src)
	fingerprint := NewFingerprintService(nil)
	pipeline := NewDetectionPipeline(fingerprint, nil)

	t.Run("EXIF orientation", func(t *testing.T) {
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, source, &jpeg.Options{Quality: 95}); err != nil {
			t.Fatal(err)
		}
		decoded, err := jpeg.Decode(bytes.NewReader(encoded.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		hash, err := fingerprint.CalculateHash(applyImageOrientation(decoded, 6))
		if err != nil {
			t.Fatal(err)
		}
		exif := jpegWithOrientation(6)
		payload := append(bytes.Clone(exif[:len(exif)-2]), encoded.Bytes()[2:]...)
		result, err := pipeline.runFingerprintStage(context.Background(), payload, []models.Card{{ID: "rotated-card", Phash: &hash}}, nil)
		if err != nil || result == nil || result.BestDistance != 0 {
			t.Fatalf("fingerprint ignored photo orientation: result=%+v err=%v", result, err)
		}
	})

	t.Run("guide crop", func(t *testing.T) {
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, source); err != nil {
			t.Fatal(err)
		}
		crop := OCRNormalizedRect{MinX: 0.05, MinY: 0.1, MaxX: 0.8, MaxY: 0.75}
		cropped, err := cropNormalized(source, crop)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := fingerprint.CalculateHash(cropped)
		if err != nil {
			t.Fatal(err)
		}
		ctx := WithOCRScanConfig(context.Background(), OCRScanConfig{GuideCrop: &crop})
		result, err := pipeline.runFingerprintStage(ctx, encoded.Bytes(), []models.Card{{ID: "cropped-card", Phash: &hash}}, nil)
		if err != nil || result == nil || result.BestDistance != 0 {
			t.Fatalf("fingerprint ignored the selected card region: result=%+v err=%v", result, err)
		}
	})
}

func TestDetectionPrintingTextConflictingWithArtworkRemainsReviewable(t *testing.T) {
	t.Parallel()
	cards := []models.Card{
		{ID: "printing-a", Name: "Pikachu", SetCode: "SV1", CollectorNumber: "025", Game: "pokemon", Language: "en"},
		{ID: "printing-b", Name: "Pikachu", SetCode: "SV2", CollectorNumber: "025", Game: "pokemon", Language: "en"},
	}
	result := runPrintingDetection(t, cards, []int{1, 4}, "Pikachu SV2 025", "printing-a")
	if result.BestMatchID() != "printing-b" || !result.BestMatchNeedsReview() {
		t.Fatalf("conflicting text and image evidence was accepted without review: %+v", result.TopMatches)
	}
}

func TestResolveOCRCandidatesDoesNotPromoteNameSuggestionToPrintingEvidence(t *testing.T) {
	t.Parallel()
	cards := []models.Card{
		{ID: "printing-a", Name: "Pikachu", SetCode: "SV1", CollectorNumber: "025"},
		{ID: "printing-b", Name: "Pikachu", SetCode: "SV2", CollectorNumber: "025"},
	}
	candidates := resolveOCRCandidates("printing-a", "Pikachu", cards)
	if len(candidates) != 2 || candidates[0].Score != candidates[1].Score {
		t.Fatalf("name-only OCR discarded equivalent printings or invented evidence: %+v", candidates)
	}
	candidates = resolveOCRCandidates("printing-a", "Pikachu SV2 025", cards)
	if len(candidates) != 2 || candidates[0].Card.ID != "printing-b" {
		t.Fatalf("OCR hint overrode the visible set and collector number: %+v", candidates)
	}
}

func TestCollectorNumberCannotMatchInsideDifferentNumber(t *testing.T) {
	t.Parallel()
	cards := []models.Card{{ID: "printing-a", Name: "Pikachu", CollectorNumber: "123"}}
	for _, input := range []string{"Pikachu 1234", "Pikachu 0123", "Pikachu 12 34"} {
		candidate := rankCandidates(input, cards, 1)[0]
		for _, reason := range candidate.Reasons {
			if reason == "collector_number" {
				t.Errorf("input %q invented collector number 123: %+v", input, candidate)
			}
		}
	}
}

func TestLocalOCRPreservesStableIDsThatDifferByZeroAndLetterO(t *testing.T) {
	t.Parallel()
	cards := []models.Card{{ID: "printing-0", Name: "Pikachu"}, {ID: "printing-o", Name: "Pikachu"}}
	matches := rankLocalMatches([]ocrEvidence{{Text: "Pikachu", Pass: "name", Role: "name"}}, cards, "eng")
	if len(matches) != 2 {
		t.Fatalf("OCR normalization collapsed different stable printing IDs: %+v", matches)
	}
}

func TestDetectionNearFingerprintDoesNotBypassConflictingOCR(t *testing.T) {
	t.Parallel()
	cards := []models.Card{
		{ID: "wrong", Name: "Pikachu", Game: "pokemon", Language: "en"},
		{ID: "right", Name: "Charizard", Game: "pokemon", Language: "en"},
	}
	result := runPrintingDetection(t, cards, []int{3, 6}, "Charizard", "right")
	if result.Status == DetectionStatusMatched && result.BestMatchID() == "wrong" {
		t.Fatalf("near fingerprint bypassed conflicting card text: %+v", result)
	}
}

func TestDetectionSameArtUsesCollectorEvidenceBeforeReturning(t *testing.T) {
	t.Parallel()
	cards := []models.Card{
		{ID: "printing-a", Name: "Pikachu", SetCode: "SV1", CollectorNumber: "025", Game: "pokemon", Language: "en"},
		{ID: "printing-b", Name: "Pikachu", SetCode: "SV2", CollectorNumber: "025", Game: "pokemon", Language: "en"},
	}
	for _, distances := range [][]int{{0, 0}, {1, 2}} {
		result := runPrintingDetection(t, cards, distances, "Pikachu SV2 025", "printing-a")
		if result.BestMatchID() != "printing-b" {
			t.Errorf("distances %v: same-art fingerprint skipped visible printing evidence: %+v", distances, result.TopMatches)
		}
	}
	result := runPrintingDetection(t, cards, []int{1, 4}, "Pikachu", "printing-a")
	if !result.BestMatchNeedsReview() || len(result.TopMatches) != 2 {
		t.Fatalf("name-only evidence selected an ambiguous printing: %+v", result.TopMatches)
	}
}

func TestFingerprintScoresDoNotFavorSecondCandidateAtSameDistance(t *testing.T) {
	t.Parallel()
	cards := []models.Card{{ID: "a"}, {ID: "b"}}
	candidates := make(map[string]*CardMatch)
	addFingerprintCandidates(candidates, &MatchResult{
		HighConfidence: &cards[0], BestDistance: 2,
		Potential: []FingerprintMatch{{Card: &cards[0], Distance: 2}, {Card: &cards[1], Distance: 2}},
	}, NewFingerprintService(nil))
	if candidates["a"].FingerprintScore.Score != candidates["b"].FingerprintScore.Score {
		t.Fatalf("equal image distances received unequal confidence: %+v versus %+v", candidates["a"].FingerprintScore, candidates["b"].FingerprintScore)
	}
}

func runPrintingDetection(t *testing.T, cards []models.Card, distances []int, text, hint string) *DetectionResult {
	t.Helper()
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.fingerprintRunner = func(context.Context, []byte, []models.Card, *ScanScope) (*MatchResult, error) {
		matches := make([]FingerprintMatch, len(cards))
		for index := range cards {
			matches[index] = FingerprintMatch{Card: &cards[index], Distance: distances[index]}
		}
		return NewFingerprintService(nil).resultFromPotential(matches), nil
	}
	pipeline.ocrRunner = func(context.Context, []byte, []models.Card, string) (string, string, []byte, error) {
		return text, hint, nil, nil
	}
	result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{
		Image: []byte("image"), Cards: cards,
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
