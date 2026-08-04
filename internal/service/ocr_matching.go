package service

import (
	"strings"
	"unicode"
)

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
