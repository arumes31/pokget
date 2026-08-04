package service

import (
	"crypto/sha256"
	"image"
	"image/color"
	"image/draw"
	"pokget/internal/models"
	"strings"
	"testing"
)

func TestBuildOCRPassesUsesLayoutAndDeduplicates(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 180, 260))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(15, 15, 165, 45), &image.Uniform{C: color.Black}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(90, 220, 170, 245), &image.Uniform{C: color.RGBA{R: 30, G: 60, B: 180, A: 255}}, image.Point{}, draw.Src)
	passes, processed, err := buildOCRPasses(img, OCRScanConfig{Game: "pokemon", UseLayoutROIs: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(processed) == 0 {
		t.Fatal("processed image is empty")
	}
	seenHashes := make(map[[sha256.Size]byte]struct{}, len(passes))
	roles := make(map[string]bool)
	for _, pass := range passes {
		hash := sha256.Sum256(pass.Image)
		if _, duplicate := seenHashes[hash]; duplicate {
			t.Fatalf("duplicate pass image %q", pass.Name)
		}
		seenHashes[hash] = struct{}{}
		roles[pass.Role] = true
	}
	if !roles["name"] || !roles["identifier"] {
		t.Fatalf("layout roles missing from passes: %v", roles)
	}
}

func TestCombineOCRResultsRanksAndDeduplicates(t *testing.T) {
	results := []ocrPassResult{
		{Pass: ocrPass{Name: "gray", Role: "full", Weight: 1}, Text: "Pikachu", Quality: 4},
		{Pass: ocrPass{Name: "name", Role: "name", Weight: 20}, Text: "  Pikachu  ", Quality: 10},
		{Pass: ocrPass{Name: "number", Role: "identifier", Weight: 15}, Text: "SV1-025", Quality: 8},
	}
	text, evidence := combineOCRResults(results)
	if text != "Pikachu\nSV1-025" {
		t.Fatalf("combined text = %q", text)
	}
	if len(evidence) != 3 {
		t.Fatalf("evidence count = %d, want all three pass observations", len(evidence))
	}
	if evidence[0].Pass != "name" || !strings.Contains(text, "SV1-025") {
		t.Fatalf("results were not ranked as expected: %#v", evidence)
	}
}

func TestInferOCRGameRequiresSingleGame(t *testing.T) {
	if got := inferOCRGame([]models.Card{{Game: "Pokémon"}, {Game: "pokemon_tcg"}}); got != "pokemon" {
		t.Fatalf("inferred game = %q", got)
	}
	if got := inferOCRGame([]models.Card{{Game: "pokemon"}, {Game: "magic"}}); got != "" {
		t.Fatalf("mixed games inferred as %q", got)
	}
}
