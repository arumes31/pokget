package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"pokget/internal/models"
)

func TestRankLocalMatchesHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	matches, err := rankLocalMatchesContext(ctx, []ocrEvidence{{Text: "Furret"}}, []models.Card{{ID: "furret-1", Name: "Furret"}}, "eng")
	if !errors.Is(err, context.Canceled) || matches != nil {
		t.Fatalf("canceled matching returned %v, %v", matches, err)
	}
}

func TestFuzzySubstringMatchBoundsAllocations(t *testing.T) {
	text := strings.Repeat("furret scratch quick attack retreat energy weakness resistance ", 8)
	allocations := testing.AllocsPerRun(3, func() {
		if fuzzySubstringMatch(text, "charizard ex") {
			t.Fatal("unrelated card name matched")
		}
	})
	if allocations > 5 {
		t.Fatalf("full card text comparison allocated %.0f times; want at most 5", allocations)
	}
}

// Keep the prior full-distance implementation as an independent oracle for
// the optimized threshold comparison, including Unicode and window lengths.
func TestFuzzySubstringMatchPreservesWindowSemantics(t *testing.T) {
	random := rand.New(rand.NewPCG(18, 91))
	alphabet := []rune("abcd あいうえ")
	for range 800 {
		target := make([]rune, 4+random.IntN(9))
		text := make([]rune, len(target)+random.IntN(12))
		for index := range target {
			target[index] = alphabet[random.IntN(len(alphabet))]
		}
		for index := range text {
			text[index] = alphabet[random.IntN(len(alphabet))]
		}
		targetText := strings.TrimSpace(string(target))
		if len([]rune(targetText)) < 4 {
			continue
		}
		want := referenceFuzzySubstringMatch(string(text), targetText)
		if got := fuzzySubstringMatch(string(text), targetText); got != want {
			t.Fatalf("comparison %q / %q = %v, want %v", text, targetText, got, want)
		}
	}
}

func referenceFuzzySubstringMatch(text, target string) bool {
	if strings.Contains(text, target) {
		return true
	}
	textRunes, targetRunes := []rune(text), []rune(target)
	if len(textRunes) < len(targetRunes) {
		return false
	}
	distance := 1
	if len(targetRunes) > 7 {
		distance = min(len(targetRunes)/4, 3)
	}
	lengths := []int{len(targetRunes)}
	if len(targetRunes) > 4 {
		lengths = append(lengths, len(targetRunes)-1, len(targetRunes)+1)
	}
	for _, length := range lengths {
		for start := 0; start+length <= len(textRunes); start++ {
			if levenshtein(string(textRunes[start:start+length]), target) <= distance {
				return true
			}
		}
	}
	return false
}

func BenchmarkRankLocalMatchesCatalog(b *testing.B) {
	// Repeated names represent separate catalog printings, not duplicate IDs.
	cards := make([]models.Card, 24000)
	for index := range cards {
		cards[index] = models.Card{ID: fmt.Sprintf("card_%064x", index), Name: fmt.Sprintf("Unrelated Creature %d", index%1000), Game: "pokemon", Language: "en"}
	}
	cards[0].Name = "Furret"
	text := strings.Repeat("Furret scratch quick attack retreat energy weakness resistance ", 5)
	evidence := []ocrEvidence{{Text: text, Pass: "gray", Role: "full", Quality: 80}, {Text: text, Pass: "blue", Role: "full", Quality: 80}, {Text: "Furret", Pass: "name", Role: "name", Quality: 6}}
	for _, count := range []int{1000, 24000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if matches := rankLocalMatches(evidence, cards[:count], "eng"); len(matches) != 1 || matches[0].Card.Name != "Furret" {
					b.Fatalf("unexpected catalog matches: %d", len(matches))
				}
			}
		})
	}
}
