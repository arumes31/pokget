//go:build ocrintegration && cgo && linux

package service

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/otiai10/gosseract/v2"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func TestTesseractRecognizesSyntheticCardName(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()

	client, err := acquireOCRClientContext(ctx)
	if err != nil {
		t.Fatalf("acquire real Tesseract client: %v", err)
	}
	defer releaseOCRClient(client, true)

	if err := client.SetLanguage("eng"); err != nil {
		t.Fatalf("load English Tesseract data: %v", err)
	}
	client.SetPageSegMode(gosseract.PSM_SINGLE_LINE)
	if err := client.SetImageFromBytes(syntheticOCRImage(t, "PIKACHU")); err != nil {
		t.Fatalf("set generated OCR image: %v", err)
	}
	text, err := client.Text()
	if err != nil {
		t.Fatalf("run real Tesseract OCR: %v", err)
	}
	if !strings.Contains(normalizeMatchText(text), "pikachu") {
		t.Fatalf("Tesseract text = %q, want it to contain PIKACHU", text)
	}
}

func syntheticOCRImage(t *testing.T, value string) []byte {
	t.Helper()

	parsedFont, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatalf("parse embedded Go font: %v", err)
	}
	face, err := opentype.NewFace(parsedFont, &opentype.FaceOptions{
		Size:    72,
		DPI:     96,
		Hinting: font.HintingFull,
	})
	if err != nil {
		t.Fatalf("create embedded Go font face: %v", err)
	}

	cardName := image.NewRGBA(image.Rect(0, 0, 900, 180))
	for offset := 0; offset < len(cardName.Pix); offset += 4 {
		cardName.Pix[offset] = 0xff
		cardName.Pix[offset+1] = 0xff
		cardName.Pix[offset+2] = 0xff
		cardName.Pix[offset+3] = 0xff
	}
	drawer := font.Drawer{
		Dst:  cardName,
		Src:  image.NewUniform(color.Black),
		Face: face,
		Dot:  fixed.P(36, 125),
	}
	drawer.DrawString(value)

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, cardName); err != nil {
		t.Fatalf("encode generated OCR image: %v", err)
	}
	return encoded.Bytes()
}
