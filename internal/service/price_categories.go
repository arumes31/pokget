package service

import "sort"

// PriceCategory is one marketplace category that feeds catalog prices. Language
// is empty for categories that price every language of a game.
type PriceCategory struct {
	Game     string
	Language string
	Category int
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
		categories = append(categories, PriceCategory{
			Game:     extra.Game,
			Language: extra.Language,
			Category: extra.Category,
		})
	}
	sort.Slice(categories, func(i, j int) bool {
		if categories[i].Game != categories[j].Game {
			return categories[i].Game < categories[j].Game
		}
		if categories[i].Language != categories[j].Language {
			return categories[i].Language < categories[j].Language
		}
		return categories[i].Category < categories[j].Category
	})
	return categories
}
