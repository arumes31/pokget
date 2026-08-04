package service

import (
	"strings"
	"unicode"

	"pokget/internal/models"
)

// fingerprintScoreFromDistance converts a Hamming distance to a 0-100 confidence score (SCAN-09).
// Distance 0 = 100%, distance >= threshold = 0%.
func fingerprintScoreFromDistance(distance int, threshold int) float64 {
	if distance >= threshold {
		return 0
	}
	if distance <= 0 {
		return 100
	}
	return float64(threshold-distance) / float64(threshold) * 100
}

// ocrScoreFromLevenshtein converts a Levenshtein similarity to a 0-100 score (SCAN-09).
func ocrScoreFromLevenshtein(ocrText, cardName string) float64 {
	if ocrText == "" || cardName == "" {
		return 0
	}
	ocrLower := strings.ToLower(ocrText)
	nameLower := strings.ToLower(cardName)

	shortLatinName := len([]rune(nameLower)) <= 3
	for _, r := range nameLower {
		if unicode.IsLetter(r) && !unicode.In(r, unicode.Latin) {
			shortLatinName = false
			break
		}
	}
	if shortLatinName {
		for _, token := range strings.FieldsFunc(ocrLower, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		}) {
			if token == nameLower {
				return 95
			}
		}
	} else if strings.Contains(ocrLower, nameLower) {
		return 95
	}

	maxLen := len([]rune(ocrLower))
	nameRunes := []rune(nameLower)
	if len(nameRunes) > maxLen {
		maxLen = len(nameRunes)
	}
	if maxLen == 0 {
		return 0
	}

	dist := levenshtein(ocrLower, nameLower)
	similarity := float64(maxLen-dist) / float64(maxLen) * 100
	if similarity < 0 {
		similarity = 0
	}
	return similarity
}

// combineScores merges confidence scores from multiple detection methods (SCAN-09).
// Weights: fingerprint=0.5, OCR=0.3, LLM=0.2.
func combineScores(fp *ConfidenceScore, ocr *ConfidenceScore, llm *ConfidenceScore) float64 {
	totalWeight := 0.0
	weightedSum := 0.0
	signalCount := 0

	if fp != nil && fp.Score > 0 {
		weightedSum += fp.Score * 0.5
		totalWeight += 0.5
		signalCount++
	}
	if ocr != nil && ocr.Score > 0 {
		weightedSum += ocr.Score * 0.3
		totalWeight += 0.3
		signalCount++
	}
	if llm != nil && llm.Score > 0 {
		weightedSum += llm.Score * 0.2
		totalWeight += 0.2
		signalCount++
	}
	if totalWeight == 0 {
		return 0
	}
	if signalCount > 1 {
		return weightedSum / totalWeight
	}
	return weightedSum
}

func ambiguousSameNameFingerprints(result *MatchResult, threshold int) []FingerprintMatch {
	if result == nil {
		return nil
	}
	matches := make([]FingerprintMatch, 0, len(result.Potential))
	name := ""
	for _, match := range result.Potential {
		if match.Distance > threshold {
			break
		}
		if name == "" {
			name = strings.ToLower(strings.TrimSpace(match.Card.Name))
		}
		if strings.ToLower(strings.TrimSpace(match.Card.Name)) != name {
			return nil
		}
		matches = append(matches, match)
	}
	return matches
}

func exactFingerprintMatches(result *MatchResult) []FingerprintMatch {
	if result == nil {
		return nil
	}
	matches := make([]FingerprintMatch, 0, 1)
	seen := make(map[string]bool)
	for _, match := range result.Potential {
		if match.Distance > 0 {
			break
		}
		if match.Card == nil || seen[match.Card.ID] {
			continue
		}
		seen[match.Card.ID] = true
		matches = append(matches, match)
	}
	return matches
}

func uniqueHighConfidenceFingerprint(result *MatchResult, threshold int) (*models.Card, int) {
	if result == nil {
		return nil, 0
	}
	var candidate *models.Card
	distance := 0
	for _, match := range result.Potential {
		if match.Distance > threshold {
			break
		}
		if candidate != nil && candidate.ID != match.Card.ID {
			return nil, 0
		}
		candidate = match.Card
		distance = match.Distance
	}
	return candidate, distance
}

func getOrCreateMatch(m map[string]*CardMatch, card *models.Card) *CardMatch {
	if cm, ok := m[card.ID]; ok {
		return cm
	}
	cm := &CardMatch{Card: card}
	m[card.ID] = cm
	return cm
}
