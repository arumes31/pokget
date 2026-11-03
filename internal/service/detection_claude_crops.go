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
	"context"
	"image"
	"math"

	"github.com/anthonynsimon/bild/transform"
)

// A card photo carries the decisive information in two thin strips: the title
// line (name, suffix, HP) and the bottom information line (collector number,
// set code, rarity, copyright). Sending those strips magnified lets the model
// read single kana instead of guessing a familiar PokÃ©mon name from a
// downscaled photo of the whole card.
const (
	// Title band: the top of the frame down to 30% of the card height.
	claudeTitleBandHeight = 0.30
	// Information band: the last 32% of the card height down to the frame end.
	claudeInfoBandHeight = 0.32
	// claudeBandMargin widens the bands horizontally beyond the measured card,
	// because a detected border can sit a few pixels inside the printed line.
	claudeBandMargin = 0.04
	// Fallback bands for photos where the card could not be located. They are
	// deliberately generous so an off-centre card is never cut in half.
	claudeFallbackTitleEnd  = 0.34
	claudeFallbackInfoStart = 0.66
	// claudeBandMinHeight is the smallest strip height that keeps printed
	// glyphs large enough for the vision encoder. Smaller strips are upscaled.
	claudeBandMinHeight = 360
	// claudeSourceMaxDimension keeps more pixels than the smaller preview source:
	// the thin title and information strips are cropped from it, so printed kana
	// stay large enough to distinguish ピ from ビ.
	claudeSourceMaxDimension = 2400
	// Card geometry: a trading card is roughly 0.7 as wide as it is tall.
	claudeCardMinAspect = 0.52
	claudeCardMaxAspect = 0.95
	claudeCardMinCoverage = 0.18
)

// claudeImageVariants returns the prepared full card followed by the magnified
// title and information bands. Images are never binarized: the vision model
// needs the original gradients, foil and anti-aliasing. An unreadable upload is
// rejected here, before any provider sees it.
func claudeImageVariants(ctx context.Context, imageData []byte) ([][]byte, error) {
	config := imagePrepareConfigFromContext(ctx)
	src, _, err := decodeScanImage(imageData, config)
	if err != nil {
		return nil, err
	}
	src, err = prepareScanSource(imageData, src, config, claudeSourceMaxDimension)
	if err != nil {
		return nil, err
	}
	full, err := encodeScanJPEG(src)
	if err != nil {
		return nil, err
	}
	variants := make([][]byte, 0, 3)
	variants = append(variants, full)
	// A landscape frame means the card was captured sideways. The bands assume
	// an upright card, so a rotated photo only gets the full-card image.
	if bounds := src.Bounds(); bounds.Dx() > bounds.Dy() {
		return variants, nil
	}
	for _, band := range claudeBands(src) {
		cropped, err := cropNormalized(src, band)
		if err != nil {
			continue
		}
		encoded, err := encodeScanJPEG(magnifyClaudeBand(cropped))
		if err != nil {
			continue
		}
		variants = append(variants, encoded)
	}
	return variants, nil
}

// claudeBands places the title and information strips relative to the card
// itself. Mobile scans often leave a wide border around the card, so fixed
// percentages of the whole frame would crop the printed name away.
func claudeBands(src image.Image) []GuideRect {
	card := cardBounds(src)
	if card.Empty() {
		return []GuideRect{
			{MinX: 0, MinY: 0, MaxX: 1, MaxY: claudeFallbackTitleEnd},
			{MinX: 0, MinY: claudeFallbackInfoStart, MaxX: 1, MaxY: 1},
		}
	}
	imageBounds := src.Bounds()
	cardWidth, cardHeight := float64(card.Dx()), float64(card.Dy())
	toX := func(x float64) float64 { return clampUnit((x - float64(imageBounds.Min.X)) / float64(imageBounds.Dx())) }
	toY := func(y float64) float64 { return clampUnit((y - float64(imageBounds.Min.Y)) / float64(imageBounds.Dy())) }
	// Reach to the frame edges on the vertical axis: a card box that is a few
	// percent short must never slice the printed name or the collector number.
	x1 := toX(float64(card.Min.X) - cardWidth*claudeBandMargin)
	x2 := toX(float64(card.Max.X) + cardWidth*claudeBandMargin)
	titleEnd := toY(float64(card.Min.Y) + cardHeight*claudeTitleBandHeight)
	infoStart := toY(float64(card.Max.Y) - cardHeight*claudeInfoBandHeight)
	return []GuideRect{
		{MinX: x1, MinY: 0, MaxX: x2, MaxY: math.Max(0.10, titleEnd)},
		{MinX: x1, MinY: math.Min(0.90, infoStart), MaxX: x2, MaxY: 1},
	}
}

// cardBounds locates an upright card inside a photo. It returns an empty
// rectangle when the frame does not contain one plausible card rectangle, so
// callers fall back to generous fixed bands instead of guessing.
func cardBounds(src image.Image) image.Rectangle {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 120 || height < 120 {
		return image.Rectangle{}
	}
	columnEnergy := make([]float64, width)
	rowEnergy := make([]float64, height)
	step := max(1, min(width, height)/400)
	for y := step; y < height; y += step {
		for x := step; x < width; x += step {
			current := pixelLuminance(src.At(bounds.Min.X+x, bounds.Min.Y+y))
			columnEnergy[x] += math.Abs(current - pixelLuminance(src.At(bounds.Min.X+x-step, bounds.Min.Y+y)))
			rowEnergy[y] += math.Abs(current - pixelLuminance(src.At(bounds.Min.X+x, bounds.Min.Y+y-step)))
		}
	}
	left, right, columnsOK := strongestOuterEdges(columnEnergy)
	top, bottom, rowsOK := strongestOuterEdges(rowEnergy)
	if !columnsOK || !rowsOK {
		return image.Rectangle{}
	}
	boxWidth, boxHeight := right-left, bottom-top
	if boxWidth < step*4 || boxHeight < step*4 {
		return image.Rectangle{}
	}
	coverage := float64(boxWidth*boxHeight) / float64(width*height)
	aspect := float64(boxWidth) / float64(boxHeight)
	if coverage < claudeCardMinCoverage {
		return image.Rectangle{}
	}
	if aspect < claudeCardMinAspect || aspect > claudeCardMaxAspect {
		return image.Rectangle{}
	}
	return image.Rect(bounds.Min.X+left, bounds.Min.Y+top, bounds.Min.X+right, bounds.Min.Y+bottom)
}

func clampUnit(value float64) float64 {
	return math.Min(0.999, math.Max(0.001, value))
}

// magnifyClaudeBand resamples a short strip up to a readable height. Wider
// strips keep their original resolution so no detail is invented.
func magnifyClaudeBand(band image.Image) image.Image {
	bounds := band.Bounds()
	if bounds.Dy() >= claudeBandMinHeight || bounds.Dy() <= 0 || bounds.Dx() <= 0 {
		return band
	}
	scale := math.Min(3, float64(claudeBandMinHeight)/float64(bounds.Dy()))
	if scale <= 1 {
		return band
	}
	return transform.Resize(band,
		max(1, int(math.Round(float64(bounds.Dx())*scale))),
		max(1, int(math.Round(float64(bounds.Dy())*scale))),
		transform.Lanczos,
	)
}
