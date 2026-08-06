package service

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/anthonynsimon/bild/adjust"
	"github.com/anthonynsimon/bild/channel"
	"github.com/anthonynsimon/bild/effect"
	"github.com/anthonynsimon/bild/transform"
	"golang.org/x/text/unicode/norm"
)

type ocrPass struct {
	Name             string
	Role             string
	PageSegmentation string
	Weight           float64
	Image            []byte
}

type ocrPassResult struct {
	Pass    ocrPass
	Text    string
	Quality float64
}

type ocrLayoutROI struct {
	Name string
	Role string
	Rect OCRNormalizedRect
}

var ocrLayouts = map[string][]ocrLayoutROI{
	"pokemon": {
		{Name: "name", Role: "name", Rect: OCRNormalizedRect{MinX: 0.04, MinY: 0.03, MaxX: 0.96, MaxY: 0.23}},
		{Name: "number", Role: "identifier", Rect: OCRNormalizedRect{MinX: 0.43, MinY: 0.78, MaxX: 0.99, MaxY: 0.99}},
	},
	"magic": {
		{Name: "name", Role: "name", Rect: OCRNormalizedRect{MinX: 0.04, MinY: 0.03, MaxX: 0.96, MaxY: 0.18}},
		{Name: "collector", Role: "identifier", Rect: OCRNormalizedRect{MinX: 0.38, MinY: 0.84, MaxX: 0.99, MaxY: 0.99}},
	},
	"one_piece": {
		{Name: "name", Role: "name", Rect: OCRNormalizedRect{MinX: 0.04, MinY: 0.65, MaxX: 0.96, MaxY: 0.86}},
		{Name: "number", Role: "identifier", Rect: OCRNormalizedRect{MinX: 0.42, MinY: 0.82, MaxX: 0.99, MaxY: 0.99}},
	},
	"lorcana": {
		{Name: "name", Role: "name", Rect: OCRNormalizedRect{MinX: 0.04, MinY: 0.64, MaxX: 0.96, MaxY: 0.84}},
		{Name: "number", Role: "identifier", Rect: OCRNormalizedRect{MinX: 0.48, MinY: 0.83, MaxX: 0.99, MaxY: 0.99}},
	},
	"weiss_schwarz": {
		{Name: "name", Role: "name", Rect: OCRNormalizedRect{MinX: 0.03, MinY: 0.64, MaxX: 0.97, MaxY: 0.86}},
		{Name: "number", Role: "identifier", Rect: OCRNormalizedRect{MinX: 0.34, MinY: 0.82, MaxX: 0.99, MaxY: 0.99}},
	},
	"yugioh": {
		{Name: "name", Role: "name", Rect: OCRNormalizedRect{MinX: 0.05, MinY: 0.07, MaxX: 0.95, MaxY: 0.23}},
		{Name: "number", Role: "identifier", Rect: OCRNormalizedRect{MinX: 0.42, MinY: 0.82, MaxX: 0.99, MaxY: 0.98}},
	},
}

func buildOCRPasses(src image.Image, config OCRScanConfig) ([]ocrPass, []byte, error) {
	base := upscaleForOCR(src)
	gray := buildOCRPreviewImage(base)
	grayBytes, err := encodeOCRJPEG(gray)
	if err != nil {
		return nil, nil, fmt.Errorf("encode grayscale OCR pass: %w", err)
	}

	passImages := []struct {
		pass  ocrPass
		image image.Image
	}{
		{pass: ocrPass{Name: "grayscale", Role: "full", PageSegmentation: "3", Weight: 12}, image: gray},
		{pass: ocrPass{Name: "blue", Role: "full", PageSegmentation: "11", Weight: 8}, image: channel.Extract(base, channel.Blue)},
		{pass: ocrPass{Name: "binary", Role: "full", PageSegmentation: "3", Weight: 10}, image: preprocessForOCR(base)},
	}

	if config.UseLayoutROIs {
		for _, roi := range ocrLayouts[normalizeGame(config.Game)] {
			cropped, cropErr := cropNormalized(base, roi.Rect)
			if cropErr != nil {
				continue
			}
			processed := effect.Grayscale(upscaleSmallROI(cropped))
			processed = adjust.Contrast(processed, 0.35)
			processed = effect.Sharpen(processed)
			segmentation := "7"
			if roi.Role == "identifier" {
				segmentation = "11"
			}
			passImages = append(passImages, struct {
				pass  ocrPass
				image image.Image
			}{
				pass:  ocrPass{Name: "layout_" + roi.Name, Role: roi.Role, PageSegmentation: segmentation, Weight: 18},
				image: processed,
			})
		}
	}

	passes := make([]ocrPass, 0, len(passImages))
	seen := make(map[[sha256.Size]byte]struct{}, len(passImages))
	for _, candidate := range passImages {
		encoded, encodeErr := encodeOCRJPEG(candidate.image)
		if encodeErr != nil {
			return nil, nil, fmt.Errorf("encode %s OCR pass: %w", candidate.pass.Name, encodeErr)
		}
		hash := sha256.Sum256(encoded)
		if _, duplicate := seen[hash]; duplicate {
			continue
		}
		seen[hash] = struct{}{}
		candidate.pass.Image = encoded
		passes = append(passes, candidate.pass)
	}
	return passes, append([]byte(nil), grayBytes...), nil
}

