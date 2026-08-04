package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	defaultOCRLanguage  = "eng+jpn+deu+fra+chi_sim+chi_tra+kor"
	defaultOCRMaxBytes  = 20 << 20
	defaultOCRMaxPixels = int64(40_000_000)
)

// OCRNormalizedRect describes a crop using coordinates in the inclusive
// 0..1 range. It is useful for camera guides because it is resolution
// independent.
type OCRNormalizedRect struct {
	MinX float64
	MinY float64
	MaxX float64
	MaxY float64
}

// OCRScanConfig controls validation and game-specific preprocessing. Existing
// callers can keep using ProcessCardScanContext without setting a config.
type OCRScanConfig struct {
	Game           string
	GuideCrop      *OCRNormalizedRect
	MaxInputBytes  int
	MaxPixels      int64
	AllowedFormats []string
	UseLayoutROIs  bool
}

type ocrScanConfigContextKey struct{}

// WithOCRScanConfig returns a context carrying OCR preprocessing options.
func WithOCRScanConfig(ctx context.Context, config OCRScanConfig) context.Context {
	return context.WithValue(ctx, ocrScanConfigContextKey{}, config)
}

func ocrScanConfigFromContext(ctx context.Context) OCRScanConfig {
	if ctx != nil {
		if config, ok := ctx.Value(ocrScanConfigContextKey{}).(OCRScanConfig); ok {
			return normalizeOCRScanConfig(config)
		}
	}
	return normalizeOCRScanConfig(OCRScanConfig{})
}

func normalizeOCRScanConfig(config OCRScanConfig) OCRScanConfig {
	config.Game = normalizeGame(config.Game)
	if config.MaxInputBytes <= 0 {
		config.MaxInputBytes = defaultOCRMaxBytes
	}
	if config.MaxPixels <= 0 {
		config.MaxPixels = defaultOCRMaxPixels
	}
	if len(config.AllowedFormats) == 0 {
		config.AllowedFormats = []string{"jpeg", "png", "webp"}
	} else {
		formats := make([]string, 0, len(config.AllowedFormats))
		seen := make(map[string]struct{}, len(config.AllowedFormats))
		for _, format := range config.AllowedFormats {
			format = strings.ToLower(strings.TrimSpace(format))
			if format == "jpg" {
				format = "jpeg"
			}
			if format == "" {
				continue
			}
			if _, ok := seen[format]; ok {
				continue
			}
			seen[format] = struct{}{}
			formats = append(formats, format)
		}
		config.AllowedFormats = formats
	}
	return config
}

func normalizeGame(game string) string {
	game = strings.ToLower(strings.TrimSpace(game))
	game = strings.NewReplacer("-", "_", " ", "_").Replace(game)
	switch game {
	case "pokemon", "pokémon", "pokemon_tcg":
		return "pokemon"
	case "magic", "mtg", "magic_the_gathering":
		return "magic"
	case "onepiece", "one_piece", "one_piece_card_game":
		return "one_piece"
	case "lorcana", "disney_lorcana":
		return "lorcana"
	case "weiss", "weiss_schwarz", "weiß_schwarz":
		return "weiss_schwarz"
	case "yugioh", "yu_gi_oh", "yu_gi_oh!":
		return "yugioh"
	default:
		return game
	}
}

// OCRInputError reports that an image was rejected before OCR execution.
type OCRInputError struct {
	Reason string
	Err    error
}

func (e *OCRInputError) Error() string {
	if e == nil {
		return "invalid OCR input"
	}
	if e.Err != nil {
		return fmt.Sprintf("invalid OCR input: %s: %v", e.Reason, e.Err)
	}
	return "invalid OCR input: " + e.Reason
}

func (e *OCRInputError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// OCRUnavailableError reports that this build cannot execute OCR. The
// processed image is still returned so callers can inspect preprocessing.
type OCRUnavailableError struct {
	Reason string
}

func (e *OCRUnavailableError) Error() string {
	if e == nil || e.Reason == "" {
		return "OCR is unavailable"
	}
	return "OCR is unavailable: " + e.Reason
}

// OCRAllPassesFailedError reports that Tesseract could not complete any of the
// generated passes.
type OCRAllPassesFailedError struct {
	Failures []error
}

func (e *OCRAllPassesFailedError) Error() string {
	if e == nil || len(e.Failures) == 0 {
		return "all OCR passes failed"
	}
	return fmt.Sprintf("all OCR passes failed (%d failures): %v", len(e.Failures), errors.Join(e.Failures...))
}

func (e *OCRAllPassesFailedError) Unwrap() []error {
	if e == nil {
		return nil
	}
	return e.Failures
}
