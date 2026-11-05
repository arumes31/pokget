package service

import (
	"testing"

	"pokget/internal/claudedetect"
	"pokget/internal/models"
)

// A genuine photo of the Wizards Black Star promo Mew #8 stayed in review at
// confidence 99 because "Rocket's Mewtwo" #8 (a different set) contains the
// detected name "Mew". A name that merely contains the detected name in another
// set is a different card, not a variant printing of the same one.
func TestClaudeDetection_NamePrefixInOtherSetDoesNotBlockExactMatch(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game: "pokemon", Name: "Mew", CollectorNumber: "8",
		Set: "Wizards Black Star Promos", Language: "eng",
		Variant: "Black Star Promo", Confidence: 0.9,
	}
	cards := []models.Card{
		{ID: "mew8", Name: "Mew", CollectorNumber: "8", Set: "Wizards Black Star Promos", SetCode: "basep", Language: "en", Game: "pokemon", Variant: "Normal"},
		{ID: "mew9", Name: "Mew", CollectorNumber: "9", Set: "Wizards Black Star Promos", SetCode: "basep", Language: "en", Game: "pokemon", Variant: "Normal"},
		{ID: "mew47", Name: "Mew", CollectorNumber: "47", Set: "Wizards Black Star Promos", SetCode: "basep", Language: "en", Game: "pokemon", Variant: "Normal"},
		{ID: "mewtwo8", Name: "Rocket's Mewtwo", CollectorNumber: "8", Set: "Best of game", SetCode: "bog", Language: "en", Game: "pokemon", Variant: "Normal"},
	}
	matches := matchCardsWithClaude(info, cards)
	if len(matches) == 0 || matches[0].Card.ID != "mew8" {
		t.Fatalf("expected Mew #8 first, got %+v", matches)
	}
	if matches[0].NeedsReview {
		t.Errorf("exact name, number and set must auto-match; another set's Rocket's Mewtwo is not a variant")
	}
}

// A zero-padded number in a three-digit set ("008") is a different card, not a
// variant of the printed "8". The full Pokémon catalog contains Whimsicott #008,
// which used to pull every exact #8 match into review.
func TestClaudeDetection_ZeroPaddedOtherNumberIsNotAVariant(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game: "pokemon", Name: "Mew", CollectorNumber: "8",
		Set: "Wizards Black Star Promos", Language: "eng",
		Variant: "Black Star Promo", Confidence: 0.9,
	}
	cards := []models.Card{
		{ID: "mew8", Name: "Mew", CollectorNumber: "8", Set: "Wizards Black Star Promos", SetCode: "basep", Language: "en", Game: "pokemon", Variant: "Normal"},
		{ID: "whimsicott", Name: "Whimsicott", CollectorNumber: "008", Set: "Prismatic Evolutions", SetCode: "sv8pt5", Language: "en", Game: "pokemon", Variant: "Normal"},
		{ID: "mew9", Name: "Mew", CollectorNumber: "9", Set: "Wizards Black Star Promos", SetCode: "basep", Language: "en", Game: "pokemon", Variant: "Normal"},
	}
	matches := matchCardsWithClaude(info, cards)
	if len(matches) == 0 || matches[0].Card.ID != "mew8" {
		t.Fatalf("expected Mew #8 first, got %+v", matches)
	}
	if matches[0].NeedsReview {
		t.Errorf("zero-padded 008 is another card, so the exact #8 match must auto-confirm")
	}
}

// A catalog number that really carries a printing marker still marks the base
// row as ambiguous: the photographed card can be the marked variant.
func TestClaudeDetection_NumberVariantSuffixStillNeedsReview(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game: "wixoss", Name: "Focalor Azure Evil", CollectorNumber: "WXDi-P01-066",
		Set: "WIXOSS Diva Booster Pack P01", SetCode: "WXDi-P01", Language: "eng", Confidence: 0.9,
	}
	cards := []models.Card{
		{ID: "base", Name: "Focalor Azure Evil", CollectorNumber: "WXDi-P01-066", Set: "WIXOSS Diva Booster Pack P01", SetCode: "WXDi-P01", Language: "en", Game: "wixoss", Variant: "Normal"},
		{ID: "parallel", Name: "Focalor Azure Evil", CollectorNumber: "WXDi-P01-066P", Set: "WIXOSS Diva Booster Pack P01", SetCode: "WXDi-P01", Language: "en", Game: "wixoss", Variant: "Parallel Foil"},
	}
	matches := matchCardsWithClaude(info, cards)
	if len(matches) == 0 {
		t.Fatal("expected candidates")
	}
	if !matches[0].NeedsReview {
		t.Errorf("a marked number variant must stay a review choice")
	}
}

// The same-name-extension guard still has to protect real variant printings that
// share the set and the number.
func TestClaudeDetection_NameSuffixInSameSetStillNeedsReview(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game: "riftbound", Name: "Body Rune", CollectorNumber: "R04",
		Set: "Origins", SetCode: "OGN", Language: "eng", Confidence: 0.9,
	}
	cards := []models.Card{
		{ID: "base", Name: "Body Rune", CollectorNumber: "R04", Set: "Origins", SetCode: "OGN", Language: "en", Game: "riftbound", Variant: "Normal"},
		{ID: "alt", Name: "Body Rune R04c", CollectorNumber: "R04", Set: "Origins", SetCode: "OGN", Language: "en", Game: "riftbound", Variant: "Alternate Art"},
	}
	matches := matchCardsWithClaude(info, cards)
	if len(matches) == 0 {
		t.Fatal("expected candidates")
	}
	if !matches[0].NeedsReview {
		t.Errorf("a same-set name-suffix variant must stay a review choice")
	}
}
