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
	"regexp"
	"strings"
)

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

	// 2. Perform OCR (Simulated if stubbed)
	client := gosseract.NewClient()
	defer client.Close()

	_ = client.SetLanguage(lang)
	_ = client.SetImageFromBytes(buf.Bytes())
	text, err := client.Text()
	if err != nil {
		return "", "", buf.Bytes(), err
	}

	// 3. Perfect Detection Logic: Multi-Stage Matching
	detectedCard := "Unknown Card"
	bestScore := 0.7 // Threshold for fuzzy match

	// Special handling for Japanese/Chinese (CJK): remove spaces for better matching
	normalizedText := text
	if lang == "jpn" || lang == "chi_sim" || lang == "chi_tra" {
		normalizedText = strings.ReplaceAll(text, " ", "")
		normalizedText = strings.ReplaceAll(normalizedText, "\n", "")
	}

	for _, card := range mockCards {
		// Stage 1: Exact/Word Boundary Match (Fast)
		pattern := `(?i)\b` + regexp.QuoteMeta(card.Name) + `\b`
		if matched, _ := regexp.MatchString(pattern, normalizedText); matched {
			detectedCard = card.Name
			break
		}

		// Stage 2: Levenshtein Fuzzy Match
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
