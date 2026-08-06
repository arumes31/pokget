//go:build !cgo || (!linux && !darwin && !freebsd)

package service

import (
	"context"
	"fmt"
	"log/slog"
	"pokget/internal/models"
)

func ProcessCardScan(imgBytes []byte, cards []models.Card, lang string, _ *LLMService) (string, string, []byte, error) {
	return ProcessCardScanContext(context.Background(), imgBytes, cards, lang, nil)
}

func ProcessCardScanContext(ctx context.Context, imgBytes []byte, cards []models.Card, lang string, _ *LLMService) (string, string, []byte, error) {
	if err := ctx.Err(); err != nil {
		return "", "", nil, err
	}
	if lang == "" {
		lang = defaultOCRLanguage
	}
	config := ocrScanConfigFromContext(ctx)
	if config.Game == "" {
		if game := inferOCRGame(cards); game != "" {
			config.Game = game
			config.UseLayoutROIs = true
		}
	}
	unavailable := &OCRUnavailableError{Reason: "this build does not include Tesseract support"}

	cacheKey := makeOCRCacheKeyWithConfig(imgBytes, lang, cards, config)
	if cached, ok := ocrCache.Load(cacheKey); ok {
		entry := cached.(ocrCacheEntry)
		return entry.Text, entry.DetectedCard, entry.ProcessedImage, unavailable
	}

	src, _, err := decodeOCRImage(imgBytes, config)
	if err != nil {
		return "", "", nil, err
	}
	src, err = prepareOCRSource(imgBytes, src, config)
	if err != nil {
		return "", "", nil, err
	}
	if err := ctx.Err(); err != nil {
		return "", "", nil, err
	}

	processedImage, err := buildOCRPreview(src)
	if err != nil {
		return "", "", nil, fmt.Errorf("prepare OCR image: %w", err)
	}
	text := "OCR Not Available (Stub)"
	detectedCard := "Unknown Card"
	entry := ocrCacheEntry{Text: text, DetectedCard: detectedCard, ProcessedImage: processedImage}
	ocrCache.Store(cacheKey, entry)
	slog.Warn("OCR preprocessing completed without Tesseract", "game", config.Game)
	return text, detectedCard, append([]byte(nil), processedImage...), unavailable
}
