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
	"github.com/anthonynsimon/bild/adjust"
	"github.com/anthonynsimon/bild/effect"
	"github.com/anthonynsimon/bild/transform"
	"github.com/otiai10/gosseract/v2"
	"image"
	_ "image/gif" // Register GIF format for image.Decode
	"image/jpeg"
	_ "image/png" // Register PNG format for image.Decode
	"log/slog"
	"pokget/internal/db"
	"pokget/internal/models"
	"strings"
	"sync"
)

var ocrMu sync.Mutex

func ProcessCardScan(imgBytes []byte, mockCards []models.Card, lang string, llm *LLMService) (string, string, []byte, error) {
	if lang == "" {
		lang = "eng+jpn+deu+fra+chi_sim+chi_tra+kor"
	}
	slog.Info("OCR: Starting scan...", "lang", lang)

	// 1. Preprocess image to harden OCR
	src, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return "", "", nil, err
	}

	// Apply enhanced filters: Resize (2x) -> Grayscale -> Balanced Contrast -> Sharpness
	bounds := src.Bounds()
	res := transform.Resize(src, bounds.Dx()*2, bounds.Dy()*2, transform.Lanczos)
	res = effect.Grayscale(res)
	res = adjust.Contrast(res, 0.3) // Tone down contrast to avoid blowout
	res = adjust.Brightness(res, 0.05)
	res = effect.Sharpen(res)

	// Encode back to bytes with high quality
	buf := new(bytes.Buffer)
	err = jpeg.Encode(buf, res, &jpeg.Options{Quality: 95})
	if err != nil {
		return "", "", nil, err
	}

	// 2. Perform OCR
	slog.Info("OCR: Initializing Tesseract client...")
	client := gosseract.NewClient()
	defer client.Close()

	slog.Info("OCR: Setting image data...")
	_ = client.SetLanguage(lang)
	_ = client.SetImageFromBytes(buf.Bytes())

	slog.Info("OCR: Executing Tesseract (Locking)...")
	ocrMu.Lock()
	text, err := client.Text()
	ocrMu.Unlock()
	slog.Info("OCR: Tesseract execution released")
	if err != nil {
		slog.Error("OCR: Tesseract execution failed", "error", err)
		return "", "", buf.Bytes(), err
	}
	slog.Info("OCR: Tesseract complete", "text_len", len(text), "raw_text", text)

	// 3. Perfect Detection Logic: Database-Driven Fuzzy Match
	detectedCard := "Unknown Card"

	// Special handling for Japanese/Chinese (CJK): remove spaces for better matching
	normalizedText := text
	if lang == "jpn" || lang == "chi_sim" || lang == "chi_tra" {
		normalizedText = strings.ReplaceAll(text, " ", "")
		normalizedText = strings.ReplaceAll(normalizedText, "\n", "")
	}
	slog.Info("OCR: Normalized text", "normalized_text", normalizedText)

	// SQL-based Trigram matching (High performance)
	if db.DB != nil {
		var name string
		slog.Info("OCR: Attempting SQL Trigram match", "text", normalizedText)
		err := db.DB.QueryRow(`
			SELECT name FROM cards 
			WHERE name % $1 
			ORDER BY similarity(name, $1) DESC 
			LIMIT 1`, normalizedText).Scan(&name)

		if err == nil {
			slog.Info("OCR: SQL match found", "name", name)
			detectedCard = name
		} else {
			slog.Info("OCR: SQL match failed or no match", "error", err)
		}
	}

	// Stage 4: LLM Refinement if still unsure
	if detectedCard == "Unknown Card" && llm != nil {
		slog.Info("OCR: Falling back to LLM refinement")
		match, err := llm.FuzzyMatchCard(normalizedText, mockCards)
		if err == nil && match != "Unknown Card" {
			slog.Info("OCR: LLM match found", "match", match)
			detectedCard = match
		} else {
			slog.Info("OCR: LLM match failed or returned unknown", "error", err, "match", match)
		}
	}

	slog.Info("OCR: Final result", "detected", detectedCard)
	return normalizedText, detectedCard, buf.Bytes(), nil
}
