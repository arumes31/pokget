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
	"pokget/internal/models"
	"github.com/anthonynsimon/bild/adjust"
	"github.com/anthonynsimon/bild/effect"
	"github.com/otiai10/gosseract/v2"
	"image"
	_ "image/gif"  // Register GIF format for image.Decode
	"image/jpeg"
	_ "image/png"  // Register PNG format for image.Decode
	"log/slog"
	"strings"
	"sync"
)

var ocrMu sync.Mutex

func levenshtein(s1, s2 string) int {
	s1 = strings.ToLower(s1)
	s2 = strings.ToLower(s2)
	n, m := len(s1), len(s2)
	if n == 0 { return m }
	if m == 0 { return n }
	d := make([][]int, n+1)
	for i := range d {
		d[i] = make([]int, m+1)
		d[i][0] = i
	}
	for j := 0; j <= m; j++ {
		d[0][j] = j
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, min(d[i][j-1]+1, d[i-1][j-1]+cost))
		}
	}
	return d[n][m]
}


func ProcessCardScan(imgBytes []byte, mockCards []models.Card, lang string) (string, string, []byte, error) {
	if lang == "" {
		lang = "eng"
	}
	slog.Info("OCR: Starting scan...", "lang", lang)

	// 1. Preprocess image to harden OCR
	src, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return "", "", nil, err
	}

	// Apply enhanced filters: Grayscale -> Adaptive Contrast -> Sharpen
	res := effect.Grayscale(src)
	res = adjust.Contrast(res, 0.7) // Increased contrast
	res = adjust.Brightness(res, 0.1)
	res = effect.Sharpen(res)

	// Encode back to bytes
	buf := new(bytes.Buffer)
	err = jpeg.Encode(buf, res, nil)
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
	slog.Info("OCR: Tesseract complete", "text_len", len(text))

	// 3. Perfect Detection Logic: Multi-Stage Matching
	detectedCard := "Unknown Card"
	bestScore := 0.7 // Threshold for fuzzy match

	// Special handling for Japanese/Chinese (CJK): remove spaces for better matching
	normalizedText := text
	if lang == "jpn" || lang == "chi_sim" || lang == "chi_tra" {
		normalizedText = strings.ReplaceAll(text, " ", "")
		normalizedText = strings.ReplaceAll(normalizedText, "\n", "")
	}

	// Optimization: Lowercase text once
	lowerText := strings.ToLower(normalizedText)

	for _, card := range mockCards {
		cardNameLower := strings.ToLower(card.Name)
		
		// Stage 1: Fast exact substring check
		if strings.Contains(lowerText, cardNameLower) {
			detectedCard = card.Name
			break
		}

		// Stage 2: Length-based pre-filter for Levenshtein (must be within 40% length)
		lenDiff := len(normalizedText) - len(card.Name)
		if lenDiff < 0 { lenDiff = -lenDiff }
		if lenDiff > len(card.Name)/2 && len(card.Name) > 5 {
			continue
		}

		// Stage 3: Levenshtein Fuzzy Match
		dist := levenshtein(normalizedText, card.Name)
		maxLen := len(normalizedText)
		if len(card.Name) > maxLen { maxLen = len(card.Name) }
		if maxLen == 0 { continue }
		
		score := 1.0 - float64(dist)/float64(maxLen)
		if score > bestScore {
			bestScore = score
			detectedCard = card.Name
		}
	}

	// Stage 3: LLM Refinement if still unsure
	if detectedCard == "Unknown Card" {
		llm := NewLLMService()
		match, err := llm.FuzzyMatchCard(normalizedText, mockCards)
		if err == nil && match != "Unknown Card" {
			detectedCard = match
		}
	}

	return normalizedText, detectedCard, buf.Bytes(), nil
}
