package service

import (
	"sort"
	"strings"
)

// PriceCategory is one marketplace category that feeds catalog prices. Languages
// is empty for categories that price every language of a game.
type PriceCategory struct {
	Game      string
	Languages []string
	Category  int
}

// marketPriceGames lists the games with a price source outside the TCGCSV
// categories: Scryfall covers Magic and YGOPRODeck covers Yu-Gi-Oh.
var marketPriceGames = map[string]struct{}{
	"magic":  {},
	"yugioh": {},
}

// HasMarketPriceSource reports whether any configured source can price a game. A
// game without one (Altered, Duel Masters) is not a gap the worker can close, so
// the UI must not label its price "unavailable" like a temporary miss.
func HasMarketPriceSource(game string) bool {
	normalized := strings.ToLower(strings.TrimSpace(game))
	if _, ok := marketPriceGames[normalized]; ok {
		return true
	}
	for _, category := range PriceCategories() {
		if category.Game == normalized {
			return true
		}
	}
	return false
}

// PriceCategories returns every TCGplayer category the bulk price source reads,
// sorted by game and language. It exists so operators and tooling can check which
// game/language combination is covered by a marketplace.
func PriceCategories() []PriceCategory {
	categories := make([]PriceCategory, 0, len(tcgPlayerPriceCategories)+len(tcgPlayerLanguageCategories))
	for game, category := range tcgPlayerPriceCategories {
		categories = append(categories, PriceCategory{Game: game, Category: category})
	}
	for _, extra := range tcgPlayerLanguageCategories {
		categories = append(categories, PriceCategory(extra))
	}
	sort.Slice(categories, func(i, j int) bool {
		if categories[i].Game != categories[j].Game {
			return categories[i].Game < categories[j].Game
		}
		left, right := strings.Join(categories[i].Languages, ","), strings.Join(categories[j].Languages, ",")
		if left != right {
			return left < right
		}
		return categories[i].Category < categories[j].Category
	})
	return categories
}
