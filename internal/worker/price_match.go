package worker

import (
	"strings"
)

// priceMatchKeys returns every lookup key a (set code, number) pair can be
// indexed under.
//
// Catalog and marketplace disagree on the same card in a handful of ways, so one
// key per card is not enough:
//
//   - leading zeros: catalog "sv02"/"001" vs TCGplayer "SV2"/"001/198"
//   - the set code repeated inside the number: "hBP01-001", "SF621 1/21"
//   - a trailing rarity: TCGplayer writes Digimon numbers as "BT27-084 SR"
//   - the print run after a slash: "089/108"
//   - set codes dropping separators: "OP-01", "TAL/", "BT-26"
//
// Both sides go through this function, so every variant is generated twice and
// the canonical forms meet in the middle.
func priceMatchKeys(setCode, number string) []string {
	numbers := priceNumberKeys(number, canonicalPriceToken(setCode))
	if len(numbers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(numbers)*2)
	seen := make(map[string]struct{}, len(numbers)*2)
	for _, set := range priceSetVariants(setCode) {
		for _, n := range numbers {
			key := set + "|" + n
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	return keys
}

// priceSetVariants returns the set spellings a marketplace may use. TCGplayer sells
// the English release of a Japanese hololive set under the Japanese code plus a
// locale suffix ("hBP01" -> "hBP01e"), so the suffix-free form matches as well.
func priceSetVariants(setCode string) []string {
	canonical := canonicalPriceToken(setCode)
	variants := []string{canonical}
	if trimmed, ok := trimLocaleSuffix(canonical); ok {
		variants = append(variants, trimmed)
	}
	return variants
}

// trimLocaleSuffix drops the trailing "e" TCGplayer appends to English releases of
// Japanese sets, but only when it follows a digit so real set codes stay intact.
func trimLocaleSuffix(canonical string) (string, bool) {
	if len(canonical) < 4 || !strings.HasSuffix(canonical, "e") {
		return "", false
	}
	previous := canonical[len(canonical)-2]
	if previous < '0' || previous > '9' {
		return "", false
	}
	return canonical[:len(canonical)-1], true
}

// priceNumberOnlyKey identifies a card by its collector number alone, for numbers
// that carry the set code themselves ("NIK/S135-E063"). Marketplaces shorten the
// set abbreviation of such a game ("NIK/" for "NIK/S135"), so the set key never
// matches even though the number is unique.
//
// The key is only produced for composite numbers (at least six characters, letters
// and digits): a plain "089" would match every set that counts the same way.
func priceNumberOnlyKey(number string) string {
	token := trimVariantLetters(number)
	if len(token) < 6 {
		return ""
	}
	var letters, digits int
	for _, r := range token {
		switch {
		case r >= 'a' && r <= 'z':
			letters++
		case r >= '0' && r <= '9':
			digits++
		}
	}
	if letters == 0 || digits < 3 {
		return ""
	}
	return token
}

// priceNameKey identifies a card by set and name. It is the fallback for
// marketplaces that list old cards without a collector number.
//
// A name that canonicalizes to fewer than three characters is rejected: names in
// Japanese, Chinese or Korean script keep no ASCII letter at all, and an empty or
// one-letter token would match every other such card in the set.
func priceNameKey(setCode, name string) string {
	set := canonicalPriceToken(setCode)
	card := canonicalPriceToken(name)
	if set == "" || len(card) < 3 {
		return ""
	}
	return set + "|" + card
}

// nonNil returns an empty slice for a nil list. pq.Array(nil) would encode SQL
// NULL and every comparison against it evaluates to NULL.
func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// priceNumberKeys normalizes one collector number into the spellings a
// marketplace may use for it.
func priceNumberKeys(number, set string) []string {
	var keys []string
	seen := make(map[string]struct{}, 4)
	add := func(raw string) {
		token := canonicalPriceToken(raw)
		if token == "" {
			return
		}
		if _, ok := seen[token]; ok {
			return
		}
		seen[token] = struct{}{}
		keys = append(keys, token)
	}

	fields := strings.Fields(number)
	hasDigit := false
	for _, field := range fields {
		if strings.IndexFunc(field, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0 {
			hasDigit = true
			break
		}
	}
	for _, field := range fields {
		// "BT27-084 SR" carries the rarity as a second word. A word without a
		// digit next to a real number is never the collector number.
		if hasDigit && !strings.ContainsAny(field, "0123456789") {
			continue
		}
		// "089/108" and "1/110" only keep the print run after the slash, but a
		// Weiss Schwarz number carries the set code with a slash inside it
		// ("NIK/S135-E063"), so the whole field is a candidate as well.
		add(field)
		segment := strings.SplitN(field, "/", 2)[0]
		add(segment)
		// Weiss Schwarz marks foils and signed copies by appending letters to the
		// number ("E063S", "E063SP"); the base printing is the same card.
		add(trimVariantLetters(field))
		if set != "" {
			add(priceStripSetPrefix(segment, set))
			add(priceStripSetPrefix(trimVariantLetters(field), set))
		}
	}
	return keys
}

// trimVariantLetters removes a short trailing letter block from a number when it
// directly follows a digit ("niks135e063sp" -> "niks135e063"). Letters that follow a
// separator are part of the number ("…-b-f") and stay, and a long block is a word
// ("006-aaj_kegon…"), not a variant marker.
func trimVariantLetters(field string) string {
	token := canonicalPriceToken(field)
	end := len(token)
	for end > 0 && token[end-1] >= 'a' && token[end-1] <= 'z' {
		end--
	}
	if end == 0 || len(token)-end > 3 {
		return token
	}
	if previous := token[end-1]; previous < '0' || previous > '9' {
		return token
	}
	return token[:end]
}

// priceStripSetPrefix removes the leading set code from a number
// ("hbp01001" + "hbp01" -> "001").
func priceStripSetPrefix(number, set string) string {
	token := canonicalPriceToken(number)
	if set == "" || token == "" || len(token) <= len(set) {
		return ""
	}
	if !strings.HasPrefix(token, set) {
		return ""
	}
	rest := token[len(set):]
	if !strings.ContainsAny(rest, "0123456789") {
		return ""
	}
	return rest
}

// canonicalPriceToken lower-cases a token, drops every separator and removes the
// leading zeros of each digit run so zero padding cannot separate two spellings
// of the same card ("AOT01" + "001" == "AOT1" + "1").
func canonicalPriceToken(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	run := make([]byte, 0, 8)
	flush := func() {
		if len(run) == 0 {
			return
		}
		trimmed := strings.TrimLeft(string(run), "0")
		if trimmed == "" {
			trimmed = "0"
		}
		builder.WriteString(trimmed)
		run = run[:0]
	}
	for _, r := range strings.ToLower(value) {
		if r >= '0' && r <= '9' {
			run = append(run, byte(r))
			continue
		}
		flush()
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		}
	}
	flush()
	return builder.String()
}
