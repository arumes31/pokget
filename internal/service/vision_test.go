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
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestDetectCardEdges(t *testing.T) {
	t.Run("InvalidData", func(t *testing.T) {
		_, err := DetectCardEdges([]byte("not an image"))
		if err == nil {
			t.Error("Expected error for invalid image data")
		}
	})

	t.Run("EmptyData", func(t *testing.T) {
		_, err := DetectCardEdges([]byte{})
		if err == nil {
			t.Error("Expected error for empty image data")
		}
	})

	t.Run("ValidPNG", func(t *testing.T) {
		img := image.NewRGBA(image.Rect(0, 0, 100, 100))
		draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 255, 255, 255}}, image.Point{}, draw.Src)

		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatalf("Failed to encode PNG: %v", err)
		}

		bounds, err := DetectCardEdges(buf.Bytes())
		if err != nil {
			t.Errorf("DetectCardEdges failed for valid PNG: %v", err)
		}
		if bounds.Left == 0 && bounds.Right == 0 {
			t.Error("Expected non-zero bounds")
		}
	})

	t.Run("ValidJPEG", func(t *testing.T) {
		img := image.NewRGBA(image.Rect(0, 0, 100, 100))
		draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{200, 200, 200, 255}}, image.Point{}, draw.Src)

		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, nil); err != nil {
			t.Fatalf("Failed to encode JPEG: %v", err)
		}

		bounds, err := DetectCardEdges(buf.Bytes())
		if err != nil {
			t.Errorf("DetectCardEdges failed for valid JPEG: %v", err)
		}
		if bounds.Top == 0 && bounds.Bottom == 0 {
			t.Error("Expected non-zero bounds")
		}
	})

	t.Run("ValidGIF", func(t *testing.T) {
		img := image.NewPaletted(image.Rect(0, 0, 50, 50), color.Palette{color.White, color.Black})
		draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

		var buf bytes.Buffer
		if err := gif.Encode(&buf, img, nil); err != nil {
			t.Fatalf("Failed to encode GIF: %v", err)
		}

		bounds, err := DetectCardEdges(buf.Bytes())
		if err != nil {
			t.Errorf("DetectCardEdges failed for valid GIF: %v", err)
		}
		if bounds.Left == 0 {
			t.Error("Expected non-zero bounds")
		}
	})

	t.Run("SmallImage", func(t *testing.T) {
		img := image.NewRGBA(image.Rect(0, 0, 10, 10))
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		_, err := DetectCardEdges(buf.Bytes())
		if err != nil {
			t.Errorf("DetectCardEdges failed for small image: %v", err)
		}
	})

	t.Run("LargeImageOptimization", func(t *testing.T) {
		img := image.NewRGBA(image.Rect(0, 0, 1000, 1000))
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		bounds, err := DetectCardEdges(buf.Bytes())
		if err != nil {
			t.Errorf("DetectCardEdges failed for large image: %v", err)
		}
		// Based on logic: 15.0 + float64(500%5) = 15.0
		if bounds.Left != 15.0 {
			t.Errorf("Expected optimized bounds.Left to be 15.0 (for 500px), got %f", bounds.Left)
		}
	})
}
