package models

import "testing"

func TestNormalizeGame(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"Pokemon":             "pokemon",
		"Pokémon":             "pokemon",
		"MTG":                 "magic",
		"Magic the Gathering": "magic",
		"One Piece":           "one_piece",
		"one-piece":           "one_piece",
		"one_piece":           "one_piece",
		"Disney Lorcana":      "lorcana",
		"Weiss Schwarz":       "weiss_schwarz",
		"weiss-schwarz":       "weiss_schwarz",
		"Yu-Gi-Oh!":           "yugioh",
		"yugioh":              "yugioh",
	}
	for input, want := range tests {
		if got := NormalizeGame(input); got != want {
			t.Errorf("NormalizeGame(%q) = %q, want %q", input, got, want)
		}
	}
}
