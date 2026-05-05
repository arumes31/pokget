//go:build cgo && (linux || darwin || freebsd)

package service

import (
	"bytes"
	"gettos/internal/models"
	"github.com/anthonynsimon/bild/adjust"
	"github.com/anthonynsimon/bild/effect"
	"github.com/otiai10/gosseract/v2"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"regexp"
)

func ProcessCardScan(imgBytes []byte, mockCards []models.Card) (string, string, error) {
	// 1. Preprocess image to harden OCR
	src, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return "", "", err
	}

	// Apply filters: Grayscale -> High Contrast -> Sharpness
	res := effect.Grayscale(src)
	res = adjust.Contrast(res, 0.5)
	res = adjust.Brightness(res, 0.1)
	res = effect.Sharpen(res)

	// Encode back to bytes for Tesseract
	buf := new(bytes.Buffer)
	err = jpeg.Encode(buf, res, nil)
	if err != nil {
		return "", "", err
	}

	// 2. Perform OCR
	client := gosseract.NewClient()
	defer client.Close()

	if err := client.SetImageFromBytes(buf.Bytes()); err != nil {
		return "", "", err
	}
	text, err := client.Text()
	if err != nil {
		return "", "", err
	}

	detectedCard := "Unknown Card"
	for _, card := range mockCards {
		// Use word boundary regex to avoid false positives (e.g., "Mew" matching "Mewtwo")
		pattern := `(?i)\b` + regexp.QuoteMeta(card.Name) + `\b`
		matched, _ := regexp.MatchString(pattern, text)
		if matched {
			detectedCard = card.Name
			break
		}
	}

	// 3. Fallback to CPU-based LLM if no exact match found
	if detectedCard == "Unknown Card" {
		llm := NewLLMService()
		match, err := llm.FuzzyMatchCard(text, mockCards)
		if err == nil && match != "Unknown Card" {
			detectedCard = match
		}
	}

	return text, detectedCard, nil
}
