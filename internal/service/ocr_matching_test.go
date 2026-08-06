package service

import (
	"math/rand/v2"
	"pokget/internal/models"
	"testing"
	"unicode/utf8"
)

func TestFuzzySubstringMatchShortLatinNameRequiresWholeToken(t *testing.T) {
	t.Parallel()

	if fuzzySubstringMatch("TRAINER", "N") {
		t.Fatal("embedded one-letter name matched OCR text")
	}
	if !fuzzySubstringMatch("TRAINER N 123", "N") {
		t.Fatal("whole-token one-letter name did not match OCR text")
	}
	if fuzzySubstringMatch("STONE", "ONE") {
		t.Fatal("embedded three-letter name matched OCR text")
	}
	if !fuzzySubstringMatch("ONE / 001", "ONE") {
		t.Fatal("whole-token three-letter name did not match OCR text")
	}
}

func TestNormalizeOCRTextNFKCAndLanguageRules(t *testing.T) {
	tests := []struct {
		name string
		text string
		lang string
		want string
	}{
		{name: "compatibility characters", text: "Ｐｉｋａｃｈｕ—ＶＭＡＸ", lang: "eng", want: "pikachu vmax"},
		{name: "Latin whitespace", text: "  One\tPiece / Card  ", lang: "eng", want: "one piece card"},
		{name: "Japanese OCR spaces", text: "ピカ チュウ\nＶ", lang: "jpn", want: "ピカチュウv"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeOCRText(test.text, test.lang); got != test.want {
				t.Fatalf("normalizeOCRText() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRankLocalMatchesReturnsDeterministicIdentifier(t *testing.T) {
	cards := []models.Card{
		{ID: "sv1-025", Name: "Pikachu", Game: "Pokemon", Language: "en"},
		{ID: "base-004", Name: "Charizard", Game: "Pokemon", Language: "en"},
		{ID: "sv1-025", Name: "Pikachu", Game: "Pokemon", Language: "en"},
	}
	evidence := []ocrEvidence{
		{Text: "Pikachu", Pass: "name", Role: "name", Quality: 8},
		{Text: "SV1 / 025", Pass: "number", Role: "identifier", Quality: 5},
	}
	for range 20 {
		rand.Shuffle(len(cards), func(i, j int) { cards[i], cards[j] = cards[j], cards[i] })
		if got := localMatchResult(evidence, cards, "eng"); got != "sv1-025" {
			t.Fatalf("localMatchResult() = %q, want card ID", got)
		}
	}
}

func TestRankLocalMatchesCJKRequiresCorroboration(t *testing.T) {
	cards := []models.Card{{ID: "s8a-001", Name: "ピカチュウ", Game: "pokemon", Language: "ja"}}
	onePass := []ocrEvidence{{Text: "ピカ チュウ", Pass: "gray", Role: "name", Quality: 10}}
	if got := localMatchResult(onePass, cards, "jpn"); got != "Unknown Card" {
		t.Fatalf("single CJK name pass matched %q", got)
	}
	twoPasses := append(onePass, ocrEvidence{Text: "ピカチュウ", Pass: "binary", Quality: 8})
	if got := localMatchResult(twoPasses, cards, "jpn"); got != "s8a-001" {
		t.Fatalf("corroborated CJK match = %q, want s8a-001", got)
	}
	identifier := []ocrEvidence{{Text: "S8A 001", Pass: "number", Role: "identifier", Quality: 4}}
	if got := localMatchResult(identifier, cards, "jpn"); got != "s8a-001" {
		t.Fatalf("CJK identifier match = %q, want s8a-001", got)
	}
}

func FuzzNormalizeOCRText(f *testing.F) {
	f.Add("Ｐｉｋａｃｈｕ", "eng")
	f.Add("ピカ チュウ", "jpn")
	f.Fuzz(func(t *testing.T, input, lang string) {
		first := normalizeOCRText(input, lang)
		second := normalizeOCRText(first, lang)
		if first != second {
			t.Fatalf("normalization is not idempotent: %q != %q", first, second)
		}
		if !utf8.ValidString(first) {
			t.Fatal("normalization returned invalid UTF-8")
		}
	})
}
