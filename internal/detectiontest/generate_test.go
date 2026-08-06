package detectiontest

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

type fakeImageFetcher struct {
	calls int
	image image.Image
}

func (f *fakeImageFetcher) Fetch(_ context.Context, card Card) (FetchedImage, error) {
	f.calls++
	return FetchedImage{
		Bytes:  []byte("source:" + card.SourceID),
		Image:  f.image,
		Format: card.ImageFormat,
		MIME:   card.ImageMIME,
	}, nil
}

func TestGeneratorGenerate(t *testing.T) {
	t.Parallel()

	fixture := image.NewNRGBA(image.Rect(0, 0, 12, 18))
	for y := range 18 {
		for x := range 12 {
			fixture.SetNRGBA(x, y, color.NRGBA{
				R: testColorChannel(x * 13),
				G: testColorChannel(y * 9),
				B: testColorChannel((x + y) * 6),
				A: 255,
			})
		}
	}
	fetcher := &fakeImageFetcher{image: fixture}
	generator := &Generator{
		downloader: fetcher,
		outputRoot: t.TempDir(),
	}

	runPath, err := generator.Generate(t.Context(), 42, SupportedGameCount)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if fetcher.calls != SupportedGameCount {
		t.Errorf("Fetch() calls = %d, want %d", fetcher.calls, SupportedGameCount)
	}
	manifest, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if err := VerifyRun(runPath, manifest.Version, ManifestSHA256(), 42, SupportedGameCount); err != nil {
		t.Fatalf("VerifyRun() error = %v", err)
	}

	manifestBytes, err := os.ReadFile(filepath.Join(runPath, outputManifestName))
	if err != nil {
		t.Fatalf("os.ReadFile(selection.json) error = %v", err)
	}
	var output OutputManifest
	if err := json.Unmarshal(manifestBytes, &output); err != nil {
		t.Fatalf("json.Unmarshal(selection.json) error = %v", err)
	}
	if len(output.Cards) != SupportedGameCount || len(output.Cards[0].Artifacts) != 7 {
		t.Fatalf("generated output has unexpected shape: %#v", output)
	}

	manifestPath := filepath.Join(runPath, outputManifestName)
	originalName := output.Cards[0].Card.Name
	output.Cards[0].Card.Name = "redefined expected card"
	writeOutputManifestForTest(t, manifestPath, output)
	if err := VerifyRun(runPath, manifest.Version, ManifestSHA256(), 42, SupportedGameCount); err == nil {
		t.Error("VerifyRun() accepted redefined card identity")
	}
	output.Cards[0].Card.Name = originalName
	originalVariant := output.Cards[0].Artifacts[0].Variant
	output.Cards[0].Artifacts[0].Variant = "unexpected"
	writeOutputManifestForTest(t, manifestPath, output)
	if err := VerifyRun(runPath, manifest.Version, ManifestSHA256(), 42, SupportedGameCount); err == nil {
		t.Error("VerifyRun() accepted unexpected variant")
	}
	output.Cards[0].Artifacts[0].Variant = originalVariant
	if err := os.WriteFile(manifestPath, manifestBytes, 0o600); err != nil {
		t.Fatalf("restore selection.json: %v", err)
	}

	secondPath, err := generator.Generate(t.Context(), 42, SupportedGameCount)
	if err != nil {
		t.Fatalf("second Generate() error = %v", err)
	}
	if secondPath != runPath {
		t.Errorf("second Generate() path = %q, want %q", secondPath, runPath)
	}
	if fetcher.calls != SupportedGameCount {
		t.Errorf("idempotent Generate() made %d fetches, want %d", fetcher.calls, SupportedGameCount)
	}

	artifactPath := filepath.Join(runPath, filepath.FromSlash(output.Cards[0].Artifacts[0].Path))
	if err := os.WriteFile(artifactPath, []byte("tampered"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(tampered artifact) error = %v", err)
	}
	if _, err := generator.Generate(t.Context(), 42, SupportedGameCount); err == nil {
		t.Error("Generate() accepted a tampered immutable run")
	}
}

func writeOutputManifestForTest(t *testing.T, path string, output OutputManifest) {
	t.Helper()
	payload, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent(output) error = %v", err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("os.WriteFile(selection.json) error = %v", err)
	}
}

func TestWriteAtomic(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "fixture.bin")
	original := []byte("original fixture")
	if err := writeAtomic(path, original); err != nil {
		t.Fatalf("writeAtomic() error = %v", err)
	}
	if err := writeAtomic(path, original); err != nil {
		t.Fatalf("idempotent writeAtomic() error = %v", err)
	}
	if err := writeAtomic(path, []byte("changed fixture")); err == nil {
		t.Error("writeAtomic() overwrite error = nil, want error")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("file contents = %q, want %q", got, original)
	}
}

func TestRunDirectoryName(t *testing.T) {
	t.Parallel()

	got := runDirectoryName(1, 20260804, 6)
	expected := "v1-seed-20260804-count-6"
	if got != expected {
		t.Errorf("runDirectoryName() = %q, want %q", got, expected)
	}
}
