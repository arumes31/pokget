package service

import (
	"cmp"
	"pokget/internal/models"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type ocrEvidence struct {
	Text    string
	Pass    string
	Role    string
	Quality float64
}

type rankedOCRMatch struct {
	Card          models.Card
	Score         float64
	NamePasses    int
	IdentifierHit bool
}

func normalizeOCRText(text, lang string) string {
	text = strings.ToLower(norm.NFKC.String(text))
	var normalized strings.Builder
	previousSpace := false
	cjk := isCJKLanguage(lang)
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			normalized.WriteRune(r)
			previousSpace = false
		case cjk:
			// OCR commonly inserts arbitrary whitespace between CJK glyphs.
			continue
		case !previousSpace:
			normalized.WriteByte(' ')
			previousSpace = true
		}
	}
	return strings.TrimSpace(normalized.String())
}

func normalizeOCRIdentifier(value string) string {
	value = strings.ToLower(norm.NFKC.String(value))
	var normalized strings.Builder
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			continue
		}
		if r == '0' {
			r = 'o'
		}
		normalized.WriteRune(r)
	}
	return normalized.String()
}

func isCJKLanguage(lang string) bool {
	lang = strings.ToLower(lang)
	return strings.Contains(lang, "jpn") || strings.Contains(lang, "ja") ||
		strings.Contains(lang, "chi") || strings.Contains(lang, "zh") ||
		strings.Contains(lang, "kor") || strings.Contains(lang, "ko")
}

func inferOCRGame(cards []models.Card) string {
	game := ""
	for _, card := range cards {
		candidate := normalizeGame(card.Game)
		if candidate == "" {
			continue
		}
		if game != "" && candidate != game {
			return ""
		}
		game = candidate
	}
	return game
}

func rankLocalMatches(evidence []ocrEvidence, cards []models.Card, lang string) []rankedOCRMatch {
	if len(evidence) == 0 || len(cards) == 0 {
		return nil
	}

	uniqueCards := make(map[string]models.Card, len(cards))
	for _, card := range cards {
		key := normalizeGame(card.Game) + "\x00"
		if card.ID != "" {
			key += "id:" + normalizeOCRIdentifier(card.ID)
		} else {
			key += "name:" + normalizeOCRText(card.Name, lang) + "\x00" + strings.ToLower(card.Language)
		}
		if previous, ok := uniqueCards[key]; !ok || compareCards(card, previous, lang) < 0 {
			uniqueCards[key] = card
		}
	}

	matches := make([]rankedOCRMatch, 0, len(uniqueCards))
	for _, card := range uniqueCards {
		name := normalizeOCRText(card.Name, lang)
		identifier := normalizeOCRIdentifier(card.ID)
		if name == "" && identifier == "" {
			continue
		}

		match := rankedOCRMatch{Card: card}
		namePasses := make(map[string]struct{})
		for index, item := range evidence {
			text := normalizeOCRText(item.Text, lang)
			compact := normalizeOCRIdentifier(item.Text)
			pass := item.Pass
			if pass == "" {
				pass = "pass-" + string(rune(index))
			}

			if len(identifier) >= 4 && strings.Contains(compact, identifier) {
				score := 110.0 + min(item.Quality, 20)
				if item.Role == "identifier" {
					score += 20
				}
				if score > match.Score {
					match.Score = score
				}
				match.IdentifierHit = true
			}

			if name == "" || !fuzzySubstringMatch(text, name) {
				continue
			}
			namePasses[pass] = struct{}{}
			score := 70.0 + min(item.Quality, 20)
			if strings.Contains(text, name) {
				score += 15
			}
			if item.Role == "name" {
				score += 10
			}
			if score > match.Score {
				match.Score = score
			}
		}

		match.NamePasses = len(namePasses)
		if isCJKLanguage(lang) && !match.IdentifierHit && match.NamePasses < 2 {
			continue
		}
		if match.Score > 0 {
			if match.NamePasses > 1 {
				match.Score += float64(min(match.NamePasses-1, 3)) * 4
			}
			matches = append(matches, match)
		}
	}

	slices.SortFunc(matches, func(left, right rankedOCRMatch) int {
		if order := cmp.Compare(right.Score, left.Score); order != 0 {
			return order
		}
		if order := cmp.Compare(normalizeOCRIdentifier(left.Card.ID), normalizeOCRIdentifier(right.Card.ID)); order != 0 {
			return order
		}
		return cmp.Compare(normalizeOCRText(left.Card.Name, lang), normalizeOCRText(right.Card.Name, lang))
	})
	return matches
}

func compareCards(left, right models.Card, lang string) int {
	if order := cmp.Compare(normalizeOCRIdentifier(left.ID), normalizeOCRIdentifier(right.ID)); order != 0 {
		return order
	}
	return cmp.Compare(normalizeOCRText(left.Name, lang), normalizeOCRText(right.Name, lang))
}

func localMatchResult(evidence []ocrEvidence, cards []models.Card, lang string) string {
	matches := rankLocalMatches(evidence, cards, lang)
	if len(matches) == 0 {
		return "Unknown Card"
	}
	if matches[0].Card.ID != "" {
		return matches[0].Card.ID
	}
	return matches[0].Card.Name
}

// fuzzySubstringMatch checks if a card name occurs in larger OCR text while
// tolerating a small edit distance for names long enough to be distinctive.
func fuzzySubstringMatch(text, target string) bool {
	targetRunes := []rune(strings.ToLower(strings.TrimSpace(target)))
	if len(targetRunes) == 0 {
		return false
	}

	textLower := strings.ToLower(text)
	targetStr := string(targetRunes)
	if isShortLatinName(targetRunes) {
		for _, token := range strings.FieldsFunc(textLower, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		}) {
			if token == targetStr {
				return true
			}
		}
		return false
	}
	if strings.Contains(textLower, targetStr) {
		return true
	}

	textRunes := []rune(textLower)
	targetLen := len(targetRunes)
	if len(textRunes) < targetLen {
		return false
	}

	maxDistance := 1
	if targetLen > 7 {
		maxDistance = min(targetLen/4, 3)
	}
	for i := 0; i <= len(textRunes)-targetLen; i++ {
		if levenshtein(string(textRunes[i:i+targetLen]), targetStr) <= maxDistance {
			return true
		}
	}
	if targetLen > 4 {
		for i := 0; i <= len(textRunes)-(targetLen-1); i++ {
			if levenshtein(string(textRunes[i:i+targetLen-1]), targetStr) <= maxDistance {
				return true
			}
		}
		if len(textRunes) >= targetLen+1 {
			for i := 0; i <= len(textRunes)-(targetLen+1); i++ {
				if levenshtein(string(textRunes[i:i+targetLen+1]), targetStr) <= maxDistance {
					return true
				}
			}
		}
	}
	return false
}

func isShortLatinName(name []rune) bool {
	if len(name) > 3 {
		return false
	}
	hasLatinLetter := false
	for _, r := range name {
		if unicode.IsLetter(r) {
			if !unicode.In(r, unicode.Latin) {
				return false
			}
			hasLatinLetter = true
		}
	}
	return hasLatinLetter
}
