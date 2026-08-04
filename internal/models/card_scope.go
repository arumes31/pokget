package models

import (
	"fmt"
	"strings"
)

// TCG is a normalized trading-card-game catalog identifier.
type TCG string

const (
	TCGUnknown      TCG = ""
	TCGPokemon      TCG = "pokemon"
	TCGMagic        TCG = "magic"
	TCGOnePiece     TCG = "one_piece"
	TCGLorcana      TCG = "lorcana"
	TCGWeissSchwarz TCG = "weiss_schwarz"
	TCGYuGiOh       TCG = "yugioh"
)

// ParseTCG converts a user-facing game label into a supported catalog ID.
func ParseTCG(value string) (TCG, error) {
	normalized := TCG(NormalizeGame(value))
	if !normalized.Valid() {
		return TCGUnknown, fmt.Errorf("models: unsupported tcg %q", value)
	}
	return normalized, nil
}

// Valid reports whether t identifies a supported catalog.
func (t TCG) Valid() bool {
	switch t {
	case TCGPokemon, TCGMagic, TCGOnePiece, TCGLorcana, TCGWeissSchwarz, TCGYuGiOh:
		return true
	default:
		return false
	}
}

// Language is a normalized card-language identifier.
type Language string

const (
	LanguageUnknown            Language = ""
	LanguageEnglish            Language = "en"
	LanguageJapanese           Language = "ja"
	LanguageGerman             Language = "de"
	LanguageFrench             Language = "fr"
	LanguageChineseSimplified  Language = "zh-hans"
	LanguageChineseTraditional Language = "zh-hant"
	LanguageKorean             Language = "ko"
)

// Valid reports whether l identifies a supported card language.
func (l Language) Valid() bool {
	switch l {
	case LanguageEnglish, LanguageJapanese, LanguageGerman, LanguageFrench,
		LanguageChineseSimplified, LanguageChineseTraditional, LanguageKorean:
		return true
	default:
		return false
	}
}

// ParseLanguage normalizes catalog and Tesseract language labels.
func ParseLanguage(value string) (Language, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")

	var language Language
	switch normalized {
	case "en", "eng", "english":
		language = LanguageEnglish
	case "ja", "jp", "jpn", "japanese":
		language = LanguageJapanese
	case "de", "deu", "ger", "german":
		language = LanguageGerman
	case "fr", "fra", "fre", "french":
		language = LanguageFrench
	case "zh", "zh-cn", "zh-hans", "chi-sim":
		language = LanguageChineseSimplified
	case "zh-tw", "zh-hant", "chi-tra":
		language = LanguageChineseTraditional
	case "ko", "kor", "korean":
		language = LanguageKorean
	default:
		return LanguageUnknown, fmt.Errorf("models: unsupported language %q", value)
	}
	return language, nil
}

// TesseractCode returns the language data code used by the OCR implementation.
func (l Language) TesseractCode() string {
	switch l {
	case LanguageEnglish:
		return "eng"
	case LanguageJapanese:
		return "jpn"
	case LanguageGerman:
		return "deu"
	case LanguageFrench:
		return "fra"
	case LanguageChineseSimplified:
		return "chi_sim"
	case LanguageChineseTraditional:
		return "chi_tra"
	case LanguageKorean:
		return "kor"
	default:
		return ""
	}
}

// Matches reports whether a catalog language label identifies l.
func (l Language) Matches(value string) bool {
	parsed, err := ParseLanguage(value)
	return err == nil && parsed == l
}
