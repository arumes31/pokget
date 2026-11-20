package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"pokget/internal/claudedetect"
	"pokget/internal/models"
)

// uprightTestCard draws an asymmetric portrait card: a red title band, a blue
// block bottom-left and a horizontal gradient, so any rotation is visible.
func uprightTestCard(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixel := color.RGBA{R: uint8(x * 255 / width), G: 200, B: 60, A: 255}
			switch {
			case y < height/5:
				pixel = color.RGBA{R: 220, G: 20, B: 20, A: 255}
			case y > height*3/5 && x < width/2:
				pixel = color.RGBA{R: 20, G: 20, B: 220, A: 255}
			}
			img.Set(x, y, pixel)
		}
	}
	return img
}

// exifJPEG encodes img as JPEG with an APP1 Exif segment carrying orientation.
func exifJPEG(t *testing.T, img image.Image, orientation uint16) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.BigEndian.PutUint16(tiff[18:20], orientation)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segment := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2)) // #nosec G115 - tiny test segment
	raw := encoded.Bytes()
	out := append([]byte{}, raw[:2]...)
	out = append(out, segment...)
	out = append(out, payload...)
	return append(out, raw[2:]...)
}

// sensorJPEG stores the upright card as a camera does for EXIF orientation 6:
// the pixels are rotated counter-clockwise and the tag says "rotate back".
func sensorJPEG(t *testing.T, upright image.Image) []byte {
	t.Helper()
	return exifJPEG(t, applyImageOrientation(upright, 8), 6)
}

func TestPrepareScanSourceAppliesEXIFOrientationAfterResize(t *testing.T) {
	upright := uprightTestCard(900, 1300)
	data := sensorJPEG(t, upright)
	if getEXIFOrientation(data) != 6 {
		t.Fatalf("test JPEG must carry orientation 6")
	}
	src, _, err := decodeScanImage(data, ImagePrepareConfig{})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	prepared, err := prepareScanSource(data, src, ImagePrepareConfig{}, 600)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	bounds := prepared.Bounds()
	if bounds.Dx() >= bounds.Dy() || max(bounds.Dx(), bounds.Dy()) > 600 {
		t.Fatalf("expected an upright card capped at 600 px, got %v", bounds)
	}
	isRed := func(c color.Color) bool { r, g, b, _ := c.RGBA(); return r>>8 > 180 && g>>8 < 80 && b>>8 < 80 }
	isBlue := func(c color.Color) bool { r, g, b, _ := c.RGBA(); return b>>8 > 180 && r>>8 < 80 && g>>8 < 80 }
	if top := prepared.At(bounds.Min.X+bounds.Dx()/2, bounds.Min.Y+bounds.Dy()/20); !isRed(top) {
		t.Errorf("title band must be at the top, got %v", top)
	}
	if corner := prepared.At(bounds.Min.X+bounds.Dx()/5, bounds.Max.Y-bounds.Dy()/10); !isBlue(corner) {
		t.Errorf("blue block must be bottom-left, got %v", corner)
	}
}

func TestScanPixelLimitBoundsDecodedBytes(t *testing.T) {
	encode := func(img image.Image) []byte {
		var buffer bytes.Buffer
		if err := png.Encode(&buffer, img); err != nil {
			t.Fatalf("encode png: %v", err)
		}
		return buffer.Bytes()
	}
	tests := []struct {
		name   string
		data   []byte
		reject bool
	}{
		{"8-bit above 16 MiP", encode(image.NewGray(image.Rect(0, 0, 4200, 4200))), true},
		{"16-bit above the byte budget", encode(image.NewNRGBA64(image.Rect(0, 0, 3000, 3000))), true},
		{"16-bit within the byte budget", encode(image.NewNRGBA64(image.Rect(0, 0, 2000, 2000))), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var inputErr *ImageInputError
			err := ValidateScanImage(test.data)
			if test.reject != errors.As(err, &inputErr) {
				t.Errorf("ValidateScanImage: reject=%v, got %v", test.reject, err)
			}
			_, _, err = decodeScanImage(test.data, ImagePrepareConfig{})
			if test.reject != errors.As(err, &inputErr) {
				t.Errorf("decodeScanImage: reject=%v, got %v", test.reject, err)
			}
		})
	}
}

func TestFingerprintTieBreakHashesTheOrientedImage(t *testing.T) {
	fingerprints := NewFingerprintService()
	upright := uprightTestCard(600, 840)
	uprightJPEG := exifJPEG(t, upright, 1)
	decoded, _, err := decodeScanImage(uprightJPEG, ImagePrepareConfig{})
	if err != nil {
		t.Fatalf("decode upright: %v", err)
	}
	reference, err := prepareScanSource(uprightJPEG, decoded, ImagePrepareConfig{}, claudeSourceMaxDimension)
	if err != nil {
		t.Fatalf("prepare upright: %v", err)
	}
	stored, err := fingerprints.CalculateHash(reference)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	upload := sensorJPEG(t, upright)
	raw, _, err := image.Decode(bytes.NewReader(upload))
	if err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	rawHash, err := fingerprints.CalculateHash(raw)
	if err != nil {
		t.Fatalf("hash raw: %v", err)
	}
	if distance := hammingDistance(uint64(rawHash), uint64(stored)); distance <= fingerprints.PhashPotential { // #nosec G115 - comparing hash bits
		t.Fatalf("the sideways sensor image must hash far from the card (distance %d)", distance)
	}

	pipeline := NewDetectionPipeline(fingerprints, nil)
	pipeline.Claude = &mockClaudeDetector{info: &claudedetect.DetectedCardInfo{
		Game: "pokemon", Name: "Rayquaza", CollectorNumber: "16/110", Set: "EX Holon Phantoms",
		SetCode: "HP", Language: "deu", Confidence: 0.98,
	}}
	result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{
		Image: upload,
		Cards: []models.Card{{
			ID: "rayquaza-hp-16", Name: "Rayquaza", CollectorNumber: "16/110", Set: "EX Holon Phantoms",
			SetCode: "HP", Language: "deu", Game: "pokemon", Phash: &stored,
		}},
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageGerman},
	})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if result.BestMatchID() != "rayquaza-hp-16" || result.BestMatchNeedsReview() {
		t.Errorf("an EXIF-rotated photo of the stored card must match without review, got %s review=%v",
			result.BestMatchID(), result.BestMatchNeedsReview())
	}
}
