package catalog

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
)

// CanonicalID returns a deterministic, globally namespaced identifier. Length
// prefixes make different part boundaries unambiguous, and the complete SHA-256
// digest avoids leaking punctuation or path separators from upstream IDs.
func CanonicalID(kind string, parts ...string) (string, error) {
	if !validIDKind(kind) {
		return "", fmt.Errorf("catalog: invalid id kind %q", kind)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("catalog: canonical id requires at least one part")
	}

	hash := sha256.New()
	var length [8]byte
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return "", fmt.Errorf("catalog: canonical id parts must not be empty")
		}
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		hash.Write(length[:])
		hash.Write([]byte(part))
	}

	return kind + "_" + hex.EncodeToString(hash.Sum(nil)), nil
}

func CardID(sourceID, sourceCardID, language string) (string, error) {
	return CanonicalID("card", sourceID, sourceCardID, language)
}

func PrintingID(sourceID, sourcePrintingID, language, variant string) (string, error) {
	return CanonicalID(
		"printing",
		sourceID,
		sourcePrintingID,
		language,
		defaultVariant(variant),
	)
}

func validIDKind(kind string) bool {
	for i, r := range kind {
		isLower := unicode.IsLower(r)
		isDigit := unicode.IsDigit(r)
		if (i == 0 && !isLower) || (i > 0 && !isLower && !isDigit && r != '_') {
			return false
		}
	}
	return kind != ""
}
