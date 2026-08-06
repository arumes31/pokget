package service

import (
	"sort"
	"strings"
	"unicode"

	"pokget/internal/models"

	"golang.org/x/text/unicode/norm"
)

type candidateEvidence struct {
	Card    models.Card
	Score   int
	Reasons []string
}

// normalizeMatchText applies compatibility Unicode normalization, folds case
// and diacritics, and gives punctuation consistent word-boundary semantics.
func normalizeMatchText(value string) string {
	value = norm.NFKD.String(strings.ToLower(strings.TrimSpace(value)))
	var builder strings.Builder
	builder.Grow(len(value))
	spacePending := false
	var previousLetter rune
	for _, r := range value {
		if unicode.Is(unicode.Mn, r) {
			if unicode.In(previousLetter, unicode.Latin) {
				continue
			}
			if builder.Len() > 0 {
				builder.WriteRune(r)
			}
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			if spacePending && builder.Len() > 0 {
				builder.WriteByte(' ')
			}
			builder.WriteRune(r)
			spacePending = false
			if unicode.IsLetter(r) {
				previousLetter = r
			} else {
				previousLetter = 0
			}
			continue
		}
		spacePending = builder.Len() > 0
		previousLetter = 0
	}
	return norm.NFC.String(builder.String())
}

func compactMatchText(value string) string {
	return strings.ReplaceAll(normalizeMatchText(value), " ", "")
}

func containsNormalizedValue(text, value string) bool {
	if text == "" || value == "" {
		return false
	}
	return strings.Contains(" "+text+" ", " "+value+" ")
}

func rankCandidates(ocrText string, cards []models.Card, maxCandidates int) []candidateEvidence {
	if len(cards) == 0 || maxCandidates <= 0 {
		return nil
	}

	normalizedOCR := normalizeMatchText(ocrText)
	compactOCR := strings.ReplaceAll(normalizedOCR, " ", "")
	ocrTokens := strings.Fields(normalizedOCR)
	bestByID := make(map[string]candidateEvidence, len(cards))

	for index := range cards {
		card := cards[index]
		evidence := scoreCandidate(normalizedOCR, compactOCR, ocrTokens, card)
		key := card.ID
		if key == "" {
			// Blank IDs cannot be stable printing identifiers. Retain them only
			// for the legacy shortlist wrapper, with a deterministic local key.
			key = "\x00" + normalizeMatchText(card.Name) + "\x00" + normalizeMatchText(card.Set) + "\x00" + normalizeMatchText(card.CollectorNumber)
		}
		if existing, ok := bestByID[key]; !ok || betterCandidateEvidence(evidence, existing) {
			bestByID[key] = evidence
		}
	}

	ranked := make([]candidateEvidence, 0, len(bestByID))
	for _, evidence := range bestByID {
		ranked = append(ranked, evidence)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return betterCandidateEvidence(ranked[i], ranked[j])
	})
	if len(ranked) > maxCandidates {
		ranked = ranked[:maxCandidates]
	}
	return ranked
}

