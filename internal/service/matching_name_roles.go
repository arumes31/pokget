package service

import "strings"

// nameRoleEvidence removes a candidate name immediately following an English
// evolution marker. Inputs use normalizeMatchText's word boundaries. The bool
// distinguishes reference-only mentions from independent title/body evidence.
func nameRoleEvidence(normalizedText, normalizedName string) (string, bool) {
	if normalizedName == "" || !strings.Contains(normalizedText, "evolves") {
		return normalizedText, false
	}
	compactName := strings.ReplaceAll(normalizedName, " ", "")
	tokens := strings.Fields(normalizedText)
	removed := false
	for index, token := range tokens {
		prefix := ""
		switch {
		case strings.HasPrefix(token, "evolvesfrom"):
			prefix = "evolvesfrom"
		case index > 0 && tokens[index-1] == "evolves" && strings.HasPrefix(token, "from"):
			prefix = "from"
		default:
			continue
		}
		end, ok := nameTokensEnd(tokens, index, len(prefix), compactName)
		if !ok {
			continue
		}
		tokens[index] = prefix
		for next := index + 1; next <= end; next++ {
			tokens[next] = ""
		}
		removed = true
	}
	if !removed {
		return normalizedText, false
	}
	remaining := tokens[:0]
	for _, token := range tokens {
		if token != "" {
			remaining = append(remaining, token)
		}
	}
	independent := false
	for index := range remaining {
		if _, ok := nameTokensEnd(remaining, index, 0, compactName); ok {
			independent = true
			break
		}
	}
	return strings.Join(remaining, " "), !independent
}

// nameTokensEnd matches a compact name across complete tokens, permitting only
// the explicitly recognized evolution prefix before the first name character.
func nameTokensEnd(tokens []string, start, prefixLength int, compactName string) (int, bool) {
	offset := 0
	for index := start; index < len(tokens); index++ {
		token := tokens[index]
		if index == start {
			token = token[prefixLength:]
		}
		if len(token) > len(compactName)-offset || compactName[offset:offset+len(token)] != token {
			return 0, false
		}
		offset += len(token)
		if offset == len(compactName) {
			return index, true
		}
	}
	return 0, false
}
