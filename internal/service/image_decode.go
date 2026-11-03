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
	_ "image/gif"  // Register GIF decoding with image.Decode.
	_ "image/jpeg" // Register JPEG decoding with image.Decode.
	_ "image/png"  // Register PNG decoding with image.Decode.
	"slices"
	"strings"

	_ "golang.org/x/image/webp" // Register WebP decoding with image.Decode.
)

// ValidateScanImage checks that an upload is a decodable, allowed image without
// decoding the full pixel buffer. Callers use it to reject garbage early with a
// client error instead of letting it reach the detector.
func ValidateScanImage(imgBytes []byte) error {
	config := normalizeImagePrepareConfig(ImagePrepareConfig{})
	if len(imgBytes) == 0 {
		return &ImageInputError{Reason: "image is empty"}
	}
	if len(imgBytes) > config.MaxInputBytes {
		return &ImageInputError{Reason: fmt.Sprintf("image has %d bytes; limit is %d", len(imgBytes), config.MaxInputBytes)}
	}
	decodedConfig, format, err := image.DecodeConfig(bytes.NewReader(imgBytes))
	if err != nil {
		return &ImageInputError{Reason: "cannot decode image header", Err: err}
	}
	if !slices.Contains(config.AllowedFormats, strings.ToLower(format)) {
		return &ImageInputError{Reason: fmt.Sprintf("format %q is not allowed", strings.ToLower(format))}
	}
	if decodedConfig.Width <= 0 || decodedConfig.Height <= 0 {
		return &ImageInputError{Reason: "image dimensions must be positive"}
	}
	if int64(decodedConfig.Width) > config.MaxPixels/int64(decodedConfig.Height) {
		return &ImageInputError{Reason: "image exceeds the pixel limit"}
	}
	return nil
}

func decodeScanImage(imgBytes []byte, config ImagePrepareConfig) (image.Image, string, error) {
	config = normalizeImagePrepareConfig(config)
	if len(imgBytes) == 0 {
		return nil, "", &ImageInputError{Reason: "image is empty"}
	}
	if len(imgBytes) > config.MaxInputBytes {
		return nil, "", &ImageInputError{Reason: fmt.Sprintf("image has %d bytes; limit is %d", len(imgBytes), config.MaxInputBytes)}
	}

	decodedConfig, format, err := image.DecodeConfig(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, "", &ImageInputError{Reason: "cannot decode image header", Err: err}
	}
	format = strings.ToLower(format)
	if !slices.Contains(config.AllowedFormats, format) {
		return nil, "", &ImageInputError{Reason: fmt.Sprintf("format %q is not allowed", format)}
	}
	if decodedConfig.Width <= 0 || decodedConfig.Height <= 0 {
		return nil, "", &ImageInputError{Reason: "image dimensions must be positive"}
	}
	width := int64(decodedConfig.Width)
	height := int64(decodedConfig.Height)
	if width > config.MaxPixels/height {
		return nil, "", &ImageInputError{Reason: fmt.Sprintf("image dimensions %dx%d exceed the %d pixel limit", decodedConfig.Width, decodedConfig.Height, config.MaxPixels)}
	}

	img, decodedFormat, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, "", &ImageInputError{Reason: "cannot decode image pixels", Err: err}
	}
	if decodedFormat != format {
		return nil, "", &ImageInputError{Reason: "image header format changed while decoding"}
	}
	return img, format, nil
}