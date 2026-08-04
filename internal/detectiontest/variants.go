package detectiontest

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
)

const (
	blurRadius       = 2
	resizeScale      = 0.70
	rotationDegrees  = 3.0
	brightnessFactor = 0.72
	jpegQuality      = 65
)

// RenderedVariant contains one deterministic transformation of a source image.
type RenderedVariant struct {
	Name   string
	Ext    string
	MIME   string
	Bytes  []byte
	Width  int
	Height int
}

// RenderVariants builds the clean and five degraded fixture variants.
func RenderVariants(source image.Image) ([]RenderedVariant, error) {
	if source == nil {
		return nil, errors.New("rendering fixture variants: nil source image")
	}
	if source.Bounds().Dx() < 1 || source.Bounds().Dy() < 1 {
		return nil, errors.New("rendering fixture variants: source image is empty")
	}

	clean := toNRGBA(source)
	images := []struct {
		name  string
		image image.Image
	}{
		{name: "clean", image: clean},
		{name: "blur", image: boxBlur(clean, blurRadius)},
		{name: "resize", image: resizeNearest(clean, resizeScale)},
		{name: "rotate", image: rotateNearest(clean, rotationDegrees)},
		{name: "brightness", image: adjustBrightness(clean, brightnessFactor)},
	}

	variants := make([]RenderedVariant, 0, len(images)+1)
	for _, item := range images {
		payload, err := encodePNG(item.image)
		if err != nil {
			return nil, fmt.Errorf("rendering %s fixture variant: %w", item.name, err)
		}
		variants = append(variants, RenderedVariant{
			Name:   item.name,
			Ext:    "png",
			MIME:   "image/png",
			Bytes:  payload,
			Width:  item.image.Bounds().Dx(),
			Height: item.image.Bounds().Dy(),
		})
	}

	jpegPayload, err := encodeJPEG(clean, jpegQuality)
	if err != nil {
		return nil, fmt.Errorf("rendering jpeg fixture variant: %w", err)
	}
	variants = append(variants, RenderedVariant{
		Name:   "jpeg",
		Ext:    "jpg",
		MIME:   "image/jpeg",
		Bytes:  jpegPayload,
		Width:  clean.Bounds().Dx(),
		Height: clean.Bounds().Dy(),
	})
	return variants, nil
}

func toNRGBA(source image.Image) *image.NRGBA {
	bounds := source.Bounds()
	destination := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(destination, destination.Bounds(), source, bounds.Min, draw.Src)
	return destination
}

func boxBlur(source *image.NRGBA, radius int) *image.NRGBA {
	bounds := source.Bounds()
	destination := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			var red, green, blue, alpha, samples uint32
			for offsetY := -radius; offsetY <= radius; offsetY++ {
				sampleY := clamp(y+offsetY, bounds.Min.Y, bounds.Max.Y-1)
				for offsetX := -radius; offsetX <= radius; offsetX++ {
					sampleX := clamp(x+offsetX, bounds.Min.X, bounds.Max.X-1)
					pixel := source.NRGBAAt(sampleX, sampleY)
					red += uint32(pixel.R)
					green += uint32(pixel.G)
					blue += uint32(pixel.B)
					alpha += uint32(pixel.A)
					samples++
				}
			}
			destination.SetNRGBA(x, y, color.NRGBA{
				R: uint8(red / samples),
				G: uint8(green / samples),
				B: uint8(blue / samples),
				A: uint8(alpha / samples),
			})
		}
	}
	return destination
}

func resizeNearest(source *image.NRGBA, scale float64) *image.NRGBA {
	sourceBounds := source.Bounds()
	width := max(1, int(math.Round(float64(sourceBounds.Dx())*scale)))
	height := max(1, int(math.Round(float64(sourceBounds.Dy())*scale)))
	destination := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		sourceY := min(sourceBounds.Dy()-1, y*sourceBounds.Dy()/height)
		for x := range width {
			sourceX := min(sourceBounds.Dx()-1, x*sourceBounds.Dx()/width)
			destination.SetNRGBA(x, y, source.NRGBAAt(sourceX, sourceY))
		}
	}
	return destination
}

func rotateNearest(source *image.NRGBA, degrees float64) *image.NRGBA {
	bounds := source.Bounds()
	destination := image.NewNRGBA(bounds)
	draw.Draw(destination, bounds, &image.Uniform{C: color.White}, image.Point{}, draw.Src)

	radians := degrees * math.Pi / 180
	sine, cosine := math.Sincos(radians)
	centerX := float64(bounds.Dx()-1) / 2
	centerY := float64(bounds.Dy()-1) / 2
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			destinationX := float64(x-bounds.Min.X) - centerX
			destinationY := float64(y-bounds.Min.Y) - centerY
			sourceX := cosine*destinationX + sine*destinationY + centerX
			sourceY := -sine*destinationX + cosine*destinationY + centerY
			nearestX := int(math.Round(sourceX)) + bounds.Min.X
			nearestY := int(math.Round(sourceY)) + bounds.Min.Y
			if nearestX < bounds.Min.X || nearestX >= bounds.Max.X {
				continue
			}
			if nearestY < bounds.Min.Y || nearestY >= bounds.Max.Y {
				continue
			}
			destination.SetNRGBA(x, y, source.NRGBAAt(nearestX, nearestY))
		}
	}
	return destination
}

func adjustBrightness(source *image.NRGBA, factor float64) *image.NRGBA {
	bounds := source.Bounds()
	destination := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := source.NRGBAAt(x, y)
			destination.SetNRGBA(x, y, color.NRGBA{
				R: scaleChannel(pixel.R, factor),
				G: scaleChannel(pixel.G, factor),
				B: scaleChannel(pixel.B, factor),
				A: pixel.A,
			})
		}
	}
	return destination
}

func scaleChannel(channel uint8, factor float64) uint8 {
	value := math.Round(float64(channel) * factor)
	return uint8(clamp(int(value), 0, 255))
}

func clamp(value, minimum, maximum int) int {
	return min(max(value, minimum), maximum)
}

func encodePNG(source image.Image) ([]byte, error) {
	var output bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&output, source); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func encodeJPEG(source image.Image, quality int) ([]byte, error) {
	var output bytes.Buffer
	if err := jpeg.Encode(&output, source, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
