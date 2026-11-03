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
	set := canonicalPriceToken(setCode)
	numbers := priceNumberKeys(number, set)
	if len(numbers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(numbers))
	seen := make(map[string]struct{}, len(numbers))
	for _, n := range numbers {
		key := set + "|" + n
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
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
		// "089/108" and "1/110" only keep the print run after the slash.
		segment := strings.SplitN(field, "/", 2)[0]
		add(segment)
		if set != "" {
			add(priceStripSetPrefix(segment, set))
		}
	}
	return keys
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
