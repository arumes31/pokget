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
	"image"
	"image/color"
	"image/draw"
	"testing"
)

// --- SCAN-04: Conditional upscaling tests ---

func TestComputeUpscaleFactors(t *testing.T) {
	tests := []struct {
		name      string
		width     int
		height    int
		wantScale float64
	}{
		{"Small image - 2x upscale", 400, 300, 2.0},
		{"Small image square", 500, 500, 2.0},
		{"Medium image - 1.5x upscale", 1000, 800, 1.5},
		{"Medium image portrait", 800, 1200, 1.5},
		{"Large image - no upscale", 2000, 1500, 1.0},
		{"Large phone photo", 3000, 4000, 1.0},
		{"Boundary at minUpscaleDim", 800, 600, 1.5},
		{"Boundary at maxUpscaleDim", 1500, 1000, 1.0},
		{"Just below minUpscaleDim", 799, 600, 2.0},
		{"Just above maxUpscaleDim", 1501, 1000, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bounds := image.Rect(0, 0, tt.width, tt.height)
			scaleX, scaleY := computeUpscaleFactors(bounds)
			if scaleX != tt.wantScale || scaleY != tt.wantScale {
				t.Errorf("computeUpscaleFactors(%dx%d) = (%f, %f), want (%f, %f)",
					tt.width, tt.height, scaleX, scaleY, tt.wantScale, tt.wantScale)
			}
		})
	}
}

// --- SCAN-05: Image preprocessing tests ---

func TestOtsuBinarize(t *testing.T) {
	// Create a simple image with black text on white background
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	// Fill with white
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	// Draw a black rectangle in the center (simulating text)
	for y := 40; y < 60; y++ {
		for x := 20; x < 80; x++ {
			img.Set(x, y, color.Black)
		}
	}

	result := otsuBinarize(img)
	if result == nil {
		t.Fatal("Expected non-nil result from otsuBinarize")
	}

	// Check bounds are preserved
	if result.Bounds() != img.Bounds() {
		t.Errorf("Bounds mismatch: got %v, want %v", result.Bounds(), img.Bounds())
	}

	// Check that the black area is still black
	r, _, _, _ := result.At(50, 50).RGBA()
	if r > 32768 {
		t.Error("Expected black pixel in center to remain black after binarization")
	}

	// Check that the white area is still white
	r, _, _, _ = result.At(5, 5).RGBA()
	if r < 32768 {
		t.Error("Expected white pixel in corner to remain white after binarization")
	}
}

func TestOtsuBinarizeAllBlack(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)

	result := otsuBinarize(img)
	if result == nil {
		t.Fatal("Expected non-nil result from otsuBinarize for all-black image")
	}
}

func TestOtsuBinarizeAllWhite(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	result := otsuBinarize(img)
	if result == nil {
		t.Fatal("Expected non-nil result from otsuBinarize for all-white image")
	}
}

func TestDeskewImage(t *testing.T) {
	// Create a simple image that doesn't need deskewing
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	// Draw horizontal lines (text-like)
	for y := 20; y < 80; y += 15 {
		for x := 10; x < 90; x++ {
			img.Set(x, y, color.Black)
		}
	}

	result := deskewImage(img)
	if result == nil {
		t.Fatal("Expected non-nil result from deskewImage")
	}
}

func TestDeskewImageTooSmall(t *testing.T) {
	// Small image should be returned as-is
	img := image.NewRGBA(image.Rect(0, 0, 30, 30))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	result := deskewImage(img)
	if result == nil {
		t.Fatal("Expected non-nil result from deskewImage for small image")
	}
}

