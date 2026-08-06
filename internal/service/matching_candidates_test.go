package service

import (
	"fmt"
	"testing"

	"pokget/internal/models"
)

func TestNormalizeMatchTextUnicodeAndPunctuation(t *testing.T) {
	t.Parallel()

	if got, want := normalizeMatchText("  Pok\u00e9mon:  M\u00c9W-ex  "), "pokemon mew ex"; got != want {
		t.Fatalf("normalizeMatchText() = %q, want %q", got, want)
	}
	if got, want := normalizeMatchText("\u30d6\u30e9\u30c3\u30ad\u30fcex"), "\u30d6\u30e9\u30c3\u30ad\u30fcex"; got != want {
		t.Fatalf("CJK normalization = %q, want %q", got, want)
	}
}

func TestRankCandidatesUsesSetAndCollectorNumber(t *testing.T) {
	t.Parallel()

	cards := []models.Card{
		{ID: "sv3-125", Name: "Charizard ex", Set: "Obsidian Flames", SetCode: "SV3", CollectorNumber: "125"},
		{ID: "sv4-223", Name: "Charizard ex", Set: "Paradox Rift", SetCode: "SV4", CollectorNumber: "223"},
	}
	ranked := rankCandidates("Charizard ex SV4 223 Paradox Rift", cards, len(cards))
	if len(ranked) != 2 || ranked[0].Card.ID != "sv4-223" {
		t.Fatalf("ranked candidates = %+v, want sv4-223 first", ranked)
	}
	if ranked[0].Score <= ranked[1].Score {
		t.Fatalf("exact set/collector evidence did not break duplicate-name tie: %+v", ranked)
	}
}

func TestRankCandidatesUsesLocalizedName(t *testing.T) {
	t.Parallel()

	cards := []models.Card{
		{ID: "jp-1", Name: "Umbreon", LocalizedNames: []string{"\u30d6\u30e9\u30c3\u30ad\u30fc"}},
		{ID: "jp-2", Name: "Espeon", LocalizedNames: []string{"\u30a8\u30fc\u30d5\u30a3"}},
	}
	ranked := rankCandidates("\u30d6\u30e9\u30c3\u30ad\u30fc", cards, 2)
	if ranked[0].Card.ID != "jp-1" || ranked[0].Score < defaultLLMMinEvidence {
		t.Fatalf("localized ranking = %+v", ranked)
	}
}

func TestRankCandidatesDeterministicPrintingIDTie(t *testing.T) {
	t.Parallel()

	cards := []models.Card{
		{ID: "printing-b", Name: "Shared Name"},
		{ID: "printing-a", Name: "Shared Name"},
	}
	for iteration := 0; iteration < 100; iteration++ {
		ranked := rankCandidates("Shared Name", cards, 2)
		if ranked[0].Card.ID != "printing-a" || ranked[1].Card.ID != "printing-b" {
			t.Fatalf("iteration %d ranking = %q, %q", iteration, ranked[0].Card.ID, ranked[1].Card.ID)
		}
	}
}

func BenchmarkRankCandidates(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		cards := make([]models.Card, count)
		for index := range cards {
			cards[index] = models.Card{
				ID: fmt.Sprintf("sv4-%05d", index), Name: fmt.Sprintf("Card Name %d", index),
				SetCode: "SV4", CollectorNumber: fmt.Sprintf("%d", index),
			}
		}
		b.Run(fmt.Sprintf("cards_%d", count), func(b *testing.B) {
			for b.Loop() {
				_ = rankCandidates("Card Name 742 SV4 742", cards, 20)
			}
		})
	}
}
