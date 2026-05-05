//go:build !cgo || (!linux && !darwin && !freebsd)

package service

import (
	"gettos/internal/models"
	"log/slog"
)

func ProcessCardScan(_ []byte, _ []models.Card) (string, string, error) {
	slog.Warn("OCR: Tesseract is not available on this platform or CGO is disabled. Falling back to dummy result.")
	return "OCR Not Available", "Unknown Card", nil
}
