package models

import "strings"

// NormalizeGame converts user-facing and legacy game labels to catalog slugs.
func NormalizeGame(game string) string {
	normalized := strings.ToLower(strings.TrimSpace(game))
	normalized = strings.NewReplacer("-", "_", " ", "_", ":", "").Replace(normalized)
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
	case "star_wars", "star_wars_unlimited", "swu", "starwars":
		return "star_wars_unlimited"
	case "flesh_and_blood", "fab":
		return "flesh_and_blood"
	case "digimon", "digimon_card_game":
		return "digimon"
	case "grand_archive", "gatcg":
		return "grand_archive"
	case "dragon_ball_super", "dragonball_super", "dragon_ball", "dbs", "dbs_masters", "dbs_cg":
		return "dragon_ball_super"
	case "dbs_fusion_world", "fusion_world", "dragon_ball_super_fusion_world", "dbsfw":
		return "dbs_fusion_world"
	case "union_arena", "unionarena", "ua":
		return "union_arena"
	case "gundam", "gundam_tcg", "gundam_card_game", "gcg":
		return "gundam"
	case "battlespirits_saga", "battle_spirits_saga", "battlespirits", "battle_spirits", "bss":
		return "battlespirits_saga"
	case "altered", "altered_tcg":
		return "altered"
	case "sorcery", "sorcery_contested_realm", "sorcerytcg":
		return "sorcery"
	case "world_of_warcraft", "wow_tcg", "wow", "warcraft":
		return "world_of_warcraft"
	default:
		return normalized
	}
}
