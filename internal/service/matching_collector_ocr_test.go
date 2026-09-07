package service

import (
	"context"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"pokget/internal/models"
)

const liveLuffyCollectorOCR = "Monkey.D.Luffy o f Supernovas/Straw Hat Crew STo1-oo1u6 é L . e Monkey.D.Luffy vl Supernovas/Straw Hat Crew s‘ro1~oo1u€"

func TestLiveLuffyCollectorOCRKeepsCorrectPrintingsInShortlist(t *testing.T) {
	cards := make([]models.Card, 0, 42)
	for index := range 40 {
		cards = append(cards, models.Card{ID: fmt.Sprintf("card_000-%02d", index), Name: "Monkey.D.Luffy", CollectorNumber: fmt.Sprintf("OP05-%03d", index+1), Game: "one_piece", Language: "en"})
	}
	for _, id := range []string{"card_zzz-base", "card_zzz-alternate"} {
		cards = append(cards, models.Card{ID: id, Name: "Monkey.D.Luffy", CollectorNumber: "ST01-001", SetCode: "ST-01", Game: "one_piece", Language: "en"})
	}
	candidates := resolveOCRCandidates(cards[0].ID, liveLuffyCollectorOCR, cards)
	if len(candidates) < 2 || candidates[0].Card.CollectorNumber != "ST01-001" || candidates[1].Card.CollectorNumber != "ST01-001" {
		t.Fatalf("actual OCR excluded the correct collector family: %+v", candidates)
	}
	if uniqueOCRPrintingID(candidates) != "" {
		t.Fatal("repaired OCR must not claim a unique alternate-art identity")
	}
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.fingerprintRunner = func(context.Context, []byte, []models.Card, *ScanScope) (*MatchResult, error) { return nil, nil }
	pipeline.ocrRunner = func(context.Context, []byte, []models.Card, string) (string, string, []byte, error) {
		return liveLuffyCollectorOCR, cards[0].ID, nil, nil
	}
	result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{Image: []byte("stub image"), Cards: cards, Scope: ScanScope{TCG: models.TCGOnePiece, Language: models.LanguageEnglish}})
	if err != nil || result.BestMatchCard() == nil || result.BestMatchCard().CollectorNumber != "ST01-001" || !result.BestMatchNeedsReview() {
		t.Fatalf("repaired collector did not remain a reviewable correct family: result=%+v error=%v", result, err)
	}
}

func TestNoisyCollectorEvidencePreservesNumericBoundaries(t *testing.T) {
	for _, test := range []struct {
		input, collector string
		want             bool
	}{
		{"STo1-oo1u6", "ST01-001", true},
		{"ST01-001u", "ST01-001", true},
		{"ST o1 oo1u6", "ST01-001", true},
		{"OPo4-o34u6", "OP04-034", true},
		{"ST01-0012", "ST01-001", false},
		{"ST01-001o6", "ST01-001", false},
		{"ST01-0001u6", "ST01-001", false},
		{"XST01-001u6", "ST01-001", false},
		{"ST01-001abc", "ST01-001", false},
		{"SR01-001u6", "ST01-001", false},
		{"123u6", "123", false},
		{"1234", "123", false},
		{"12 34", "123", false},
	} {
		t.Run(test.input, func(t *testing.T) {
			candidate := rankCandidates(test.input, []models.Card{{ID: "canonical-card", Name: "Unrelated", CollectorNumber: test.collector}}, 1)[0]
			got := slices.Contains(candidate.Reasons, "ocr_collector_number")
			if got != test.want {
				t.Fatalf("collector evidence=%t, want %t: %+v", got, test.want, candidate)
			}
			if !test.want && slices.Contains(candidate.Reasons, "collector_number") {
				t.Fatalf("numeric boundary produced strict evidence: %+v", candidate)
			}
		})
	}
}

func TestPrintedIdentifierTokenComparisonPreservesConcatenationSemantics(t *testing.T) {
	reference := func(tokens []string, value string) bool {
		value = strings.ReplaceAll(value, " ", "")
		if value == "" {
			return false
		}
		for start := range tokens {
			joined := ""
			for _, token := range tokens[start:] {
				joined += token
				if joined == value {
					return true
				}
				if len(joined) >= len(value) {
					break
				}
			}
		}
		return false
	}
	random := rand.New(rand.NewSource(42))
	alphabet := []string{"", "12", "3", "4", "st", "01", "001", "α", "あ", "い"}
	for range 2000 {
		tokens := make([]string, random.Intn(10))
		for index := range tokens {
			tokens[index] = alphabet[random.Intn(len(alphabet))]
		}
		value := alphabet[random.Intn(len(alphabet))] + alphabet[random.Intn(len(alphabet))]
		if got, want := containsPrintedIdentifierTokens(tokens, value), reference(tokens, value); got != want {
			t.Fatalf("tokens=%q value=%q result=%t, want %t", tokens, value, got, want)
		}
	}
}
