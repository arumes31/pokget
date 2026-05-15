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
	"pokget/internal/db"
	"pokget/internal/models"

	"github.com/anthonynsimon/bild/adjust"
	"github.com/anthonynsimon/bild/effect"
)



func ProcessCardScan(imgBytes []byte, _ []models.Card, _ string, _ *LLMService) (string, string, []byte, error) {
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

	// SQL-based Trigram matching (High performance)
	if db.DB != nil {
		var name string
		err := db.DB.QueryRow(`
			SELECT name FROM cards 
			WHERE name % $1 
			ORDER BY similarity(name, $1) DESC 
			LIMIT 1`, text).Scan(&name)
		
		if err == nil {
			detectedCard = name
		}
	}

	return text, detectedCard, buf.Bytes(), nil
}
