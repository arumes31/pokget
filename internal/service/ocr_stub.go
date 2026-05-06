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

//go:build !cgo || (!linux && !darwin && !freebsd)

package service

import (
	"bytes"
	"image"
	"image/jpeg"
	"log/slog"
	"pokget/internal/models"
	"strings"

	"github.com/anthonynsimon/bild/adjust"
	"github.com/anthonynsimon/bild/effect"
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

func min(a, b int) int {
	if a < b { return a }
	return b
}

func ProcessCardScan(imgBytes []byte, mockCards []models.Card, _ string) (string, string, []byte, error) {
	slog.Warn("OCR: Tesseract is not available on this platform. Preprocessing ONLY.")

	// Run Preprocessing even in stub to test Vision pipeline
	src, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return "", "", nil, err
	}

	res := effect.Grayscale(src)
	res = adjust.Contrast(res, 0.7)
	res = adjust.Brightness(res, 0.1)
	res = effect.Sharpen(res)

	buf := new(bytes.Buffer)
	_ = jpeg.Encode(buf, res, nil)

	// Mock detected text for testing matching logic
	text := "OCR Not Available (Stub)"
	detectedCard := "Unknown Card"
	bestScore := 0.7

	for _, card := range mockCards {
		dist := levenshtein(text, card.Name)
		maxLen := len(text)
		if len(card.Name) > maxLen { maxLen = len(card.Name) }
		if maxLen == 0 { continue }
		
		score := 1.0 - float64(dist)/float64(maxLen)
		if score > bestScore {
			bestScore = score
			detectedCard = card.Name
		}
	}

	return text, detectedCard, buf.Bytes(), nil
}
