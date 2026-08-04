package service

import (
	"encoding/binary"
	"image"
	"image/color"
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
	longestSide := bounds.Dx()
	if bounds.Dy() > longestSide {
		longestSide = bounds.Dy()
	}

	switch {
	case longestSide > maxUpscaleDim:
		return 1.0, 1.0
	case longestSide >= minUpscaleDim:
		return 1.5, 1.5
	default:
		return 2.0, 2.0
	}
}

func applyEXIFOrientation(imgBytes []byte, img image.Image) image.Image {
	orientation := getEXIFOrientation(imgBytes)
	if orientation == 1 || orientation == 0 {
		return img
	}

	bounds := img.Bounds()
	dst := image.NewRGBA(bounds)

	switch orientation {
	case 2:
		flipHorizontal(dst, img)
	case 3:
		rotate180(dst, img)
	case 4:
		flipVertical(dst, img)
	case 5:
		return rotate90CW(img)
	case 6:
		return rotate90CW(img)
	case 7:
		flipped := rotate90CCW(img)
		dst2 := image.NewRGBA(flipped.Bounds())
		flipHorizontal(dst2, flipped)
		return dst2
	case 8:
		return rotate90CCW(img)
	}

	return dst
}

func getEXIFOrientation(imgBytes []byte) int {
	if len(imgBytes) < 4 || imgBytes[0] != 0xFF || imgBytes[1] != 0xD8 {
		return 0
	}

	i := 2
	for i < len(imgBytes)-1 {
		if imgBytes[i] != 0xFF {
			break
		}
		marker := imgBytes[i+1]
		if marker == 0xE1 {
			return parseEXIFOrientation(imgBytes, i+2)
		}
		if marker == 0xD9 {
			break
		}
		if marker == 0x00 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		if i+3 < len(imgBytes) {
			segLen := int(binary.BigEndian.Uint16(imgBytes[i+2 : i+4]))
			i += 2 + segLen
		} else {
			break
		}
	}
	return 0
}

func parseEXIFOrientation(data []byte, offset int) int {
	if offset+4 > len(data) {
		return 0
	}

	if offset+6 <= len(data) && string(data[offset:offset+6]) == "Exif\x00\x00" {
		offset += 6
	}

	if offset+8 > len(data) {
		return 0
	}

	var byteOrder binary.ByteOrder
	if data[offset] == 0x49 && data[offset+1] == 0x49 {
		byteOrder = binary.LittleEndian
	} else if data[offset] == 0x4D && data[offset+1] == 0x4D {
		byteOrder = binary.BigEndian
	} else {
		return 0
	}

	if offset+6 > len(data) {
		return 0
	}
	magic := byteOrder.Uint16(data[offset+2 : offset+4])
	if magic != 42 {
		return 0
	}

	if offset+8 > len(data) {
		return 0
	}
	ifdOffset := int(byteOrder.Uint32(data[offset+4 : offset+8]))
	if ifdOffset+offset+2 > len(data) {
		return 0
	}
	numEntries := int(byteOrder.Uint16(data[offset+ifdOffset : offset+ifdOffset+2]))

	for j := 0; j < numEntries; j++ {
		entryOff := offset + ifdOffset + 2 + j*12
		if entryOff+12 > len(data) {
			break
		}
		tag := byteOrder.Uint16(data[entryOff : entryOff+2])
		if tag == 0x0112 {
			orientation := byteOrder.Uint16(data[entryOff+8 : entryOff+10])
			return int(orientation)
		}
	}

	return 0
}

func flipHorizontal(dst *image.RGBA, src image.Image) {
	b := dst.Bounds()
	sb := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			sx := sb.Max.X - 1 - (x - b.Min.X)
			dst.Set(x, y, src.At(sx, y))
		}
	}
}

func flipVertical(dst *image.RGBA, src image.Image) {
	b := dst.Bounds()
	sb := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			sy := sb.Max.Y - 1 - (y - b.Min.Y)
			dst.Set(x, y, src.At(x, sy))
		}
	}
}

func rotate180(dst *image.RGBA, src image.Image) {
	b := dst.Bounds()
	sb := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			sx := sb.Max.X - 1 - (x - b.Min.X)
			sy := sb.Max.Y - 1 - (y - b.Min.Y)
			dst.Set(x, y, src.At(sx, sy))
		}
	}
}

func rotate90CW(src image.Image) *image.RGBA {
	sb := src.Bounds()
	newW := sb.Dy()
	newH := sb.Dx()
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			sx := y + sb.Min.Y
			sy := sb.Max.Y - 1 - x
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func rotate90CCW(src image.Image) *image.RGBA {
	sb := src.Bounds()
	newW := sb.Dy()
	newH := sb.Dx()
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			sx := sb.Max.X - 1 - y
			sy := x + sb.Min.X
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
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
			r, _, _, _ := src.At(x, y).RGBA()
			histogram[uint8(r>>8)]++
		}
	}

	sum := 0
	for i := 0; i < 256; i++ {
		sum += i * histogram[i]
	}

	sumB := 0
	wB := 0
	maxVariance := 0.0
	threshold := 0
	for t := 0; t < 256; t++ {
		wB += histogram[t]
		if wB == 0 {
			continue
		}
		wF := totalPixels - wB
		if wF == 0 {
			break
		}
		sumB += t * histogram[t]
		mB := float64(sumB) / float64(wB)
		mF := float64(sum-sumB) / float64(wF)
		variance := float64(wB) * float64(wF) * (mB - mF) * (mB - mF)
		if variance > maxVariance {
			maxVariance = variance
			threshold = t
		}
	}

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, _, _, _ := src.At(x, y).RGBA()
			var c color.Color
			if uint8(r>>8) > uint8(threshold) {
				c = color.White
			} else {
				c = color.Black
			}
			dst.Set(x, y, c)
		}
	}

	return dst
}

func deskewImage(src image.Image) image.Image {
	bestAngle := 0.0
	bestScore := -1.0
	bounds := src.Bounds()
	if bounds.Dx() < 50 || bounds.Dy() < 50 {
		return src
	}

	for angle := -15.0; angle <= 15.0; angle += 3.0 {
		rotated := transform.Rotate(src, angle, nil)
		score := horizontalLineScore(rotated)
		if score > bestScore {
			bestScore = score
			bestAngle = angle
		}
	}

	if math.Abs(bestAngle) < 0.5 {
		return src
	}
	return transform.Rotate(src, bestAngle, nil)
}

func horizontalLineScore(src image.Image) float64 {
	bounds := src.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return 0
	}

	rowTransitions := make([]int, 0, bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		transitions := 0
		prevBlack := false
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, _, _, _ := src.At(x, y).RGBA()
			isBlack := r < 32768
			if isBlack != prevBlack {
				transitions++
			}
			prevBlack = isBlack
		}
		rowTransitions = append(rowTransitions, transitions)
	}

	totalScore := 0
	for _, transitions := range rowTransitions {
		totalScore += transitions
	}
	return float64(totalScore) / float64(bounds.Dy())
}
