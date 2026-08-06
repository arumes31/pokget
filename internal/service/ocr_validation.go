package service

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // Register GIF decoding with image.Decode.
	_ "image/jpeg" // Register JPEG decoding with image.Decode.
	_ "image/png"  // Register PNG decoding with image.Decode.
	"slices"
	"strings"

	_ "golang.org/x/image/webp" // Register WebP decoding with image.Decode.
)

func decodeOCRImage(imgBytes []byte, config OCRScanConfig) (image.Image, string, error) {
	config = normalizeOCRScanConfig(config)
	if len(imgBytes) == 0 {
		return nil, "", &OCRInputError{Reason: "image is empty"}
	}
	if len(imgBytes) > config.MaxInputBytes {
		return nil, "", &OCRInputError{Reason: fmt.Sprintf("image has %d bytes; limit is %d", len(imgBytes), config.MaxInputBytes)}
	}

	decodedConfig, format, err := image.DecodeConfig(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, "", &OCRInputError{Reason: "cannot decode image header", Err: err}
	}
	format = strings.ToLower(format)
	if !slices.Contains(config.AllowedFormats, format) {
		return nil, "", &OCRInputError{Reason: fmt.Sprintf("format %q is not allowed", format)}
	}
	if decodedConfig.Width <= 0 || decodedConfig.Height <= 0 {
		return nil, "", &OCRInputError{Reason: "image dimensions must be positive"}
	}
	width := int64(decodedConfig.Width)
	height := int64(decodedConfig.Height)
	if width > config.MaxPixels/height {
		return nil, "", &OCRInputError{Reason: fmt.Sprintf("image dimensions %dx%d exceed the %d pixel limit", decodedConfig.Width, decodedConfig.Height, config.MaxPixels)}
	}

	img, decodedFormat, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, "", &OCRInputError{Reason: "cannot decode image pixels", Err: err}
	}
	if decodedFormat != format {
		return nil, "", &OCRInputError{Reason: "image header format changed while decoding"}
	}
	return img, format, nil
}
