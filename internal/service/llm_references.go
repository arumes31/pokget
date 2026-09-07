package service

import (
	"net"
	"net/url"
	"strings"

	"pokget/internal/catalog/source"
	"pokget/internal/models"
)

const maxLLMArtworkReferences = 8

type llmArtworkReference struct {
	CardID string `json:"reference_card_id"`
	URL    string `json:"-"`
}

var llmArtworkHosts = func() map[string]struct{} {
	hosts := make(map[string]struct{})
	for _, sourceHosts := range source.DefaultImageHosts() {
		for _, host := range sourceHosts {
			hosts[host] = struct{}{}
		}
	}
	return hosts
}()

func allowedLLMArtworkURL(raw string) string {
	if len(raw) > 4096 || strings.Contains(raw, "#") {
		return ""
	}
	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "https" || target.User != nil || target.Fragment != "" || target.Opaque != "" {
		return ""
	}
	host := strings.ToLower(target.Hostname())
	// Exact catalog hostnames exclude private addresses, lookalike domains,
	// and arbitrary image servers. Explicit ports are not accepted.
	if host == "" || target.Host != target.Hostname() || net.ParseIP(host) != nil {
		return ""
	}
	if _, allowed := llmArtworkHosts[host]; !allowed {
		return ""
	}
	return target.String()
}

type llmArtworkGroupKey struct {
	Name, Set, SetCode, CollectorNumber, Language, Game, Variant string
}

func artworkGroupKey(card models.Card) llmArtworkGroupKey {
	normalize := func(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
	return llmArtworkGroupKey{
		Name: normalize(card.Name), Set: normalize(card.Set), SetCode: normalize(card.SetCode),
		CollectorNumber: normalize(card.CollectorNumber), Language: normalize(card.Language),
		Game: normalize(card.Game), Variant: normalize(card.Variant),
	}
}

// shortlistArtworkReferences attaches complete groups whose printed metadata
// cannot distinguish their artwork. The score-ranked shortlist supplies group
// priority; a group never partially consumes the reference-image budget.
func shortlistArtworkReferences(shortlist []candidateEvidence, eligible []models.Card) []llmArtworkReference {
	groups := make(map[llmArtworkGroupKey][]models.Card)
	order := make([]llmArtworkGroupKey, 0, len(shortlist))
	for _, candidate := range shortlist {
		key := artworkGroupKey(candidate.Card)
		if _, exists := groups[key]; !exists {
			order = append(order, key)
		}
		groups[key] = append(groups[key], candidate.Card)
	}
	eligibleIDs := make(map[llmArtworkGroupKey]map[string]struct{}, len(groups))
	for _, card := range eligible {
		if !card.IsCatalogActive() {
			continue
		}
		key := artworkGroupKey(card)
		if _, selected := groups[key]; !selected || card.ID == "" {
			continue
		}
		if eligibleIDs[key] == nil {
			eligibleIDs[key] = make(map[string]struct{})
		}
		eligibleIDs[key][card.ID] = struct{}{}
	}
	var references []llmArtworkReference
	for _, key := range order {
		cards := groups[key]
		if len(cards) < 2 || len(cards) != len(eligibleIDs[key]) || len(cards) > maxLLMArtworkReferences-len(references) {
			continue
		}
		group := make([]llmArtworkReference, 0, len(cards))
		for _, card := range cards {
			artURL := allowedLLMArtworkURL(card.ImageURL)
			if artURL == "" || card.ID == "" {
				group = nil
				break
			}
			group = append(group, llmArtworkReference{CardID: card.ID, URL: artURL})
		}
		references = append(references, group...)
	}
	return references
}
