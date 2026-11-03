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
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"strings"

	"github.com/anthonynsimon/bild/transform"
)

const (
	defaultScanImageMaxBytes  = 20 << 20
	defaultScanImageMaxPixels = int64(40_000_000)
)

// GuideRect describes a crop using coordinates in the inclusive 0..1 range. It
// is resolution independent, so the camera guide passes its rectangle through
// unchanged.
type GuideRect struct {
	MinX float64
	MinY float64
	MaxX float64
	MaxY float64
}

// ImagePrepareConfig bounds and normalizes an uploaded photo before the
// detector sees it. Zero values select the safe defaults.
type ImagePrepareConfig struct {
	GuideCrop      *GuideRect
	MaxInputBytes  int
	MaxPixels      int64
	AllowedFormats []string
}

type imagePrepareConfigContextKey struct{}

// WithImagePrepareConfig returns a context carrying scan-image options.
func WithImagePrepareConfig(ctx context.Context, config ImagePrepareConfig) context.Context {
	return context.WithValue(ctx, imagePrepareConfigContextKey{}, config)
}

func imagePrepareConfigFromContext(ctx context.Context) ImagePrepareConfig {
	if ctx != nil {
		if config, ok := ctx.Value(imagePrepareConfigContextKey{}).(ImagePrepareConfig); ok {
			return normalizeImagePrepareConfig(config)
		}
	}
	return normalizeImagePrepareConfig(ImagePrepareConfig{})
}

func normalizeImagePrepareConfig(config ImagePrepareConfig) ImagePrepareConfig {
	if config.MaxInputBytes <= 0 {
		config.MaxInputBytes = defaultScanImageMaxBytes
	}
	if config.MaxPixels <= 0 {
		config.MaxPixels = defaultScanImageMaxPixels
	}
	if len(config.AllowedFormats) == 0 {
		config.AllowedFormats = []string{"jpeg", "png", "webp"}
		return config
	}
	formats := make([]string, 0, len(config.AllowedFormats))
	seen := make(map[string]struct{}, len(config.AllowedFormats))
	for _, format := range config.AllowedFormats {
		format = strings.ToLower(strings.TrimSpace(format))
		if format == "jpg" {
			format = "jpeg"
		}
		if format == "" {
			continue
		}
		if _, ok := seen[format]; ok {
			continue
		}
		seen[format] = struct{}{}
		formats = append(formats, format)
	}
	config.AllowedFormats = formats
	return config
}

// ImageInputError reports that an upload was rejected before any detector saw
// it.
type ImageInputError struct {
	Reason string
	Err    error
}

func (e *ImageInputError) Error() string {
	if e == nil {
		return "invalid scan image"
	}
	if e.Err != nil {
		return fmt.Sprintf("invalid scan image: %s: %v", e.Reason, e.Err)
	}
	return "invalid scan image: " + e.Reason
}

