package service

import "testing"

func TestNormalizeMatchTextUnicodeAndPunctuation(t *testing.T) {
	t.Parallel()

	if got, want := normalizeMatchText("  Pokémon:  MÉW-ex  "), "pokemon mew ex"; got != want {
		t.Fatalf("normalizeMatchText() = %q, want %q", got, want)
	}
	if got, want := normalizeMatchText("ブラッキーex"), "ブラッキーex"; got != want {
		t.Fatalf("CJK normalization = %q, want %q", got, want)
	}
}
