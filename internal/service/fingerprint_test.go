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
	"image"
	"image/color"
	"image/draw"
	"pokget/internal/models"
	"testing"
)

func createTestImage(c color.Color, pattern string) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	
	if pattern == "circle" {
		// Draw a simple "circle" (square in the middle)
		draw.Draw(img, image.Rect(25, 25, 75, 75), &image.Uniform{c}, image.Point{}, draw.Src)
	} else if pattern == "stripe" {
		// Draw a stripe
		draw.Draw(img, image.Rect(0, 40, 100, 60), &image.Uniform{c}, image.Point{}, draw.Src)
	} else {
		draw.Draw(img, img.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
	}
	return img
}

func TestFingerprintCalculationAndMatching(t *testing.T) {
	svc := NewFingerprintService(nil)

	// Create two different patterned images
	imgRed := createTestImage(color.RGBA{255, 0, 0, 255}, "circle")
	imgBlue := createTestImage(color.RGBA{0, 0, 255, 255}, "stripe")

	hashRed, err := svc.CalculateHash(imgRed)
	if err != nil {
		t.Fatalf("Failed to calculate hash: %v", err)
	}

	hashBlue, err := svc.CalculateHash(imgBlue)
	if err != nil {
		t.Fatalf("Failed to calculate hash: %v", err)
	}

	if hashRed == hashBlue {
		t.Errorf("Expected different hashes for red and blue images, got both %d", hashRed)
	}

	// Test Matching
	cards := []models.Card{
		{ID: "red", Name: "Red Card", Phash: &hashRed},
		{ID: "blue", Name: "Blue Card", Phash: &hashBlue},
	}

	// Match red image against cards
	match, distance, err := svc.MatchFingerprint(hashRed, cards)
	if err != nil {
		t.Fatalf("MatchFingerprint failed: %v", err)
	}

	if match == nil || match.ID != "red" {
		t.Errorf("Expected match for 'red', got %v (distance %d)", match, distance)
	}

	if distance != 0 {
		t.Errorf("Expected distance 0 for exact match, got %d", distance)
	}

	// Test distance threshold
	matchFail, distanceFail, _ := svc.MatchFingerprint(hashRed, []models.Card{{ID: "blue", Name: "Blue", Phash: &hashBlue}})
	if matchFail != nil {
		t.Errorf("Expected no match for very different images, got %v (distance %d)", matchFail, distanceFail)
	}
}

func TestMetadataServiceProcessing(t *testing.T) {
	// MetadataService depends on FingerprintService
	fSvc := NewFingerprintService(nil)
	mSvc := NewMetadataService(fSvc)

	// We can't easily test the network download part without mocking http.Client
	// but we can test the logic if we split ProcessCard or mock the download.
	// For now, let's just ensure NewMetadataService works.
	if mSvc == nil {
		t.Fatal("Expected MetadataService to be initialized")
	}
}
