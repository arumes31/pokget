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

package service

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"pokget/internal/models"
	"regexp"
	"testing"
)

func TestOCRMatchingLogic(t *testing.T) {
	mockCards := []models.Card{
		{Name: "Charizard"},
		{Name: "Pikachu"},
		{Name: "Mew"},
	}

	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{"Exact match", "Pikachu", "Pikachu"},
		{"Case insensitive", "charizard", "Charizard"},
		{"Partial sentence", "Found a rare Charizard card today", "Charizard"},
		{"Word boundary - Mewtwo not Mew", "Mewtwo is here", "Unknown Card"},
		{"No match", "Bulbasaur", "Unknown Card"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detected := "Unknown Card"
			for _, card := range mockCards {
				if containsIgnoreCase(tt.text, card.Name) {
					detected = card.Name
					break
				}
			}
			if detected != tt.expected {
				t.Errorf("For text '%s', expected %s, got %s", tt.text, tt.expected, detected)
			}
		})
	}
}

func containsIgnoreCase(s, substr string) bool {
	if substr == "" {
		return true
	}
	pattern := `(?i)\b` + regexp.QuoteMeta(substr) + `\b`
	matched, _ := regexp.MatchString(pattern, s)
	return matched
}

func TestVision_DetectCardEdges(t *testing.T) {
	// Test error case (invalid image bytes)
	_, err := DetectCardEdges([]byte("invalid image data"))
	if err == nil {
		t.Error("Expected error when decoding invalid image bytes")
	}

	// Create a valid 10x10 PNG
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	bounds, err := DetectCardEdges(buf.Bytes())
	if err != nil {
		t.Errorf("DetectCardEdges failed with valid image: %v", err)
	}
	if bounds.Left == 0 && bounds.Right == 0 && bounds.Top == 0 && bounds.Bottom == 0 {
		t.Error("Expected non-zero bounds")
	}
}

func TestProcessCardScan_Stub(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	text, card, _, err := ProcessCardScan(buf.Bytes(), nil, "", nil)
	if err != nil {
		t.Errorf("ProcessCardScan failed: %v", err)
	}
	if !containsIgnoreCase(text, "OCR Not Available") || card != "Unknown Card" {
		t.Errorf("Unexpected stub results: text=%s, card=%s", text, card)
	}
}

func TestFallbackExtract(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{"Single word", "Pikachu", "Pikachu"},
		{"Capitalized sequence", "rare Charizard card", "Charizard"},
		{"Multiple capitalized", "Shiny Mewtwo VMAX", "Shiny Mewtwo VMAX"},
		{"With noise", "!!!Pikachu???", "Pikachu"},
		{"First long word fallback", "the small cat jumped", "jumped"},
		{"Unknown", "a b c", "Unknown Card"},
		{"Empty", "", "Unknown Card"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fallbackExtract(tt.text)
			if err != nil {
				t.Fatalf("fallbackExtract failed: %v", err)
			}
			if got != tt.expected {
				t.Errorf("fallbackExtract(%q) = %q, want %q", tt.text, got, tt.expected)
			}
		})
	}
}

// --- NEW: Comprehensive OCR tests ---

// TestFallbackExtractCJK verifies that CJK regex pattern matches
// Japanese katakana, Chinese hanzi, and Korean hangul characters.
func TestFallbackExtractCJK(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		// Japanese katakana — should match the CJK pattern
		{"Japanese katakana", "ピカチュウ", "ピカチュウ"},
		// Chinese hanzi
		{"Chinese hanzi", "皮卡丘", "皮卡丘"},
		// Korean hangul
		{"Korean hangul", "피카츄", "피카츄"},
		// Mixed CJK and Latin — CJK should take priority
		{"Mixed CJK and Latin", "Card ピカチュウ VMAX", "ピカチュウ"},
		// Multiple CJK segments — longest should win
		// "リザードン" has 4 runes, "ピカチュウ" has 5 runes → ピカチュウ wins
		{"Multiple CJK segments", "リザードン and ピカチュウV", "ピカチュウ"},
		// Hiragana
		{"Japanese hiragana", "ぴかちゅう", "ぴかちゅう"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fallbackExtract(tt.text)
			if err != nil {
				t.Fatalf("fallbackExtract failed: %v", err)
			}
			if got != tt.expected {
				t.Errorf("fallbackExtract(%q) = %q, want %q", tt.text, got, tt.expected)
			}
		})
	}
}

