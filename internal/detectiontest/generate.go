package detectiontest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

const (
	outputManifestName = "selection.json"
	maxOutputFileBytes = int64(32 << 20)
	maxManifestBytes   = int64(1 << 20)
)

// Generator creates an immutable, fully validated fixture run.
type Generator struct {
	downloader imageFetcher
	outputRoot string
}

type imageFetcher interface {
	Fetch(context.Context, Card) (FetchedImage, error)
}

// GeneratorConfig configures fixture generation.
type GeneratorConfig struct {
	Downloader *Downloader
	OutputRoot string
}

// OutputManifest records the exact selection and generated artifact hashes.
type OutputManifest struct {
	Version        int          `json:"version"`
	SourceManifest string       `json:"source_manifest_sha256"`
	Seed           int64        `json:"seed"`
	SelectionCount int          `json:"selection_count"`
	Cards          []OutputCard `json:"cards"`
}

// OutputCard records one selected card and its generated files.
type OutputCard struct {
	Card      Card       `json:"card"`
	Artifacts []Artifact `json:"artifacts"`
}

// Artifact records the integrity and dimensions of one generated file.
type Artifact struct {
	Variant string `json:"variant"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	MIME    string `json:"mime"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

// NewGenerator constructs an isolated artifact generator.
func NewGenerator(config GeneratorConfig) (*Generator, error) {
	if config.Downloader == nil {
		return nil, errors.New("fixture generator: downloader is required")
	}
	if config.OutputRoot == "" {
		return nil, errors.New("fixture generator: output root is required")
	}
	return &Generator{
		downloader: config.Downloader,
		outputRoot: filepath.Clean(config.OutputRoot),
	}, nil
}

// Generate creates or verifies the immutable run for seed and count.
func (g *Generator) Generate(
	ctx context.Context,
	seed int64,
	count int,
) (string, error) {
	if g == nil || g.downloader == nil {
		return "", errors.New("generating fixtures: nil generator")
	}
	manifest, err := LoadManifest()
	if err != nil {
		return "", fmt.Errorf("generating fixtures: %w", err)
	}
	selected, err := Select(manifest.Cards, seed, count)
	if err != nil {
		return "", err
	}
	if err := ensureOutputRoot(g.outputRoot); err != nil {
		return "", err
	}

	runPath := filepath.Join(g.outputRoot, runDirectoryName(manifest.Version, seed, count))
	if _, err := os.Lstat(runPath); err == nil {
		if err := VerifyRun(runPath, manifest.Version, ManifestSHA256(), seed, count); err != nil {
			return "", fmt.Errorf("verifying existing fixture run: %w", err)
		}
		return runPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("checking fixture run: %w", err)
	}

	stagePath, err := os.MkdirTemp(g.outputRoot, ".stage-")
	if err != nil {
		return "", fmt.Errorf("creating fixture stage: %w", err)
	}
	published := false
	defer func() {
		if !published {
			removeStage(g.outputRoot, stagePath)
		}
	}()

	output := OutputManifest{
		Version:        manifest.Version,
		SourceManifest: ManifestSHA256(),
		Seed:           seed,
		SelectionCount: count,
		Cards:          make([]OutputCard, 0, len(selected)),
	}
	for _, card := range selected {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("generating fixtures: %w", err)
		}
		fetched, err := g.downloader.Fetch(ctx, card)
		if err != nil {
			return "", err
		}

		cardOutput, err := writeCardArtifacts(stagePath, card, fetched)
		if err != nil {
			return "", err
		}
		output.Cards = append(output.Cards, cardOutput)
	}

	manifestBytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding output manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := writeAtomic(filepath.Join(stagePath, outputManifestName), manifestBytes); err != nil {
		return "", fmt.Errorf("writing output manifest: %w", err)
	}

	if err := os.Rename(stagePath, runPath); err != nil {
		return "", fmt.Errorf("publishing fixture run: %w", err)
	}
	published = true
	return runPath, nil
}

