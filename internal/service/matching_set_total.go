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

type setTotalEntry struct {
	index int
	total int
}

// setTotalIndex holds the official set sizes (Card.SetTotal) of the active
// catalog rows, built in one pass so every lookup is a map hit instead of a
// scan. Each map keeps the first row like a linear scan would, and the keys
// mirror sameProduct: set codes compare with codes, and a set name matches
// rows without a code (or any row when the product has no code).
type setTotalIndex struct {
	byCode       map[string]setTotalEntry
	byName       map[string]setTotalEntry
	byNameNoCode map[string]setTotalEntry
}

func newSetTotalIndex() *setTotalIndex {
	return &setTotalIndex{
		byCode:       map[string]setTotalEntry{},
		byName:       map[string]setTotalEntry{},
		byNameNoCode: map[string]setTotalEntry{},
	}
}

// add records an active catalog row; code and name are already normalized
// with normalizeMatchText. Rows must be added in catalog order.
func (s *setTotalIndex) add(index int, code, name string, total int) {
	if total <= 0 {
		return
	}
	entry := setTotalEntry{index: index, total: total}
	putFirst := func(entries map[string]setTotalEntry, key string) {
		if _, ok := entries[key]; !ok {
			entries[key] = entry
		}
	}
	if code != "" {
		putFirst(s.byCode, code)
	}
	if name != "" {
		putFirst(s.byName, name)
		if code == "" {
			putFirst(s.byNameNoCode, name)
		}
	}
}

// evidence classifies one product against a printed set total using the
// official size its source publishes. A set prints exactly one total: Stellar
// Crown prints /142 and Brilliant Stars /172 even though both contain a #142.
// Without a published size the product is unknown; the catalog rows alone
// cannot prove a set too small, because rows may be missing.
func (s *setTotalIndex) evidence(product models.Card, total int) setTotalVerdict {
	code := normalizeMatchText(product.SetCode)
	name := normalizeMatchText(product.Set)
	var entry setTotalEntry
	found := false
	if code != "" {
		entry, found = s.byCode[code]
		if name != "" {
			if byName, ok := s.byNameNoCode[name]; ok && (!found || byName.index < entry.index) {
				entry, found = byName, true
			}
		}
	} else if name != "" {
		entry, found = s.byName[name]
	}
	switch {
	case !found:
		return setTotalUnknown
	case entry.total == total:
		return setTotalConsistent
	default:
		return setTotalInconsistent
	}
}

// setTotalWinner picks the single product that can print the read fraction
// among products tied on name and number. It returns false unless exactly one
// product is consistent and every other product is provably inconsistent, so
// an incomplete catalog never turns a guess into an automatic match.
func setTotalWinner(totals *setTotalIndex, products []models.Card, total int) (models.Card, bool) {
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
		switch totals.evidence(product, total) {
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
