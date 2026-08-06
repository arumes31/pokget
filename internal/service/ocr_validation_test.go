package service

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestDecodeOCRImageValidation(t *testing.T) {
	imageData := func(encode func(*bytes.Buffer) error) []byte {
		var buffer bytes.Buffer
		if err := encode(&buffer); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}
	img := image.NewRGBA(image.Rect(0, 0, 20, 10))
	img.Set(1, 1, color.Black)
	pngData := imageData(func(buffer *bytes.Buffer) error { return png.Encode(buffer, img) })
	jpegData := imageData(func(buffer *bytes.Buffer) error { return jpeg.Encode(buffer, img, nil) })
	gifData := imageData(func(buffer *bytes.Buffer) error { return gif.Encode(buffer, img, nil) })

	tests := []struct {
		name   string
		data   []byte
		config OCRScanConfig
		format string
		ok     bool
	}{
		{name: "PNG", data: pngData, format: "png", ok: true},
		{name: "JPEG", data: jpegData, format: "jpeg", ok: true},
		{name: "empty", config: OCRScanConfig{}},
		{name: "byte limit", data: pngData, config: OCRScanConfig{MaxInputBytes: len(pngData) - 1}},
		{name: "pixel limit", data: pngData, config: OCRScanConfig{MaxPixels: 199}},
		{name: "GIF rejected by default", data: gifData},
		{name: "GIF explicitly allowed", data: gifData, config: OCRScanConfig{AllowedFormats: []string{"gif"}}, format: "gif", ok: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decoded, format, err := decodeOCRImage(test.data, test.config)
			if test.ok {
				if err != nil || decoded == nil || format != test.format {
					t.Fatalf("decodeOCRImage() = (%v, %q, %v)", decoded, format, err)
				}
				return
			}
			var inputErr *OCRInputError
			if !errors.As(err, &inputErr) {
				t.Fatalf("error = %T %v, want *OCRInputError", err, err)
			}
		})
	}
}

func FuzzDecodeOCRImage(f *testing.F) {
	var seed bytes.Buffer
	_ = png.Encode(&seed, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	f.Add(seed.Bytes())
	f.Add([]byte("not an image"))
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _, _ = decodeOCRImage(data, OCRScanConfig{MaxInputBytes: 1 << 20, MaxPixels: 1_000_000})
	})
}