func scoreCandidate(normalizedOCR, compactOCR string, ocrTokens []string, card models.Card) candidateEvidence {
	evidence := candidateEvidence{Card: card}
	addEvidence := func(score int, reason string) {
		evidence.Score += score
		evidence.Reasons = append(evidence.Reasons, reason)
	}

	cardID := normalizeMatchText(card.ID)
	collectorNumber := normalizeMatchText(card.CollectorNumber)
	setCode := normalizeMatchText(card.SetCode)
	setName := normalizeMatchText(card.Set)

	if containsNormalizedValue(normalizedOCR, cardID) || (len(compactMatchText(card.ID)) >= 4 && strings.Contains(compactOCR, compactMatchText(card.ID))) {
		addEvidence(1000, "card_id")
	}
	compactCollector := compactMatchText(card.CollectorNumber)
	collectorMatched := collectorNumber != "" && (containsNormalizedValue(normalizedOCR, collectorNumber) ||
		(len([]rune(compactCollector)) >= 3 && strings.Contains(compactOCR, compactCollector)))
	if collectorMatched {
		addEvidence(700, "collector_number")
	}
	if setCode != "" && containsNormalizedValue(normalizedOCR, setCode) {
		addEvidence(400, "set_code")
	}
	if collectorMatched && setCode != "" && containsNormalizedValue(normalizedOCR, setCode) {
		addEvidence(500, "set_and_collector")
	}
	if setName != "" && containsNormalizedValue(normalizedOCR, setName) {
		addEvidence(180, "set_name")
	}

	names := make([]string, 0, 1+len(card.LocalizedNames))
	names = append(names, card.Name)
	names = append(names, card.LocalizedNames...)
	bestNameScore := 0
	bestNameReason := ""
	for nameIndex, name := range names {
		normalizedName := normalizeMatchText(name)
		if normalizedName == "" {
			continue
		}
		score, reason := nameEvidence(normalizedOCR, compactOCR, ocrTokens, normalizedName)
		if nameIndex > 0 && reason != "" {
			reason = "localized_" + reason
		}
		if score > bestNameScore {
			bestNameScore = score
			bestNameReason = reason
		}
	}
	if bestNameScore > 0 {
		addEvidence(bestNameScore, bestNameReason)
	}

	variant := normalizeMatchText(card.Variant)
	if variant != "" && containsNormalizedValue(normalizedOCR, variant) {
		addEvidence(80, "variant")
	}
	return evidence
}

func nameEvidence(normalizedOCR, compactOCR string, ocrTokens []string, normalizedName string) (int, string) {
	if containsNormalizedValue(normalizedOCR, normalizedName) {
		return 650 + min(len([]rune(normalizedName)), 100), "exact_name"
	}
	compactName := strings.ReplaceAll(normalizedName, " ", "")
	if len([]rune(compactName)) >= 3 && strings.Contains(compactOCR, compactName) {
		return 550 + min(len([]rune(compactName)), 100), "compact_name"
	}

	nameTokens := strings.Fields(normalizedName)
	if len(nameTokens) == 0 {
		return 0, ""
	}
	ocrSet := make(map[string]struct{}, len(ocrTokens))
	for _, token := range ocrTokens {
		ocrSet[token] = struct{}{}
	}
	matched := 0
	for _, token := range nameTokens {
		if len([]rune(token)) < 2 {
			continue
		}
		if _, ok := ocrSet[token]; ok {
			matched++
		}
	}
	if matched == 0 {
		compactNameRunes := []rune(compactName)
		compactOCRRunes := []rune(compactOCR)
		if len(compactNameRunes) >= 4 && len(compactOCRRunes) <= len(compactNameRunes)+2 {
			distance := levenshtein(compactOCR, compactName)
			if distance <= max(1, len(compactNameRunes)/5) {
				return 260 - distance*20, "fuzzy_name"
			}
		}
		return 0, ""
	}
	ratio := float64(matched) / float64(len(nameTokens))
	if ratio < 0.34 {
		return 0, ""
	}
	return int(ratio * 350), "name_tokens"
}

func betterCandidateEvidence(left, right candidateEvidence) bool {
	if left.Score != right.Score {
		return left.Score > right.Score
	}
	if left.Card.ID != right.Card.ID {
		return left.Card.ID < right.Card.ID
	}
	if left.Card.SetCode != right.Card.SetCode {
		return left.Card.SetCode < right.Card.SetCode
	}
	if left.Card.CollectorNumber != right.Card.CollectorNumber {
		return left.Card.CollectorNumber < right.Card.CollectorNumber
	}
	return left.Card.Name < right.Card.Name
}

// buildShortlist retains the legacy helper while using the same deterministic
// printing-level ranking as the strict matching path.
func buildShortlist(ocrText string, cards []models.Card, maxCandidates int) []models.Card {
	ranked := rankCandidates(ocrText, cards, maxCandidates)
	if ranked == nil {
		return nil
	}
	shortlist := make([]models.Card, 0, len(ranked))
	for _, candidate := range ranked {
		shortlist = append(shortlist, candidate.Card)
	}
	return shortlist
}
