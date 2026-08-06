package service

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/anthonynsimon/bild/adjust"
	"github.com/anthonynsimon/bild/effect"
	"github.com/anthonynsimon/bild/transform"
)

const (
	maxUpscaleDim = 1500
	minUpscaleDim = 800
)

func computeUpscaleFactors(bounds image.Rectangle) (scaleX, scaleY float64) {
	longestSide := max(bounds.Dx(), bounds.Dy())
	switch {
	case longestSide >= maxUpscaleDim:
		return 1, 1
	case longestSide >= minUpscaleDim:
		return 1.5, 1.5
	default:
		return 2, 2
	}
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

func flipHorizontal(dst *image.RGBA, src image.Image) {
	destinationBounds := dst.Bounds()
	sourceBounds := src.Bounds()
	for y := 0; y < destinationBounds.Dy(); y++ {
		for x := 0; x < destinationBounds.Dx(); x++ {
			dst.Set(destinationBounds.Min.X+x, destinationBounds.Min.Y+y,
				src.At(sourceBounds.Max.X-1-x, sourceBounds.Min.Y+y))
		}
	}
}

func flipVertical(dst *image.RGBA, src image.Image) {
	destinationBounds := dst.Bounds()
	sourceBounds := src.Bounds()
	for y := 0; y < destinationBounds.Dy(); y++ {
		for x := 0; x < destinationBounds.Dx(); x++ {
			dst.Set(destinationBounds.Min.X+x, destinationBounds.Min.Y+y,
				src.At(sourceBounds.Min.X+x, sourceBounds.Max.Y-1-y))
		}
	}
}

func rotate180(dst *image.RGBA, src image.Image) {
	destinationBounds := dst.Bounds()
	sourceBounds := src.Bounds()
	for y := 0; y < destinationBounds.Dy(); y++ {
		for x := 0; x < destinationBounds.Dx(); x++ {
			dst.Set(destinationBounds.Min.X+x, destinationBounds.Min.Y+y,
				src.At(sourceBounds.Max.X-1-x, sourceBounds.Max.Y-1-y))
		}
	}
}

func rotate90CW(src image.Image) *image.RGBA {
	return applyImageOrientation(src, 6).(*image.RGBA)
}

func rotate90CCW(src image.Image) *image.RGBA {
	return applyImageOrientation(src, 8).(*image.RGBA)
}

func cropNormalized(src image.Image, crop OCRNormalizedRect) (image.Image, error) {
	values := []float64{crop.MinX, crop.MinY, crop.MaxX, crop.MaxY}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return nil, &OCRInputError{Reason: "guide crop coordinates must be finite values from 0 to 1"}
		}
	}
	if crop.MinX >= crop.MaxX || crop.MinY >= crop.MaxY {
		return nil, &OCRInputError{Reason: "guide crop must have positive width and height"}
	}

	bounds := src.Bounds()
	rectangle := image.Rect(
		bounds.Min.X+int(math.Floor(crop.MinX*float64(bounds.Dx()))),
		bounds.Min.Y+int(math.Floor(crop.MinY*float64(bounds.Dy()))),
		bounds.Min.X+int(math.Ceil(crop.MaxX*float64(bounds.Dx()))),
		bounds.Min.Y+int(math.Ceil(crop.MaxY*float64(bounds.Dy()))),
	).Intersect(bounds)
	if rectangle.Empty() {
		return nil, &OCRInputError{Reason: "guide crop does not overlap the image"}
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

func preprocessForOCR(src image.Image) image.Image {
	gray := effect.Grayscale(src)
	enhanced := adjust.Contrast(gray, 0.4)
	binarized := otsuBinarize(enhanced)
	return deskewImage(binarized)
}

func otsuBinarize(src image.Image) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	histogram := make([]int, 256)
	totalPixels := bounds.Dx() * bounds.Dy()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			histogram[uint8(pixelLuminance(src.At(x, y)))]++
		}
	}

	sum := 0
	for intensity, count := range histogram {
		sum += intensity * count
	}
	sumBackground, backgroundWeight, threshold := 0, 0, 0
	maxVariance := 0.0
	for candidate, count := range histogram {
		backgroundWeight += count
		if backgroundWeight == 0 {
			continue
		}
		foregroundWeight := totalPixels - backgroundWeight
		if foregroundWeight == 0 {
			break
		}
		sumBackground += candidate * count
		backgroundMean := float64(sumBackground) / float64(backgroundWeight)
		foregroundMean := float64(sum-sumBackground) / float64(foregroundWeight)
		variance := float64(backgroundWeight) * float64(foregroundWeight) * math.Pow(backgroundMean-foregroundMean, 2)
		if variance > maxVariance {
			maxVariance = variance
			threshold = candidate
		}
	}

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if pixelLuminance(src.At(x, y)) > float64(threshold) {
				dst.Set(x, y, color.White)
			} else {
				dst.Set(x, y, color.Black)
			}
		}
	}
	return dst
}

func deskewImage(src image.Image) image.Image {
	bounds := src.Bounds()
	if bounds.Dx() < 50 || bounds.Dy() < 50 {
		return src
	}

	scoreImage := resizeToMaxDimension(src, 600)
	baselineScore := horizontalLineScore(scoreImage)
	bestScore, bestAngle := baselineScore, 0.0
	for angle := -15.0; angle <= 15.0; angle += 3 {
		rotated := flattenOnWhite(transform.Rotate(scoreImage, angle, nil))
		if score := horizontalLineScore(rotated); score > bestScore {
			bestScore, bestAngle = score, angle
		}
	}
	coarseAngle := bestAngle
	for angle := coarseAngle - 2; angle <= coarseAngle+2; angle += 0.5 {
		if angle < -15 || angle > 15 || math.Abs(angle) < 0.1 {
			continue
		}
		rotated := flattenOnWhite(transform.Rotate(scoreImage, angle, nil))
		if score := horizontalLineScore(rotated); score > bestScore {
			bestScore, bestAngle = score, angle
		}
	}
	if math.Abs(bestAngle) < 0.5 || baselineScore > 0 && bestScore < baselineScore*1.03 {
		return src
	}
	return flattenOnWhite(transform.Rotate(src, bestAngle, nil))
}

func horizontalLineScore(src image.Image) float64 {
	bounds := src.Bounds()
	if bounds.Empty() {
		return 0
	}
	projection := make([]float64, bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if pixelLuminance(src.At(x, y)) < 128 {
				projection[y-bounds.Min.Y]++
			}
		}
	}
	mean := 0.0
	for _, value := range projection {
		mean += value
	}
	mean /= float64(len(projection))
	variance, adjacentChange := 0.0, 0.0
	for index, value := range projection {
		variance += math.Pow(value-mean, 2)
		if index > 0 {
			adjacentChange += math.Pow(value-projection[index-1], 2)
		}
	}
	return (variance + adjacentChange*0.5) / (float64(bounds.Dx()*bounds.Dx()) * float64(bounds.Dy()))
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

func prepareOCRSource(imgBytes []byte, src image.Image, config OCRScanConfig) (image.Image, error) {
	src = applyEXIFOrientation(imgBytes, src)
	if config.GuideCrop != nil {
		cropped, err := cropNormalized(src, *config.GuideCrop)
		if err != nil {
			return nil, fmt.Errorf("apply OCR guide crop: %w", err)
		}
		src = cropped
	}
	src = resizeToMaxDimension(src, 1800)
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
