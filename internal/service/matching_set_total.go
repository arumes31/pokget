package service

import (
	"strings"

	"pokget/internal/models"
)

// printedSetTotal returns the denominator of a printed collector fraction
// ("005/189" -> 189). Small fractions are rules text, not set sizes, so totals
// below 10 are ignored like in printedCollectorFractions. Prefixed subset
// totals ("TG05/TG30", "SV1/SV94") are not reported: sources do not publish
// those subset sizes, so they cannot be checked.
func printedSetTotal(collector string) (int, bool) {
	index := strings.Index(collector, "/")
	if index < 0 {
		return 0, false
	}
	prefix, total, ok := collectorNumberParts(normalizeCollectorNum(collector[index+1:]))
	if !ok || prefix != "" || total < 10 {
		return 0, false
	}
	return total, true
}

type setTotalVerdict int

const (
	setTotalUnknown setTotalVerdict = iota
	setTotalConsistent
	setTotalInconsistent
)

// setTotalEvidence classifies one product against a printed set total using
// the official size its source publishes (Card.SetTotal). A set prints exactly
// one total: Stellar Crown prints /142 and Brilliant Stars /172 even though
// both contain a #142. Without a published size the product is unknown; the
// catalog rows alone cannot prove a set too small, because rows may be missing.
func setTotalEvidence(cards []models.Card, product models.Card, total int) setTotalVerdict {
	for index := range cards {
		card := cards[index]
		if card.SetTotal > 0 && card.IsCatalogActive() && sameProduct(card, product) {
			if card.SetTotal == total {
				return setTotalConsistent
			}
			return setTotalInconsistent
		}
	}
	return setTotalUnknown
}

// setTotalWinner picks the single product that can print the read fraction
// among products tied on name and number. It returns false unless exactly one
// product is consistent and every other product is provably inconsistent, so
// an incomplete catalog never turns a guess into an automatic match.
func setTotalWinner(cards []models.Card, products []models.Card, total int) (models.Card, bool) {
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
		switch setTotalEvidence(cards, product, total) {
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