//nolint:unused // Used by ocr_stub.go when Tesseract/CGO is unavailable.
func buildOCRPreview(src image.Image) ([]byte, error) {
	return encodeOCRJPEG(buildOCRPreviewImage(upscaleForOCR(src)))
}

func buildOCRPreviewImage(src image.Image) image.Image {
	preview := effect.Grayscale(src)
	preview = adjust.Contrast(preview, 0.3)
	preview = adjust.Brightness(preview, 0.05)
	return effect.Sharpen(preview)
}

func upscaleForOCR(src image.Image) image.Image {
	bounds := src.Bounds()
	if max(bounds.Dx(), bounds.Dy()) > maxUpscaleDim {
		return resizeToMaxDimension(src, maxUpscaleDim)
	}
	scaleX, scaleY := computeUpscaleFactors(bounds)
	if scaleX <= 1 && scaleY <= 1 {
		return src
	}
	width := int(math.Round(float64(bounds.Dx()) * scaleX))
	height := int(math.Round(float64(bounds.Dy()) * scaleY))
	return transform.Resize(src, width, height, transform.Lanczos)
}

func upscaleSmallROI(src image.Image) image.Image {
	bounds := src.Bounds()
	if bounds.Dx() >= 900 || bounds.Dy() >= 220 {
		return src
	}
	scale := min(3.0, max(1.5, 900/float64(max(bounds.Dx(), 1))))
	return transform.Resize(src,
		max(1, int(math.Round(float64(bounds.Dx())*scale))),
		max(1, int(math.Round(float64(bounds.Dy())*scale))),
		transform.Lanczos,
	)
}

func encodeOCRJPEG(src image.Image) ([]byte, error) {
	buffer := new(bytes.Buffer)
	if err := jpeg.Encode(buffer, flattenOnWhite(src), &jpeg.Options{Quality: 95}); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func scoreOCRText(text string) float64 {
	text = strings.TrimSpace(norm.NFKC.String(text))
	if text == "" {
		return 0
	}
	useful, controls, replacements := 0, 0, 0
	for _, r := range text {
		switch {
		case r == unicode.ReplacementChar:
			replacements++
		case unicode.IsControl(r) && !unicode.IsSpace(r):
			controls++
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			useful++
		}
	}
	return math.Min(float64(useful), 80) - float64(controls*4+replacements*6)
}

func combineOCRResults(results []ocrPassResult) (string, []ocrEvidence) {
	results = append([]ocrPassResult(nil), results...)
	slices.SortFunc(results, func(left, right ocrPassResult) int {
		leftScore := left.Quality + left.Pass.Weight
		rightScore := right.Quality + right.Pass.Weight
		if order := cmp.Compare(rightScore, leftScore); order != 0 {
			return order
		}
		return cmp.Compare(left.Pass.Name, right.Pass.Name)
	})

	seen := make(map[string]struct{}, len(results))
	texts := make([]string, 0, len(results))
	evidence := make([]ocrEvidence, 0, len(results))
	for _, result := range results {
		text := strings.TrimSpace(result.Text)
		if text == "" {
			continue
		}
		evidence = append(evidence, ocrEvidence{
			Text:    text,
			Pass:    result.Pass.Name,
			Role:    result.Pass.Role,
			Quality: result.Quality,
		})
		fingerprint := strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(text)), " "))
		if _, duplicate := seen[fingerprint]; duplicate {
			continue
		}
		seen[fingerprint] = struct{}{}
		texts = append(texts, text)
	}
	return strings.Join(texts, "\n"), evidence
}
