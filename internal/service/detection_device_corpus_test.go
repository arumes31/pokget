package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"pokget/internal/models"
)

func TestDeviceOCRCorpusPreservesReviewCandidatesWithoutServerImages(t *testing.T) {
	data, err := os.ReadFile("../../testdata/ocr/device-selection.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Kind, Language, Game, OCR, Expected string
		Candidates                                []struct {
			ID     string `json:"card_id"`
			Name   string `json:"name"`
			Number string `json:"collector_number"`
		}
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		if !strings.Contains(tc.Kind, "device_tesseract") {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			game, err := models.ParseTCG(tc.Game)
			if err != nil {
				t.Fatal(err)
			}
			lang, err := models.ParseLanguage(tc.Language)
			if err != nil {
				t.Fatal(err)
			}
			cards := make([]models.Card, 0, len(tc.Candidates))
			for _, c := range tc.Candidates {
				cards = append(cards, models.Card{ID: "catalog-" + c.ID, Name: c.Name, CollectorNumber: c.Number, Game: tc.Game, Language: tc.Language})
			}
			result, err := NewDetectionPipeline(nil, nil).DetectTextScoped(context.Background(), TextDetectionRequest{
				Text: tc.OCR, Cards: cards, Scope: ScanScope{TCG: game, Language: lang},
			})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, match := range result.TopMatches {
				found = found || match.Card.ID == "catalog-"+tc.Expected
				if !match.NeedsReview {
					t.Fatal("device evidence must not auto-confirm a printing")
				}
			}
			if !found {
				t.Fatalf("correct printing %q was lost from review candidates", "catalog-"+tc.Expected)
			}
			if result.BestMatchID() != "catalog-"+tc.Expected && !deviceTextNeedsLLM(resolveOCRCandidates("", tc.OCR, cards)) {
				t.Fatal("an unresolved identity must be eligible for bounded model selection")
			}
		})
	}
}