func (e *ImageInputError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func applyEXIFOrientation(imgBytes []byte, img image.Image) image.Image {
	orientation := getEXIFOrientation(imgBytes)
	if orientation < 2 || orientation > 8 {
		return img
	}
	return applyImageOrientation(img, orientation)
}

func applyImageOrientation(src image.Image, orientation int) image.Image {
	sourceBounds := src.Bounds()
	width, height := sourceBounds.Dx(), sourceBounds.Dy()
	if orientation < 2 || orientation > 8 || width == 0 || height == 0 {
		return src
	}

	outputWidth, outputHeight := width, height
	if orientation >= 5 {
		outputWidth, outputHeight = height, width
	}
	dst := image.NewRGBA(image.Rect(0, 0, outputWidth, outputHeight))
	for y := 0; y < outputHeight; y++ {
		for x := 0; x < outputWidth; x++ {
			var sourceX, sourceY int
			switch orientation {
			case 2:
				sourceX, sourceY = width-1-x, y
			case 3:
				sourceX, sourceY = width-1-x, height-1-y
			case 4:
				sourceX, sourceY = x, height-1-y
			case 5:
				sourceX, sourceY = y, x
			case 6:
				sourceX, sourceY = y, height-1-x
			case 7:
				sourceX, sourceY = width-1-y, height-1-x
			case 8:
				sourceX, sourceY = width-1-y, x
			}
			dst.Set(x, y, src.At(sourceBounds.Min.X+sourceX, sourceBounds.Min.Y+sourceY))
		}
	}
	return dst
}

func getEXIFOrientation(imgBytes []byte) int {
	if len(imgBytes) < 4 || imgBytes[0] != 0xff || imgBytes[1] != 0xd8 {
		return 0
	}

	position := 2
	for position < len(imgBytes) {
		for position < len(imgBytes) && imgBytes[position] == 0xff {
			position++
		}
		if position >= len(imgBytes) {
			break
		}
		marker := imgBytes[position]
		position++
		if marker == 0xd9 || marker == 0xda {
			break
		}
		if marker == 0x00 || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if position+2 > len(imgBytes) {
			break
		}
		segmentLength := int(binary.BigEndian.Uint16(imgBytes[position : position+2]))
		if segmentLength < 2 || segmentLength > len(imgBytes)-position {
			break
		}
		payloadOffset := position + 2
		segmentEnd := position + segmentLength
		if marker == 0xe1 && payloadOffset+6 <= segmentEnd && string(imgBytes[payloadOffset:payloadOffset+6]) == "Exif\x00\x00" {
			if orientation := parseEXIFOrientation(imgBytes[:segmentEnd], payloadOffset); orientation != 0 {
				return orientation
			}
		}
		position = segmentEnd
	}
	return 0
}

func parseEXIFOrientation(data []byte, offset int) int {
	if offset < 0 || offset > len(data)-6 || string(data[offset:offset+6]) != "Exif\x00\x00" {
		return 0
	}
	tiffOffset := offset + 6
	if tiffOffset > len(data)-8 {
		return 0
	}

	var byteOrder binary.ByteOrder
	switch string(data[tiffOffset : tiffOffset+2]) {
	case "II":
		byteOrder = binary.LittleEndian
	case "MM":
		byteOrder = binary.BigEndian
	default:
		return 0
	}
	if byteOrder.Uint16(data[tiffOffset+2:tiffOffset+4]) != 42 {
		return 0
	}

	ifdRelative := uint64(byteOrder.Uint32(data[tiffOffset+4 : tiffOffset+8]))
	availableBytes := len(data) - tiffOffset - 2
	availableBytes64 := uint64(availableBytes) // #nosec G115 -- availableBytes is non-negative after the bounds check above.
	if ifdRelative > availableBytes64 {
		return 0
	}
	ifdOffset := tiffOffset + int(ifdRelative) // #nosec G115 -- ifdRelative is bounded by availableBytes, an int.
	entryCount := int(byteOrder.Uint16(data[ifdOffset : ifdOffset+2]))
	entriesOffset := ifdOffset + 2
	if entryCount > (len(data)-entriesOffset)/12 {
		return 0
	}

	for index := 0; index < entryCount; index++ {
		entryOffset := entriesOffset + index*12
		if byteOrder.Uint16(data[entryOffset:entryOffset+2]) != 0x0112 {
			continue
		}
		fieldType := byteOrder.Uint16(data[entryOffset+2 : entryOffset+4])
		count := byteOrder.Uint32(data[entryOffset+4 : entryOffset+8])
		if fieldType != 3 || count != 1 {
			return 0
		}
		orientation := int(byteOrder.Uint16(data[entryOffset+8 : entryOffset+10]))
		if orientation >= 1 && orientation <= 8 {
			return orientation
		}
		return 0
	}
	return 0
}

func cropNormalized(src image.Image, crop GuideRect) (image.Image, error) {
	values := []float64{crop.MinX, crop.MinY, crop.MaxX, crop.MaxY}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return nil, &ImageInputError{Reason: "guide crop coordinates must be finite values from 0 to 1"}
		}
	}
	if crop.MinX >= crop.MaxX || crop.MinY >= crop.MaxY {
		return nil, &ImageInputError{Reason: "guide crop must have positive width and height"}
	}

	bounds := src.Bounds()
	rectangle := image.Rect(
		bounds.Min.X+int(math.Floor(crop.MinX*float64(bounds.Dx()))),
		bounds.Min.Y+int(math.Floor(crop.MinY*float64(bounds.Dy()))),
		bounds.Min.X+int(math.Ceil(crop.MaxX*float64(bounds.Dx()))),
		bounds.Min.Y+int(math.Ceil(crop.MaxY*float64(bounds.Dy()))),
	).Intersect(bounds)
	if rectangle.Empty() {
		return nil, &ImageInputError{Reason: "guide crop does not overlap the image"}
	}
	return copyImageRegion(src, rectangle), nil
}

