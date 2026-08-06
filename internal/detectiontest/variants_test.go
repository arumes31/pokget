package detectiontest

import (
	"bytes"
	"crypto/sha256"
	"image"
	"image/color"
	"slices"
	"testing"
)

func TestRenderVariants(t *testing.T) {
	t.Parallel()

	source := image.NewNRGBA(image.Rect(0, 0, 20, 30))
	for y := range 30 {
		for x := range 20 {
			source.SetNRGBA(x, y, color.NRGBA{
				R: testColorChannel(x * 10),
				G: testColorChannel(y * 7),
				B: testColorChannel((x + y) * 4),
				A: 255,
			})
		}
	}
	originalPixels := slices.Clone(source.Pix)

	first, err := RenderVariants(source)
	if err != nil {
		t.Fatalf("RenderVariants() error = %v", err)
	}
	second, err := RenderVariants(source)
	if err != nil {
		t.Fatalf("second RenderVariants() error = %v", err)
	}
	if !slices.Equal(source.Pix, originalPixels) {
		t.Error("RenderVariants mutated its source")
	}

	expected := []struct {
		name   string
		format string
		width  int
		height int
	}{
		{name: "clean", format: "png", width: 20, height: 30},
		{name: "blur", format: "png", width: 20, height: 30},
		{name: "resize", format: "png", width: 14, height: 21},
		{name: "rotate", format: "png", width: 20, height: 30},
		{name: "brightness", format: "png", width: 20, height: 30},
		{name: "jpeg", format: "jpeg", width: 20, height: 30},
	}
	if len(first) != len(expected) {
		t.Fatalf("len(variants) = %d, want %d", len(first), len(expected))
	}
	for index, expectedVariant := range expected {
		variant := first[index]
		if variant.Name != expectedVariant.name {
			t.Errorf("variant[%d].Name = %q, want %q", index, variant.Name, expectedVariant.name)
		}
		decoded, format, err := image.Decode(bytes.NewReader(variant.Bytes))
		if err != nil {
			t.Errorf("image.Decode(%s) error = %v", variant.Name, err)
			continue
		}
		if format != expectedVariant.format {
			t.Errorf("image.Decode(%s) format = %q, want %q", variant.Name, format, expectedVariant.format)
		}
		if decoded.Bounds().Dx() != expectedVariant.width || decoded.Bounds().Dy() != expectedVariant.height {
			t.Errorf(
				"image.Decode(%s) dimensions = %v, want %dx%d",
				variant.Name,
				decoded.Bounds(),
				expectedVariant.width,
				expectedVariant.height,
			)
		}
		if sha256.Sum256(variant.Bytes) != sha256.Sum256(second[index].Bytes) {
			t.Errorf("%s variant is not deterministic", variant.Name)
		}
	}
	if bytes.Equal(first[0].Bytes, first[1].Bytes) {
		t.Error("blur variant equals clean variant")
	}
	if bytes.Equal(first[0].Bytes, first[4].Bytes) {
		t.Error("brightness variant equals clean variant")
	}
}

func testColorChannel(value int) uint8 {
	if value < 0 || value > 255 {
		panic("test color channel is outside byte range")
	}
	return uint8(value) // #nosec G115 -- the test helper enforces the byte range.
}

func TestRenderVariantsRejectsNil(t *testing.T) {
	t.Parallel()

	if _, err := RenderVariants(nil); err == nil {
		t.Error("RenderVariants(nil) error = nil, want error")
	}
}
