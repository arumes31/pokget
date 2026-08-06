package service

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func TestApplyImageOrientationGolden(t *testing.T) {
	source := image.NewRGBA(image.Rect(10, 20, 12, 23))
	values := [][]uint8{{1, 2}, {3, 4}, {5, 6}}
	for y, row := range values {
		for x, value := range row {
			source.Set(10+x, 20+y, color.RGBA{R: value, A: 255})
		}
	}
	want := map[int][][]uint8{
		1: {{1, 2}, {3, 4}, {5, 6}},
		2: {{2, 1}, {4, 3}, {6, 5}},
		3: {{6, 5}, {4, 3}, {2, 1}},
		4: {{5, 6}, {3, 4}, {1, 2}},
		5: {{1, 3, 5}, {2, 4, 6}},
		6: {{5, 3, 1}, {6, 4, 2}},
		7: {{6, 4, 2}, {5, 3, 1}},
		8: {{2, 4, 6}, {1, 3, 5}},
	}

	for orientation := 1; orientation <= 8; orientation++ {
		t.Run(string(rune('0'+orientation)), func(t *testing.T) {
			result := applyImageOrientation(source, orientation)
			bounds := result.Bounds()
			if bounds.Dy() != len(want[orientation]) || bounds.Dx() != len(want[orientation][0]) {
				t.Fatalf("orientation %d bounds = %v", orientation, bounds)
			}
			for y, row := range want[orientation] {
				for x, expected := range row {
					red, _, _, _ := result.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
					if red>>8 != uint32(expected) {
						t.Errorf("orientation %d pixel (%d,%d) = %d, want %d", orientation, x, y, red>>8, expected)
					}
				}
			}
		})
	}
}

func TestGetEXIFOrientationAllValues(t *testing.T) {
	for orientation := 1; orientation <= 8; orientation++ {
		t.Run(string(rune('0'+orientation)), func(t *testing.T) {
			if got := getEXIFOrientation(jpegWithOrientation(orientation)); got != orientation {
				t.Fatalf("getEXIFOrientation() = %d, want %d", got, orientation)
			}
		})
	}
}

func jpegWithOrientation(orientation int) []byte {
	if orientation < 1 || orientation > 8 {
		panic("EXIF orientation must be between 1 and 8")
	}
	tiff := make([]byte, 8+2+12+4)
	copy(tiff, "II")
	binary.LittleEndian.PutUint16(tiff[2:4], 42)
	binary.LittleEndian.PutUint32(tiff[4:8], 8)
	binary.LittleEndian.PutUint16(tiff[8:10], 1)
	binary.LittleEndian.PutUint16(tiff[10:12], 0x0112)
	binary.LittleEndian.PutUint16(tiff[12:14], 3)
	binary.LittleEndian.PutUint32(tiff[14:18], 1)
	binary.LittleEndian.PutUint16(tiff[18:20], uint16(orientation)) // #nosec G115 -- orientation is bounded above.
	payload := append([]byte("Exif\x00\x00"), tiff...)
	result := []byte{0xff, 0xd8, 0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(result[4:6], jpegSegmentLength(payload))
	result = append(result, payload...)
	return append(result, 0xff, 0xd9)
}

func TestGetEXIFOrientationSkipsNonEXIFAPP1(t *testing.T) {
	firstPayload := []byte("not-exif")
	first := []byte{0xff, 0xd8, 0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(first[4:6], jpegSegmentLength(firstPayload))
	first = append(first, firstPayload...)
	second := jpegWithOrientation(6)[2:]
	combined := append(first, second...)
	if got := getEXIFOrientation(combined); got != 6 {
		t.Fatalf("getEXIFOrientation() = %d, want 6", got)
	}
}

func jpegSegmentLength(payload []byte) uint16 {
	length := len(payload) + 2
	if length > 65535 {
		panic("JPEG segment payload exceeds uint16 length")
	}
	return uint16(length) // #nosec G115 -- length is explicitly bounded above.
}

func TestCropNormalized(t *testing.T) {
	img := image.NewRGBA(image.Rect(10, 20, 110, 220))
	cropped, err := cropNormalized(img, OCRNormalizedRect{MinX: 0.25, MinY: 0.25, MaxX: 0.75, MaxY: 0.75})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cropped.Bounds(), image.Rect(0, 0, 50, 100); got != want {
		t.Fatalf("crop bounds = %v, want %v", got, want)
	}

	_, err = cropNormalized(img, OCRNormalizedRect{MinX: 0.8, MinY: 0.2, MaxX: 0.2, MaxY: 0.9})
	if err == nil {
		t.Fatal("invalid guide crop was accepted")
	}
}

func TestCropToContentEdgesConservative(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 300))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(20, 30, 180, 270), &image.Uniform{C: color.RGBA{R: 30, G: 40, B: 50, A: 255}}, image.Point{}, draw.Src)
	cropped := cropToContentEdges(img)
	if cropped.Bounds().Dx() >= img.Bounds().Dx() || cropped.Bounds().Dy() >= img.Bounds().Dy() {
		t.Fatalf("content crop did not trim image: got %v", cropped.Bounds())
	}
	if cropped.Bounds().Dx() < 150 || cropped.Bounds().Dy() < 230 {
		t.Fatalf("content crop was too aggressive: got %v", cropped.Bounds())
	}
}

func TestMalformedEXIFDoesNotPanic(_ *testing.T) {
	inputs := [][]byte{
		{0xff, 0xd8, 0xff, 0xe1, 0xff, 0xff},
		{0xff, 0xd8, 0xff, 0xe1, 0, 2},
		append(jpegWithOrientation(6)[:12], 0xff, 0xd9),
	}
	for _, input := range inputs {
		_ = getEXIFOrientation(bytes.Clone(input))
	}
}