func TestHorizontalLineScore(t *testing.T) {
	// Create image with horizontal lines (high score expected)
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	for y := 20; y < 80; y += 10 {
		for x := 10; x < 90; x++ {
			img.Set(x, y, color.Black)
		}
	}

	score := horizontalLineScore(img)
	if score <= 0 {
		t.Errorf("Expected positive horizontal line score for image with horizontal lines, got %f", score)
	}

	// Uniform image should have low score
	uniform := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(uniform, uniform.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	uniformScore := horizontalLineScore(uniform)
	if uniformScore != 0 {
		t.Errorf("Expected 0 horizontal line score for uniform image, got %f", uniformScore)
	}
}

func TestPreprocessForOCR(t *testing.T) {
	// Create a test image
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	for y := 50; y < 150; y += 20 {
		for x := 20; x < 180; x++ {
			img.Set(x, y, color.Black)
		}
	}

	result := preprocessForOCR(img)
	if result == nil {
		t.Fatal("Expected non-nil result from preprocessForOCR")
	}
}

// --- SCAN-11: EXIF orientation tests ---

func TestGetEXIFOrientationNonJPEG(t *testing.T) {
	// PNG data should return 0
	pngData := []byte{0x89, 0x50, 0x4E, 0x47}
	orientation := getEXIFOrientation(pngData)
	if orientation != 0 {
		t.Errorf("Expected orientation 0 for non-JPEG data, got %d", orientation)
	}
}

func TestGetEXIFOrientationEmptyData(t *testing.T) {
	orientation := getEXIFOrientation([]byte{})
	if orientation != 0 {
		t.Errorf("Expected orientation 0 for empty data, got %d", orientation)
	}
}

func TestGetEXIFOrientationShortData(t *testing.T) {
	orientation := getEXIFOrientation([]byte{0xFF, 0xD8})
	if orientation != 0 {
		t.Errorf("Expected orientation 0 for short JPEG data, got %d", orientation)
	}
}

func TestGetEXIFOrientationJPEGNoEXIF(t *testing.T) {
	// JPEG SOI + EOI markers, no EXIF
	data := []byte{0xFF, 0xD8, 0xFF, 0xD9}
	orientation := getEXIFOrientation(data)
	if orientation != 0 {
		t.Errorf("Expected orientation 0 for JPEG without EXIF, got %d", orientation)
	}
}

func TestApplyEXIFOrientationNoChange(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 0, 0, 255}}, image.Point{}, draw.Src)

	// Non-JPEG data should return image unchanged
	result := applyEXIFOrientation([]byte{0x89, 0x50, 0x4E, 0x47}, img)
	if result != img {
		t.Error("Expected same image for non-JPEG data")
	}
}

func TestApplyEXIFOrientationIdentity(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 0, 0, 255}}, image.Point{}, draw.Src)

	// Empty data (orientation 0) should return image unchanged
	result := applyEXIFOrientation([]byte{}, img)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
}

// --- SCAN-11: Rotation helper tests ---

func TestRotate90CW(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 40))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 0, 0, 255}}, image.Point{}, draw.Src)

	result := rotate90CW(img)
	if result == nil {
		t.Fatal("Expected non-nil result from rotate90CW")
	}
	// After 90° CW rotation, width and height should swap
	if result.Bounds().Dx() != 40 {
		t.Errorf("Expected width 40 after 90° CW rotation, got %d", result.Bounds().Dx())
	}
	if result.Bounds().Dy() != 20 {
		t.Errorf("Expected height 20 after 90° CW rotation, got %d", result.Bounds().Dy())
	}
}

func TestRotate90CCW(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 40))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0, 0, 255, 255}}, image.Point{}, draw.Src)

	result := rotate90CCW(img)
	if result == nil {
		t.Fatal("Expected non-nil result from rotate90CCW")
	}
	// After 90° CCW rotation, width and height should swap
	if result.Bounds().Dx() != 40 {
		t.Errorf("Expected width 40 after 90° CCW rotation, got %d", result.Bounds().Dx())
	}
	if result.Bounds().Dy() != 20 {
		t.Errorf("Expected height 20 after 90° CCW rotation, got %d", result.Bounds().Dy())
	}
}

func TestFlipHorizontal(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 50))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	// Draw a red pixel at (0, 0)
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})

	dst := image.NewRGBA(img.Bounds())
	flipHorizontal(dst, img)

	// After horizontal flip, the red pixel should be at (99, 0)
	r, _, _, _ := dst.At(99, 0).RGBA()
	if r < 50000 {
		t.Error("Expected red pixel at (99, 0) after horizontal flip")
	}
}

func TestFlipVertical(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 50))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	img.Set(0, 0, color.RGBA{0, 0, 255, 255})

	dst := image.NewRGBA(img.Bounds())
	flipVertical(dst, img)

	// After vertical flip, the blue pixel should be at (0, 49)
	_, _, b, _ := dst.At(0, 49).RGBA()
	if b < 50000 {
		t.Error("Expected blue pixel at (0, 49) after vertical flip")
	}
}

func TestRotate180(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 50))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	img.Set(0, 0, color.RGBA{0, 255, 0, 255})

	dst := image.NewRGBA(img.Bounds())
	rotate180(dst, img)

	// After 180° rotation, the green pixel should be at (99, 49)
	_, g, _, _ := dst.At(99, 49).RGBA()
	if g < 50000 {
		t.Error("Expected green pixel at (99, 49) after 180° rotation")
	}
}
