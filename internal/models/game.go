package models

import "strings"

// NormalizeGame converts user-facing and legacy game labels to catalog slugs.
func NormalizeGame(game string) string {
	normalized := strings.ToLower(strings.TrimSpace(game))
	normalized = strings.NewReplacer("-", "_", " ", "_").Replace(normalized)
	for strings.Contains(normalized, "__") {
		normalized = strings.ReplaceAll(normalized, "__", "_")
	}
	normalized = strings.Trim(normalized, "_")

	switch normalized {
	case "pokemon", "pokémon", "pokemon_tcg":
		return "pokemon"
	case "magic", "mtg", "magic_the_gathering":
		return "magic"
	case "one_piece", "onepiece":
		return "one_piece"
	case "lorcana", "disney_lorcana":
		return "lorcana"
	case "weiss_schwarz", "weissschwarz", "weiss":
		return "weiss_schwarz"
	case "yugioh", "yu_gi_oh", "yu_gi_oh!":
		return "yugioh"
	default:
		return normalized
	}
}
