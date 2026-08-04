package models

import "testing"

func TestParseTCG(t *testing.T) {
	t.Parallel()

	tests := map[string]TCG{
		"Pokemon TCG":    TCGPokemon,
		"MTG":            TCGMagic,
		"One Piece":      TCGOnePiece,
		"Disney Lorcana": TCGLorcana,
		"Weiss Schwarz":  TCGWeissSchwarz,
		"Yu-Gi-Oh!":      TCGYuGiOh,
	}
	for input, want := range tests {
		got, err := ParseTCG(input)
		if err != nil {
			t.Fatalf("ParseTCG(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("ParseTCG(%q) = %q, want %q", input, got, want)
		}
	}

	if _, err := ParseTCG("unsupported"); err == nil {
		t.Fatal("ParseTCG accepted an unsupported catalog")
	}
}

func TestParseLanguageAndTesseractCode(t *testing.T) {
	t.Parallel()

	tests := map[string]Language{
		"eng":     LanguageEnglish,
		"jp":      LanguageJapanese,
		"deu":     LanguageGerman,
		"French":  LanguageFrench,
		"chi_sim": LanguageChineseSimplified,
		"zh-Hant": LanguageChineseTraditional,
		"kor":     LanguageKorean,
	}
	for input, want := range tests {
		got, err := ParseLanguage(input)
		if err != nil {
			t.Fatalf("ParseLanguage(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("ParseLanguage(%q) = %q, want %q", input, got, want)
		}
		if got.TesseractCode() == "" {
			t.Errorf("TesseractCode(%q) is empty", got)
		}
	}

	if _, err := ParseLanguage("klingon"); err == nil {
		t.Fatal("ParseLanguage accepted an unsupported language")
	}
}

func TestCardIsCatalogActive(t *testing.T) {
	t.Parallel()

	active, inactive := true, false
	if !(Card{}).IsCatalogActive() {
		t.Fatal("legacy card without activity metadata should be active")
	}
	if !(Card{CatalogActive: &active}).IsCatalogActive() {
		t.Fatal("active catalog card should be active")
	}
	if (Card{CatalogActive: &inactive}).IsCatalogActive() {
		t.Fatal("inactive catalog card should not be active")
	}
}