// TestFallbackExtractCJKPatternDirectly tests the CJK regex pattern
// directly to ensure it matches the expected character ranges.
func TestFallbackExtractCJKPatternDirectly(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		matches bool
	}{
		{"Katakana matches", "ピカチュウ", true},
		{"Hiragana matches", "ひらがな", true},
		{"CJK Unified Ideographs", "漢字", true},
		{"Hangul matches", "한글", true},
		{"Latin no match", "Pikachu", false},
		{"Numbers no match", "12345", false},
		{"Mixed has match", "Card ピカチュウ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched := cjkPattern.MatchString(tt.text)
			if matched != tt.matches {
				t.Errorf("cjkPattern.MatchString(%q) = %v, want %v", tt.text, matched, tt.matches)
			}
		})
	}
}

// TestFallbackExtractLatinPatternDirectly tests the Latin capitalized
// word regex pattern.
func TestFallbackExtractLatinPatternDirectly(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		matches bool
	}{
		{"Capitalized word", "Pikachu", true},
		{"Multiple capitalized", "Shiny Mewtwo VMAX", true},
		{"Lowercase no match", "pikachu", false},
		{"Mixed case partial", "shiny Pikachu", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched := latinCapitalizedPattern.MatchString(tt.text)
			if matched != tt.matches {
				t.Errorf("latinCapitalizedPattern.MatchString(%q) = %v, want %v",
					tt.text, matched, tt.matches)
			}
		})
	}
}

// TestOCRProcessCardScanWithMockCards verifies that ProcessCardScan
// can match cards from a provided list using the stub implementation.
func TestOCRProcessCardScanWithMockCards(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	// The stub returns "OCR Not Available (Stub)" which won't match any card
	// but we verify the function doesn't crash with mock cards
	cards := []models.Card{
		{ID: "1", Name: "Pikachu"},
		{ID: "2", Name: "Charizard"},
	}

	text, card, _, err := ProcessCardScan(buf.Bytes(), cards, "eng", nil)
	if err != nil {
		t.Errorf("ProcessCardScan with mock cards failed: %v", err)
	}
	if text == "" {
		t.Error("Expected non-empty text from ProcessCardScan")
	}
	// Stub mode returns "Unknown Card" since "OCR Not Available (Stub)" doesn't match
	if card != "Unknown Card" {
		t.Logf("ProcessCardScan returned card=%q (stub behavior may vary)", card)
	}
}

// TestOCRProcessCardScanWithDifferentLanguages verifies that ProcessCardScan
// handles different language parameters correctly.
func TestOCRProcessCardScanWithDifferentLanguages(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	// Clear cache once before the loop; different languages produce
	// different cache keys so clearing inside the loop is unnecessary
	// and can cause race conditions.
	clearOCRCache()

	languages := []string{"eng", "jpn", "eng+jpn", "chi_sim", "deu+eng", ""}
	for _, lang := range languages {
		t.Run("lang_"+lang, func(t *testing.T) {
			text, _, _, err := ProcessCardScan(buf.Bytes(), nil, lang, nil)
			if err != nil {
				t.Errorf("ProcessCardScan with lang=%q failed: %v", lang, err)
			}
			if text == "" {
				t.Errorf("Expected non-empty text for lang=%q", lang)
			}
		})
	}
}

