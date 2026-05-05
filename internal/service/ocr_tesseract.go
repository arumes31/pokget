// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

//go:build cgo && (linux || darwin || freebsd)

package service

import (
	"bytes"
	"gettos/internal/models"
	"github.com/anthonynsimon/bild/adjust"
	"github.com/anthonynsimon/bild/effect"
	"github.com/otiai10/gosseract/v2"
	"image"
	_ "image/gif"  // Register GIF format for image.Decode
	"image/jpeg"
	_ "image/png"  // Register PNG format for image.Decode
	"log/slog"
	"regexp"
)

func ProcessCardScan(imgBytes []byte, mockCards []models.Card, lang string) (string, string, error) {
	if lang == "" {
		lang = "eng"
	}
	slog.Info("OCR: Starting scan...", "lang", lang)

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

	if err := client.SetLanguage(lang); err != nil {
		slog.Warn("OCR: Failed to set language, falling back to eng", "lang", lang, "error", err)
		_ = client.SetLanguage("eng")
	}

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
