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

	"pokget/internal/models"

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

func TestTesseractDisambiguatesSameArtworkPrinting(t *testing.T) {
	for _, test := range []struct {
		text       string
		wantID     string
		wantReview bool
	}{
		{text: "PIKACHU", wantID: "printing-a", wantReview: true},
		{text: "SV2 025", wantID: "printing-b", wantReview: false},
	} {
		t.Run(test.text, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			payload := syntheticOCRImage(t, test.text)
			decoded, err := png.Decode(bytes.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			fingerprint := NewFingerprintService(nil)
			hash, err := fingerprint.CalculateHash(decoded)
			if err != nil {
				t.Fatal(err)
			}
			cards := []models.Card{
				{ID: "printing-a", Name: "Pikachu", Game: "pokemon", Language: "en", SetCode: "SV1", CollectorNumber: "025", Phash: &hash},
				{ID: "printing-b", Name: "Pikachu", Game: "pokemon", Language: "en", SetCode: "SV2", CollectorNumber: "025", Phash: &hash},
			}
			result, err := NewDetectionPipeline(fingerprint, nil).DetectScoped(ctx, DetectionRequest{
				Image: payload, Cards: cards, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.BestMatchID() != test.wantID || result.BestMatchNeedsReview() != test.wantReview {
				t.Fatalf("OCR text %q: result ID=%q review=%t, want ID=%q review=%t", result.OCRText, result.BestMatchID(), result.BestMatchNeedsReview(), test.wantID, test.wantReview)
			}
		})
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
	t.Cleanup(func() { _ = face.Close() })

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
