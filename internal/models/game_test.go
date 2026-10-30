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
		"Star Wars: Unlimited": "star_wars_unlimited",
		"swu":                 "star_wars_unlimited",
		"Flesh and Blood":     "flesh_and_blood",
		"fab":                 "flesh_and_blood",
		"Digimon":             "digimon",
		"digimon-card-game":   "digimon",
		"Grand Archive":       "grand_archive",
		"gatcg":               "grand_archive",
	}
	for input, want := range tests {
		if got := NormalizeGame(input); got != want {
			t.Errorf("NormalizeGame(%q) = %q, want %q", input, got, want)
		}
	}
}
