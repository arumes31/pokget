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
		"Dragon Ball Super":   "dragon_ball_super",
		"dbs":                 "dragon_ball_super",
		"dbs_masters":         "dragon_ball_super",
		"DBS Fusion World":    "dbs_fusion_world",
		"fusion_world":        "dbs_fusion_world",
		"Union Arena":         "union_arena",
		"unionarena":          "union_arena",
		"Gundam":              "gundam",
		"gundam-tcg":          "gundam",
		"Battle Spirits Saga": "battlespirits_saga",
		"bss":                 "battlespirits_saga",
		"Altered":             "altered",
		"altered_tcg":         "altered",
		"Sorcery":             "sorcery",
		"Sorcery: Contested Realm": "sorcery",
		"World of Warcraft":   "world_of_warcraft",
		"WoW TCG":             "world_of_warcraft",
		"wow":                 "world_of_warcraft",
		"FFTCG":               "fftcg",
		"Final Fantasy":       "fftcg",
		"Godzilla":            "godzilla",
		"Shadowverse: Evolve": "shadowverse_evolve",
		"UniVersus":           "universus",
		"Cardfight!! Vanguard": "vanguard",
		"hololive":            "hololive",
		"Duel Masters":        "duel_masters",
		"CookieRun: Braverse": "cookie_run",
		"Force of Will":       "force_of_will",
		"WIXOSS":              "wixoss",
		"Alpha Clash":         "alpha_clash",
		"Riftbound":           "riftbound",
	}
	for input, want := range tests {
		if got := NormalizeGame(input); got != want {
			t.Errorf("NormalizeGame(%q) = %q, want %q", input, got, want)
		}
	}
}
