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
	}
}
