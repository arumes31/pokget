package service

import (
	"fmt"
	"testing"

	"pokget/internal/claudedetect"
	"pokget/internal/models"
)

// setTotalBenchmarkCards builds a 20k-row scope where 200 sets each contain a
// "Pikachu" #025; only set s137 is officially 165 cards large.
func setTotalBenchmarkCards() []models.Card {
	cards := make([]models.Card, 0, 20_000)
	for set := 0; set < 200; set++ {
		total := 100 + set
		if set == 137 {
			total = 165
		}
		if total == 165 && set != 137 {
			total = 400
		}
		code := fmt.Sprintf("s%d", set)
		for number := 1; number <= 100; number++ {
			name := fmt.Sprintf("Filler %d-%d", set, number)
			if number == 25 {
				name = "Pikachu"
			}
			cards = append(cards, models.Card{
				ID: fmt.Sprintf("%s-%03d", code, number), Name: name, CollectorNumber: fmt.Sprintf("%03d", number),
				Set: "Set " + code, SetCode: code, Language: "en", Game: "pokemon", SetTotal: total,
			})
		}
	}
	return cards
}

// linearSetTotalEvidence is the former catalog scan, kept as the reference the
// index must reproduce.
func linearSetTotalEvidence(cards []models.Card, product models.Card, total int) setTotalVerdict {
	for index := range cards {
		card := cards[index]
		if card.SetTotal > 0 && card.IsCatalogActive() && sameProduct(card, product) {
			if card.SetTotal == total {
				return setTotalConsistent
			}
			return setTotalInconsistent
		}
	}
	return setTotalUnknown
}

func TestSetTotalIndexMatchesLinearScan(t *testing.T) {
	inactive := false
	cards := []models.Card{
		{Set: "Shared", SetTotal: 0},                    // no published size
		{Set: "Shared", SetCode: "aaa", SetTotal: 100},  // code + generic name
		{Set: "Shared", SetTotal: 150},                  // code-less row of the name
		{Set: "Shared", SetCode: "bbb", SetTotal: 120},  // other code, same name
		{Set: "Other", SetCode: "ccc", SetTotal: 90, CatalogActive: &inactive},
		{Set: "Other", SetCode: "ccc", SetTotal: 91},    // first active ccc row
		{Set: "Late", SetCode: "ddd", SetTotal: 60},     // code row before the code-less one
		{Set: "Late", SetTotal: 61},
		{Set: "Ünïcode Set", SetTotal: 70},
		{SetCode: "eee", SetTotal: 80},                  // code only
	}
	totals := newSetTotalIndex()
	for index, card := range cards {
		if card.IsCatalogActive() {
			totals.add(index, normalizeMatchText(card.SetCode), normalizeMatchText(card.Set), card.SetTotal)
		}
	}
	products := []models.Card{
		{Set: "Shared"}, {Set: "Shared", SetCode: "aaa"}, {Set: "Shared", SetCode: "bbb"},
		{Set: "Shared", SetCode: "zzz"}, {SetCode: "aaa"}, {Set: "Other"}, {SetCode: "ccc"},
		{Set: "Late", SetCode: "ddd"}, {Set: "Late", SetCode: "xxx"}, {Set: "Late"},
		{Set: "unicode set"}, {SetCode: "EEE"}, {Set: "Missing"}, {},
	}
	for _, product := range products {
		for _, total := range []int{60, 61, 70, 80, 91, 100, 120, 150} {
			if got, want := totals.evidence(product, total), linearSetTotalEvidence(cards, product, total); got != want {
				t.Errorf("product %+v total %d: index %d, linear scan %d", product, total, got, want)
			}
		}
	}
}

func BenchmarkMatchCardsWithClaudeSetTotal(b *testing.B) {
	cards := setTotalBenchmarkCards()
	info := &claudedetect.DetectedCardInfo{
		Game: "pokemon", Name: "Pikachu", CollectorNumber: "025/165", Language: "en", Confidence: 0.95,
	}
	if matches := matchCardsWithClaude(info, cards); len(matches) == 0 || matches[0].Card.ID != "s137-025" {
		b.Fatalf("set total must pick s137-025, got %+v", matches)
	}
	for b.Loop() {
		matchCardsWithClaude(info, cards)
	}
}
