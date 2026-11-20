package service

import (
	"strings"

	"pokget/internal/models"
)

// printedSetTotal returns the denominator of a printed collector fraction
// ("005/189" -> 189, "TG05/TG30" -> tg, 30). Small fractions are rules text,
// not set sizes, so totals below 10 are ignored like in
// printedCollectorFractions.
func printedSetTotal(collector string) (string, int, bool) {
	index := strings.Index(collector, "/")
	if index < 0 {
		return "", 0, false
	}
	prefix, total, ok := collectorNumberParts(normalizeCollectorNum(collector[index+1:]))
	if !ok || total < 10 {
		return "", 0, false
	}
	return prefix, total, true
}

// setTotalEvidence classifies one product against a printed set total. A set
// whose catalog rows contain the total itself as a collector number ("189" in
// a 189-card set) is consistent. A set is inconsistent only when its highest
// plain number stays below the total: such a set cannot print that fraction,
// while a set with a missing catalog row is merely unknown.
type setTotalVerdict int

const (
	setTotalUnknown setTotalVerdict = iota
	setTotalConsistent
	setTotalInconsistent
)

func setTotalEvidence(cards []models.Card, product models.Card, prefix string, total int) setTotalVerdict {
	// The published official size is exact: a set prints exactly one total
	// (Stellar Crown 142, Brilliant Stars 172) even though both contain a #142,
	// and promo sets, which print no fraction at all, never equal it by chance
	// in practice. Prefixed totals (TG30, SV94) are subset sizes the source does
	// not publish, so they use the catalog numbers below.
	if prefix == "" {
		for index := range cards {
			card := cards[index]
			if card.SetTotal > 0 && card.IsCatalogActive() && sameProduct(card, product) {
				if card.SetTotal == total {
					return setTotalConsistent
				}
				return setTotalInconsistent
			}
		}
	}
	highest := -1
	for index := range cards {
		card := cards[index]
		if !card.IsCatalogActive() || !sameProduct(card, product) {
			continue
		}
		cardPrefix, number, ok := collectorNumberParts(normalizeCollectorNum(extractNumerator(card.CollectorNumber)))
		if !ok || cardPrefix != prefix {
			continue
		}
		if number == total {
			return setTotalConsistent
		}
		if number > highest {
			highest = number
		}
	}
	if highest >= 0 && highest < total {
		return setTotalInconsistent
	}
	return setTotalUnknown
}

// setTotalWinner picks the single product that can print the read fraction
// among products tied on name and number. It returns false unless exactly one
// product is consistent and every other product is provably inconsistent, so
// an incomplete catalog never turns a guess into an automatic match.
func setTotalWinner(cards []models.Card, products []models.Card, prefix string, total int) (models.Card, bool) {
	var winner models.Card
	found := false
	seen := make(map[string]bool, len(products))
	for _, product := range products {
		identity := setIdentity(product)
		if identity == "" {
			return models.Card{}, false
		}
		if seen[identity] {
			continue
		}
		seen[identity] = true
		switch setTotalEvidence(cards, product, prefix, total) {
		case setTotalConsistent:
			if found {
				return models.Card{}, false
			}
			winner, found = product, true
		case setTotalInconsistent:
		default:
			return models.Card{}, false
		}
	}
	return winner, found && len(seen) > 1
}