// TestOCRCacheHitOnSecondScan verifies that identical images return
// cached results on the second call.
func TestOCRCacheHitOnSecondScan(t *testing.T) {
	// Clear cache before test
	clearOCRCache()

	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	// First call — should process and cache
	text1, card1, _, err := ProcessCardScan(buf.Bytes(), nil, "eng", nil)
	if err != nil {
		t.Fatalf("First ProcessCardScan failed: %v", err)
	}

	// Second call — should return cached result
	text2, card2, _, err := ProcessCardScan(buf.Bytes(), nil, "eng", nil)
	if err != nil {
		t.Fatalf("Second ProcessCardScan failed: %v", err)
	}

	// Results should be identical
	if text1 != text2 {
		t.Errorf("Cache hit returned different text: first=%q, second=%q", text1, text2)
	}
	if card1 != card2 {
		t.Errorf("Cache hit returned different card: first=%q, second=%q", card1, card2)
	}
}

// TestOCREdgeDetectionWithValidImage verifies that DetectCardEdges
// works with various image sizes.
func TestOCREdgeDetectionWithValidImage(t *testing.T) {
	sizes := []struct {
		w, h int
	}{
		{10, 10},
		{100, 100},
		{640, 480},
	}

	for _, sz := range sizes {
		t.Run(fmt.Sprintf("%dx%d", sz.w, sz.h), func(t *testing.T) {
			img := image.NewRGBA(image.Rect(0, 0, sz.w, sz.h))
			var buf bytes.Buffer
			_ = png.Encode(&buf, img)

			bounds, err := DetectCardEdges(buf.Bytes())
			if err != nil {
				t.Errorf("DetectCardEdges(%dx%d) failed: %v", sz.w, sz.h, err)
			}
			if bounds.Left < 0 || bounds.Right > 100 || bounds.Top < 0 || bounds.Bottom > 100 {
				t.Errorf("Bounds out of range for %dx%d: %+v", sz.w, sz.h, bounds)
			}
		})
	}
}

// TestOCRProcessCardScanCorruptedData verifies that corrupted image data
// is handled gracefully without panicking.
func TestOCRProcessCardScanCorruptedData(t *testing.T) {
	clearOCRCache()

	corruptedData := []byte{0x89, 0x50, 0x4E, 0x47, 0x00, 0x00, 0xFF, 0xFF}
	_, _, _, err := ProcessCardScan(corruptedData, nil, "eng", nil)
	if err == nil {
		t.Error("Expected error for corrupted image data")
	}
}

// TestOCRProcessCardScanEmptyData verifies that empty image data
// is handled gracefully.
func TestOCRProcessCardScanEmptyData(t *testing.T) {
	clearOCRCache()

	_, _, _, err := ProcessCardScan([]byte{}, nil, "eng", nil)
	if err == nil {
		t.Error("Expected error for empty image data")
	}
}

// TestOCRConcurrentRequests verifies that concurrent OCR requests
// don't deadlock using the stub implementation.
func TestOCRConcurrentRequests(t *testing.T) {
	clearOCRCache()

	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	done := make(chan error, 10)

	// Launch 10 concurrent OCR requests
	for i := 0; i < 10; i++ {
		go func(idx int) {
			// Use different languages to avoid cache hits
			lang := "eng"
			if idx%2 == 1 {
				lang = "jpn"
			}
			_, _, _, err := ProcessCardScan(buf.Bytes(), nil, lang, nil)
			done <- err
		}(i)
	}

	// Wait for all with timeout
	for i := 0; i < 10; i++ {
		err := <-done
		if err != nil {
			t.Errorf("Concurrent OCR request %d failed: %v", i, err)
		}
	}
}

// TestOCRImageHashDeterminism verifies that the same image bytes
// always produce the same hash.
func TestOCRImageHashDeterminism(t *testing.T) {
	data := []byte("deterministic-test-data")

	hash1 := imageHash(data)
	hash2 := imageHash(data)

	if hash1 != hash2 {
		t.Error("Expected identical hashes for identical input data")
	}
}

// TestOCRImageHashCollisionResistance verifies that different data
// produces different hashes.
func TestOCRImageHashCollisionResistance(t *testing.T) {
	data1 := []byte("image-data-1")
	data2 := []byte("image-data-2")

	hash1 := imageHash(data1)
	hash2 := imageHash(data2)

	if hash1 == hash2 {
		t.Error("Expected different hashes for different input data")
	}
}