// VerifyRun verifies every file in an existing immutable fixture run.
func VerifyRun(runPath string, version int, manifestHash string, seed int64, count int) error {
	sourceManifest, err := LoadManifest()
	if err != nil {
		return fmt.Errorf("loading source manifest: %w", err)
	}
	if version != sourceManifest.Version || manifestHash != ManifestSHA256() {
		return errors.New("requested fixture run does not match the current source manifest")
	}
	expectedCards, err := Select(sourceManifest.Cards, seed, count)
	if err != nil {
		return fmt.Errorf("recomputing fixture selection: %w", err)
	}

	info, err := os.Lstat(runPath)
	if err != nil {
		return fmt.Errorf("examining run directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("run path is not a real directory")
	}

	manifestPath := filepath.Join(runPath, outputManifestName)
	manifestBytes, err := readBoundedRegularFile(manifestPath, maxManifestBytes)
	if err != nil {
		return fmt.Errorf("reading output manifest: %w", err)
	}
	var output OutputManifest
	if err := json.Unmarshal(manifestBytes, &output); err != nil {
		return fmt.Errorf("decoding output manifest: %w", err)
	}
	if output.Version != version || output.SourceManifest != manifestHash {
		return errors.New("output manifest does not match source manifest")
	}
	if output.Seed != seed || output.SelectionCount != count || len(output.Cards) != count {
		return errors.New("output manifest does not match requested selection")
	}

	seenPaths := make(map[string]struct{})
	for cardIndex, card := range output.Cards {
		if card.Card != expectedCards[cardIndex] {
			return fmt.Errorf(
				"card %d identity does not match seeded source selection",
				cardIndex,
			)
		}
		if len(card.Artifacts) != 7 {
			return fmt.Errorf("card %q has %d artifacts, want 7", card.Card.SourceID, len(card.Artifacts))
		}
		expectedVariants := map[string]string{
			"source":     card.Card.ImageMIME,
			"clean":      "image/png",
			"blur":       "image/png",
			"resize":     "image/png",
			"rotate":     "image/png",
			"brightness": "image/png",
			"jpeg":       "image/jpeg",
		}
		seenVariants := make(map[string]struct{}, len(expectedVariants))
		for _, artifact := range card.Artifacts {
			expectedMIME, exists := expectedVariants[artifact.Variant]
			if !exists {
				return fmt.Errorf("card %q has unexpected variant %q", card.Card.SourceID, artifact.Variant)
			}
			if _, exists := seenVariants[artifact.Variant]; exists {
				return fmt.Errorf("card %q has duplicate variant %q", card.Card.SourceID, artifact.Variant)
			}
			seenVariants[artifact.Variant] = struct{}{}
			if artifact.MIME != expectedMIME {
				return fmt.Errorf(
					"card %q variant %q has mime %q, want %q",
					card.Card.SourceID,
					artifact.Variant,
					artifact.MIME,
					expectedMIME,
				)
			}
			if !filepath.IsLocal(filepath.FromSlash(artifact.Path)) {
				return fmt.Errorf("artifact path %q is not local", artifact.Path)
			}
			if _, exists := seenPaths[artifact.Path]; exists {
				return fmt.Errorf("duplicate artifact path %q", artifact.Path)
			}
			seenPaths[artifact.Path] = struct{}{}

			payload, err := readBoundedRegularFile(
				filepath.Join(runPath, filepath.FromSlash(artifact.Path)),
				maxOutputFileBytes,
			)
			if err != nil {
				return fmt.Errorf("reading artifact %q: %w", artifact.Path, err)
			}
			if int64(len(payload)) != artifact.Size {
				return fmt.Errorf("artifact %q size mismatch", artifact.Path)
			}
			if hashBytes(payload) != artifact.SHA256 {
				return fmt.Errorf("artifact %q sha256 mismatch", artifact.Path)
			}
		}
		if len(seenVariants) != len(expectedVariants) {
			return fmt.Errorf("card %q is missing fixture variants", card.Card.SourceID)
		}
	}
	return nil
}

func writeCardArtifacts(stagePath string, card Card, fetched FetchedImage) (OutputCard, error) {
	cardDirectory := filepath.Join(stagePath, card.GameSlug, card.FileSlug)
	if err := os.MkdirAll(cardDirectory, 0o750); err != nil {
		return OutputCard{}, fmt.Errorf("creating card artifact directory %q: %w", card.SourceID, err)
	}

	width := fetched.Image.Bounds().Dx()
	height := fetched.Image.Bounds().Dy()
	sourceName := "source." + extensionForFormat(fetched.Format)
	sourcePath := filepath.Join(cardDirectory, sourceName)
	if err := writeAtomic(sourcePath, fetched.Bytes); err != nil {
		return OutputCard{}, fmt.Errorf("writing source fixture %q: %w", card.SourceID, err)
	}
	sourceArtifact, err := newArtifact(
		"source",
		stagePath,
		sourcePath,
		fetched.Bytes,
		fetched.MIME,
		width,
		height,
	)
	if err != nil {
		return OutputCard{}, fmt.Errorf("recording source fixture %q: %w", card.SourceID, err)
	}
	artifacts := []Artifact{sourceArtifact}

	variants, err := RenderVariants(fetched.Image)
	if err != nil {
		return OutputCard{}, fmt.Errorf("rendering fixture %q: %w", card.SourceID, err)
	}
	for _, variant := range variants {
		variantPath := filepath.Join(cardDirectory, variant.Name+"."+variant.Ext)
		if err := writeAtomic(variantPath, variant.Bytes); err != nil {
			return OutputCard{}, fmt.Errorf(
				"writing %s fixture %q: %w",
				variant.Name,
				card.SourceID,
				err,
			)
		}
		artifact, err := newArtifact(
			variant.Name,
			stagePath,
			variantPath,
			variant.Bytes,
			variant.MIME,
			variant.Width,
			variant.Height,
		)
		if err != nil {
			return OutputCard{}, fmt.Errorf(
				"recording %s fixture %q: %w",
				variant.Name,
				card.SourceID,
				err,
			)
		}
		artifacts = append(artifacts, artifact)
	}
	return OutputCard{Card: card, Artifacts: artifacts}, nil
}

func newArtifact(
	variant string,
	stagePath string,
	path string,
	payload []byte,
	MIME string,
	width int,
	height int,
) (Artifact, error) {
	relativePath, err := filepath.Rel(stagePath, path)
	if err != nil {
		return Artifact{}, err
	}
	if !filepath.IsLocal(relativePath) {
		return Artifact{}, errors.New("artifact path escaped stage")
	}
	return Artifact{
		Variant: variant,
		Path:    filepath.ToSlash(relativePath),
		SHA256:  hashBytes(payload),
		Size:    int64(len(payload)),
		MIME:    MIME,
		Width:   width,
		Height:  height,
	}, nil
}

func runDirectoryName(version int, seed int64, count int) string {
	return "v" + strconv.Itoa(version) + "-seed-" + strconv.FormatInt(seed, 10) +
		"-count-" + strconv.Itoa(count)
}

func extensionForFormat(format string) string {
	if format == "jpeg" {
		return "jpg"
	}
	return format
}

func hashBytes(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func ensureOutputRoot(path string) error {
	if err := os.MkdirAll(path, 0o750); err != nil {
		return fmt.Errorf("creating fixture output root: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("examining fixture output root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("fixture output root is not a real directory")
	}
	return nil
}

func writeAtomic(path string, payload []byte) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("target exists and is not a regular file")
		}
		existing, err := readBoundedRegularFile(path, maxOutputFileBytes)
		if err != nil {
			return err
		}
		if hashBytes(existing) == hashBytes(payload) {
			return nil
		}
		return errors.New("target exists with different contents")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".fixture-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}

func readBoundedRegularFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("path is not a regular file")
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("file size %d exceeds limit %d", info.Size(), limit)
	}

	file, err := os.Open(path) // #nosec G304 -- callers provide generator-owned, validated fixture paths.
	if err != nil {
		return nil, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("file exceeds limit %d", limit)
	}
	return payload, nil
}

func removeStage(root string, stage string) {
	relative, err := filepath.Rel(root, stage)
	if err != nil || !filepath.IsLocal(relative) || filepath.Dir(relative) != "." {
		return
	}
	if filepath.Base(relative) == relative && len(relative) > len(".stage-") {
		_ = os.RemoveAll(stage)
	}
}
