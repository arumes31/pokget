package service

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

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

// foldVoicing drops the Japanese dakuten (゛) and handakuten (゜) marks, so an
// OCR or vision error that misses a single mark (ピジョット vs ビジョット) still
// finds the card family. Call it on normalized text; the result is only ever
// review evidence, because two real cards can differ by exactly that mark.
func foldVoicing(normalized string) string {
	if normalized == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(normalized))
	changed := false
	for _, r := range norm.NFD.String(normalized) {
		if unicode.Is(unicode.Mn, r) {
			changed = true
			continue
		}
		builder.WriteRune(r)
	}
	if !changed {
		return normalized
	}
	return norm.NFC.String(builder.String())
}

func asciiDigits(value string) bool {
	for index := range len(value) {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return value != ""
}
