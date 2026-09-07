package service

import (
	"cmp"
	"context"
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
	matches, _ := rankLocalMatchesContext(context.Background(), evidence, cards, lang)
	return matches
}

func rankLocalMatchesContext(ctx context.Context, evidence []ocrEvidence, cards []models.Card, lang string) ([]rankedOCRMatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(evidence) == 0 || len(cards) == 0 {
		return nil, nil
	}

	type preparedEvidence struct {
		ocrEvidence
		normalizedText string
		compactText    string
		matchTokens    []string
	}
	prepared := make([]preparedEvidence, len(evidence))
	for index, item := range evidence {
		if item.Pass == "" {
			item.Pass = "pass-" + string(rune(index))
		}
		normalizedText := normalizeOCRText(item.Text, lang)
		prepared[index] = preparedEvidence{
			ocrEvidence: item, normalizedText: normalizedText,
			compactText: strings.ReplaceAll(normalizedText, " ", ""),
			matchTokens: strings.Fields(normalizeMatchText(item.Text)),
		}
	}

	uniqueCards := make(map[string]models.Card, len(cards))
	for index, card := range cards {
		if index%64 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		key := normalizeGame(card.Game) + "\x00"
		if card.ID != "" {
			// Source IDs are opaque identities, not OCR text: 0 and O may
			// identify different printings and must never be deduplicated.
			key += "id:" + card.ID
		} else {
			key += "name:" + normalizeOCRText(card.Name, lang) + "\x00" + strings.ToLower(card.Language)
		}
		if previous, ok := uniqueCards[key]; !ok || compareCards(card, previous, lang) < 0 {
			uniqueCards[key] = card
		}
	}

	// Alternate printings share their name evidence. Compute each name once,
	// while retaining separate identifier evidence and scores for every card.
	type nameEvidence struct {
		score  float64
		passes int
	}
	nameCache := make(map[string]nameEvidence)
	matches := make([]rankedOCRMatch, 0, min(64, len(uniqueCards)))
	cjk := isCJKLanguage(lang)
	for _, card := range uniqueCards {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := normalizeOCRText(card.Name, lang)
		identifier := normalizeOCRIdentifier(card.ID)
		if name == "" && identifier == "" {
			continue
		}

		cachedName, exists := nameCache[name]
		if !exists {
			namePasses := make(map[string]struct{})
			compactName := strings.ReplaceAll(name, " ", "")
			for _, item := range prepared {
				text := item.normalizedText
				if strings.Contains(text, "evolves") && strings.Contains(item.compactText, compactName) {
					var referenceOnly bool
					text, referenceOnly = nameRoleEvidence(text, name)
					if referenceOnly {
						continue
					}
				}
				if name == "" || !fuzzySubstringMatch(text, name) {
					continue
				}
				namePasses[item.Pass] = struct{}{}
				score := 70.0 + min(item.Quality, 20)
				if strings.Contains(text, name) {
					score += 15
				}
				if item.Role == "name" {
					score += 10
				}
				cachedName.score = max(cachedName.score, score)
			}
			cachedName.passes = len(namePasses)
			nameCache[name] = cachedName
		}

		match := rankedOCRMatch{Card: card, Score: cachedName.score, NamePasses: cachedName.passes}
		if len(identifier) >= 4 {
			matchIdentifier := normalizeMatchText(card.ID)
			for _, item := range prepared {
				if !containsPrintedIdentifierTokens(item.matchTokens, matchIdentifier) {
					continue
				}
				score := 110.0 + min(item.Quality, 20)
				if item.Role == "identifier" {
					score += 20
				}
				match.Score = max(match.Score, score)
				match.IdentifierHit = true
			}
		}
		if cjk && !match.IdentifierHit && match.NamePasses < 2 {
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
	return matches, nil
}

func compareCards(left, right models.Card, lang string) int {
	if order := cmp.Compare(normalizeOCRIdentifier(left.ID), normalizeOCRIdentifier(right.ID)); order != 0 {
		return order
	}
	return cmp.Compare(normalizeOCRText(left.Name, lang), normalizeOCRText(right.Name, lang))
}

func localMatchResult(evidence []ocrEvidence, cards []models.Card, lang string) string {
	matches := rankLocalMatches(evidence, cards, lang)
	return localMatchFromRanked(matches)
}

func localMatchFromRanked(matches []rankedOCRMatch) string {
	if len(matches) == 0 {
		return "Unknown Card"
	}
	if len(matches) > 1 && matches[0].Score-matches[1].Score <= 5 {
		if normalizeMatchText(matches[0].Card.Name) == normalizeMatchText(matches[1].Card.Name) {
			return matches[0].Card.Name
		}
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
	previous := make([]int, targetLen+1)
	current := make([]int, targetLen+1)
	for _, windowLength := range [...]int{targetLen, targetLen - 1, targetLen + 1} {
		if targetLen <= 4 && windowLength != targetLen {
			continue
		}
		for start := 0; start+windowLength <= len(textRunes); start++ {
			if withinEditDistance(textRunes[start:start+windowLength], targetRunes, maxDistance, previous, current) {
				return true
			}
		}
	}
	return false
}

// withinEditDistance visits only the diagonal band that can satisfy the limit.
// The caller reuses both rows across windows, avoiding per-window allocations.
func withinEditDistance(text, target []rune, limit int, previous, current []int) bool {
	for index := range previous {
		previous[index] = min(index, limit+1)
	}
	for row, r := range text {
		i := row + 1
		current[0] = min(i, limit+1)
		from, to := max(1, i-limit), min(len(target), i+limit)
		if from > 1 {
			current[from-1] = limit + 1
		}
		best := limit + 1
		for column := from; column <= to; column++ {
			cost := 1
			if r == target[column-1] {
				cost = 0
			}
			current[column] = min(current[column-1]+1, previous[column]+1, previous[column-1]+cost)
			best = min(best, current[column])
		}
		if best > limit {
			return false
		}
		if to < len(target) {
			current[to+1] = limit + 1
		}
		previous, current = current, previous
	}
	return previous[len(target)] <= limit
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
