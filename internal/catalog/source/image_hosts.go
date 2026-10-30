package source

// DefaultImageHosts returns the exact HTTPS hostnames used by the primary
// no-auth catalog adapters. The image processor rejects every hostname that is
// not listed here, including redirect targets.
func DefaultImageHosts() map[string][]string {
	return map[string][]string{
		"tcgdex":            {"assets.tcgdex.net"},
		"scryfall":          {"cards.scryfall.io"},
		"onepiece_official": {"en.onepiece-cardgame.com"},
		"lorcanajson":       {"api.lorcana.ravensburger.com"},
		"weiss_official":    {"en.ws-tcg.com", "ws-tcg.com", "www.ws-tcg.com"},
		"ygoprodeck":        {"images.ygoprodeck.com"},
		"swudb":             {"cdn.swu-db.com"},
		"fabcube":           {"storage.googleapis.com"},
		"digimoncardio":     {"images.digimoncard.io"},
		"gatcg":             {"api.gatcg.com"},
		"altered":           {"cdn.alteredcore.org", "altered-prod-eu.s3.amazonaws.com", "img.altered-db.com"},
		"sorcery":           {"d27a44hjr9gen3.cloudfront.net", "fourcores-home.netlify.app", "sorcerytcg.com", "curiosa.io"},
		"dbs_official":              {"www.dbs-cardgame.com", "dbs-cardgame.com"},
		"dbs_fusion_world_official": {"www.dbs-cardgame.com", "dbs-cardgame.com", "www.dbs-fusionworld.com", "dbs-fusionworld.com"},
		"unionarena_official":       {"www.unionarena-tcg.com", "unionarena-tcg.com"},
		"gundam_official":           {"www.gundam-gcg.com", "gundam-gcg.com", "images.gundam-tcg.com"},
		"battlespirits_official":    {"battlespirits-saga.com", "www.battlespirits-saga.com"},
		"wowtcg":                    {"wow.tcgbrowser.com", "tcgbrowser.com", "tcgplayer-cdn.tcgplayer.com"},
	}
}
