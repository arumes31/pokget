package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"pokget/internal/models"
)

func TestPipelineArtworkReferencesRejectEarlierTruncatedGroup(t *testing.T) {
	cards := make([]models.Card, 0, 14)
	for index := range 9 {
		id := fmt.Sprintf("art-%02d", index*2)
		cards = append(cards, models.Card{ID: id, Name: "Foxy", Set: "Shared Set", SetCode: "OP07", CollectorNumber: "OP07-071", Language: "en", Game: "one_piece", Variant: "Normal", ImageURL: "https://en.onepiece-cardgame.com/images/cardlist/card/" + id + ".png"})
	}
	for index := range 5 {
		id := fmt.Sprintf("art-%02d", index*2+1)
		cards = append(cards, models.Card{ID: id, Name: "Foxy", Set: "Shared Set", SetCode: "OP07", CollectorNumber: "OP07-059", Language: "en", Game: "one_piece", Variant: "Normal", ImageURL: "https://en.onepiece-cardgame.com/images/cardlist/card/" + id + ".png"})
	}
	var primaryCalls atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls.Add(1)
		var request struct {
			Messages []struct {
				Content []struct {
					Type string
					Text string
				}
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		labels := make(map[string]bool)
		for _, message := range request.Messages {
			for _, part := range message.Content {
				if !strings.Contains(part.Text, "reference_card_id") {
					continue
				}
				var label struct {
					CardID string `json:"reference_card_id"`
				}
				if err := json.Unmarshal([]byte(part.Text), &label); err != nil {
					t.Error(err)
				}
				labels[label.CardID] = true
			}
		}
		if len(labels) != 5 {
			t.Errorf("reference count=%d, want complete five-art group", len(labels))
		}
		for index := range 9 {
			if labels[fmt.Sprintf("art-%02d", index*2)] {
				t.Error("pipeline attached part of a nine-art group truncated before the reference builder")
			}
		}
		for index := range 5 {
			if !labels[fmt.Sprintf("art-%02d", index*2+1)] {
				t.Error("a complete eligible artwork group was omitted")
			}
		}
		writePrimaryLLMResponse(w, `{"card_id":"art-01"}`)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected fallback") }))
	defer fallback.Close()
	pipeline := NewDetectionPipeline(nil, primarySecurityService(t, primary.URL, fallback.URL, nil))
	pipeline.fingerprintRunner = func(context.Context, []byte, []models.Card, *ScanScope) (*MatchResult, error) { return nil, nil }
	pipeline.ocrRunner = func(context.Context, []byte, []models.Card, string) (string, string, []byte, error) {
		return "Foxy", "Foxy", []byte("\x89PNG\r\n\x1a\n"), nil
	}
	result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{Image: []byte("stub image"), Cards: cards, Scope: ScanScope{TCG: models.TCGOnePiece, Language: models.LanguageEnglish}})
	if err != nil || result == nil || result.BestMatchID() != "art-01" || !result.BestMatchNeedsReview() || primaryCalls.Load() != 1 {
		t.Fatalf("result=%+v error=%v primary calls=%d", result, err, primaryCalls.Load())
	}
}
