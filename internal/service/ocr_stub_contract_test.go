//go:build !cgo || (!linux && !darwin && !freebsd)

package service

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"testing"
)

func TestOCRStubReturnsTypedUnavailableAndProcessedImage(t *testing.T) {
	clearOCRCache()
	var input bytes.Buffer
	if err := png.Encode(&input, image.NewRGBA(image.Rect(0, 0, 32, 48))); err != nil {
		t.Fatal(err)
	}
	text, card, firstImage, err := ProcessCardScan(input.Bytes(), nil, "eng", nil)
	var unavailable *OCRUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("error = %T %v, want *OCRUnavailableError", err, err)
	}
	if text == "" || card != "Unknown Card" || len(firstImage) == 0 {
		t.Fatalf("stub result = (%q, %q, %d bytes)", text, card, len(firstImage))
	}

	_, _, cachedImage, err := ProcessCardScan(input.Bytes(), nil, "eng", nil)
	if !errors.As(err, &unavailable) {
		t.Fatalf("cached error = %T %v, want *OCRUnavailableError", err, err)
	}
	if !bytes.Equal(firstImage, cachedImage) {
		t.Fatal("cache hit did not return the processed image")
	}
}