func copyImageRegion(src image.Image, rectangle image.Rectangle) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, rectangle.Dx(), rectangle.Dy()))
	draw.Draw(dst, dst.Bounds(), src, rectangle.Min, draw.Src)
	return dst
}

// cropToContentEdges conservatively trims an axis-aligned outer background.
// It deliberately retains at least 55% of each dimension; ambiguous images
// remain unchanged.
func cropToContentEdges(src image.Image) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 100 || height < 100 {
		return src
	}

	columnEnergy := make([]float64, width)
	rowEnergy := make([]float64, height)
	for y := 1; y < height; y++ {
		for x := 1; x < width; x++ {
			current := pixelLuminance(src.At(bounds.Min.X+x, bounds.Min.Y+y))
			horizontal := math.Abs(current - pixelLuminance(src.At(bounds.Min.X+x-1, bounds.Min.Y+y)))
			vertical := math.Abs(current - pixelLuminance(src.At(bounds.Min.X+x, bounds.Min.Y+y-1)))
			columnEnergy[x] += horizontal
			rowEnergy[y] += vertical
		}
	}

	left, right, columnsOK := strongestOuterEdges(columnEnergy)
	top, bottom, rowsOK := strongestOuterEdges(rowEnergy)
	if !columnsOK || !rowsOK {
		return src
	}
	paddingX := max(2, width/100)
	paddingY := max(2, height/100)
	left = max(0, left-paddingX)
	right = min(width, right+paddingX)
	top = max(0, top-paddingY)
	bottom = min(height, bottom+paddingY)
	if right-left < width*55/100 || bottom-top < height*55/100 {
		return src
	}
	if left < width/50 && right > width-width/50 && top < height/50 && bottom > height-height/50 {
		return src
	}
	return copyImageRegion(src, image.Rect(bounds.Min.X+left, bounds.Min.Y+top, bounds.Min.X+right, bounds.Min.Y+bottom))
}

func strongestOuterEdges(energy []float64) (int, int, bool) {
	if len(energy) < 10 {
		return 0, len(energy), false
	}
	maximum := 0.0
	for _, value := range energy {
		maximum = max(maximum, value)
	}
	if maximum == 0 {
		return 0, len(energy), false
	}
	threshold := maximum * 0.22
	first, last := -1, -1
	for index, value := range energy {
		if value < threshold {
			continue
		}
		if first == -1 {
			first = index
		}
		last = index
	}
	if first == -1 || last <= first {
		return 0, len(energy), false
	}
	return first, last + 1, true
}

func pixelLuminance(value color.Color) float64 {
	r, g, b, a := value.RGBA()
	if a == 0 {
		return 255
	}
	alpha := float64(a) / 65535
	luminance := (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 257
	return luminance*alpha + 255*(1-alpha)
}

func flattenOnWhite(src image.Image) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Over)
	return dst
}

// prepareScanSource applies the shared orientation, guide-crop and content-trim
// pipeline, then bounds the long side to maxDimension.
func prepareScanSource(imgBytes []byte, src image.Image, config ImagePrepareConfig, maxDimension int) (image.Image, error) {
	src = applyEXIFOrientation(imgBytes, src)
	if config.GuideCrop != nil {
		cropped, err := cropNormalized(src, *config.GuideCrop)
		if err != nil {
			return nil, fmt.Errorf("apply guide crop: %w", err)
		}
		src = cropped
	}
	src = resizeToMaxDimension(src, maxDimension)
	return cropToContentEdges(src), nil
}

func resizeToMaxDimension(src image.Image, maximum int) image.Image {
	bounds := src.Bounds()
	longest := max(bounds.Dx(), bounds.Dy())
	if longest <= maximum || longest == 0 {
		return src
	}
	scale := float64(maximum) / float64(longest)
	return transform.Resize(src,
		max(1, int(math.Round(float64(bounds.Dx())*scale))),
		max(1, int(math.Round(float64(bounds.Dy())*scale))),
		transform.Lanczos,
	)
}

// encodeScanJPEG writes an opaque JPEG. The vision encoder needs the original
// gradients, so nothing is binarized here.
func encodeScanJPEG(src image.Image) ([]byte, error) {
	buffer := new(bytes.Buffer)
	if err := jpeg.Encode(buffer, flattenOnWhite(src), &jpeg.Options{Quality: 95}); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}