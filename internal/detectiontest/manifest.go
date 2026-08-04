// Package detectiontest builds deterministic card-image fixtures for detection tests.
package detectiontest

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const (
	// SupportedGameCount is the number of TCGs represented in every fixture run.
	SupportedGameCount       = 6
	expectedGameCount        = SupportedGameCount
	minimumCandidatesPerGame = 2
)

var expectedGameSlugs = []struct {
	name string
	slug string
}{
	{name: "Pokemon", slug: "pokemon"},
	{name: "Magic", slug: "magic"},
	{name: "One Piece", slug: "one-piece"},
	{name: "Lorcana", slug: "lorcana"},
	{name: "Weiss Schwarz", slug: "weiss-schwarz"},
	{name: "Yu-Gi-Oh", slug: "yu-gi-oh"},
}

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

//go:embed manifest.json
var manifestJSON []byte

// Manifest describes the immutable source fixture set.
type Manifest struct {
	Version     int    `json:"version"`
	DefaultSeed int64  `json:"default_seed"`
	Cards       []Card `json:"cards"`
}

// Card is the source metadata and integrity contract for one fixture image.
type Card struct {
	Game            string `json:"game"`
	GameSlug        string `json:"game_slug"`
	FileSlug        string `json:"file_slug"`
	Source          string `json:"source"`
	SourceID        string `json:"source_id"`
	Name            string `json:"name"`
	SetID           string `json:"set_id"`
	SetName         string `json:"set_name"`
	CollectorNumber string `json:"collector_number"`
	Language        string `json:"language"`
	SourceURL       string `json:"source_url"`
	ImageSource     string `json:"image_source"`
	ImageURL        string `json:"image_url"`
	ImageSHA256     string `json:"image_sha256"`
	ImageSize       int64  `json:"image_size"`
	ImageMIME       string `json:"image_mime"`
	ImageFormat     string `json:"image_format"`
}

// LoadManifest parses and validates the checked-in source manifest.
func LoadManifest() (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decoding fixture manifest: %w", err)
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	manifest.Cards = slices.Clone(manifest.Cards)
	return manifest, nil
}

// ManifestSHA256 returns the digest of the embedded manifest with normalized line endings.
func ManifestSHA256() string {
	normalized := bytes.ReplaceAll(manifestJSON, []byte("\r\n"), []byte("\n"))
	digest := sha256.Sum256(normalized)
	return hex.EncodeToString(digest[:])
}

// ValidateManifest checks the fixture schema and its uniqueness invariants.
func ValidateManifest(manifest Manifest) error {
	if manifest.Version < 1 {
		return errors.New("fixture manifest: version must be positive")
	}
	if len(manifest.Cards) < expectedGameCount*minimumCandidatesPerGame {
		return fmt.Errorf(
			"fixture manifest: got %d cards, want at least %d",
			len(manifest.Cards),
			expectedGameCount*minimumCandidatesPerGame,
		)
	}

	gameSlugs := make(map[string]string, len(expectedGameSlugs))
	gameCounts := make(map[string]int, len(expectedGameSlugs))
	for _, game := range expectedGameSlugs {
		gameSlugs[game.name] = game.slug
	}
	IDs := make(map[string]struct{}, len(manifest.Cards))
	paths := make(map[string]struct{}, len(manifest.Cards))
	for _, card := range manifest.Cards {
		if err := validateCard(card); err != nil {
			return fmt.Errorf("fixture manifest: %s: %w", card.Game, err)
		}
		expectedSlug, exists := gameSlugs[card.Game]
		if !exists {
			return fmt.Errorf("fixture manifest: unsupported game %q", card.Game)
		}
		if card.GameSlug != expectedSlug {
			return fmt.Errorf(
				"fixture manifest: game %q requires slug %q, got %q",
				card.Game,
				expectedSlug,
				card.GameSlug,
			)
		}
		gameCounts[card.Game]++

		key := card.Source + "\x00" + card.SourceID
		if _, exists := IDs[key]; exists {
			return fmt.Errorf("fixture manifest: duplicate source id %q", card.SourceID)
		}
		IDs[key] = struct{}{}

		pathKey := card.GameSlug + "/" + card.FileSlug
		if _, exists := paths[pathKey]; exists {
			return fmt.Errorf("fixture manifest: duplicate path %q", pathKey)
		}
		paths[pathKey] = struct{}{}
	}
	for _, game := range expectedGameSlugs {
		if gameCounts[game.name] < minimumCandidatesPerGame {
			return fmt.Errorf(
				"fixture manifest: game %q has %d candidates, want at least %d",
				game.name,
				gameCounts[game.name],
				minimumCandidatesPerGame,
			)
		}
	}
	return nil
}

func validateCard(card Card) error {
	required := []struct {
		name  string
		value string
	}{
		{name: "game", value: card.Game},
		{name: "source", value: card.Source},
		{name: "source id", value: card.SourceID},
		{name: "name", value: card.Name},
		{name: "set id", value: card.SetID},
		{name: "set name", value: card.SetName},
		{name: "collector number", value: card.CollectorNumber},
		{name: "language", value: card.Language},
		{name: "image source", value: card.ImageSource},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s must not be empty", field.name)
		}
	}

	if !slugPattern.MatchString(card.GameSlug) {
		return fmt.Errorf("invalid game slug %q", card.GameSlug)
	}
	if !slugPattern.MatchString(card.FileSlug) {
		return fmt.Errorf("invalid file slug %q", card.FileSlug)
	}
	if err := validateHTTPSURL(card.SourceURL); err != nil {
		return fmt.Errorf("invalid source url: %w", err)
	}
	if err := validateHTTPSURL(card.ImageURL); err != nil {
		return fmt.Errorf("invalid image url: %w", err)
	}
	if card.ImageSize <= 0 {
		return errors.New("image size must be positive")
	}
	decodedHash, err := hex.DecodeString(card.ImageSHA256)
	if err != nil || len(decodedHash) != sha256.Size {
		return errors.New("image sha256 must be 64 hexadecimal characters")
	}

	formatMIME := map[string]string{
		"jpeg": "image/jpeg",
		"png":  "image/png",
		"webp": "image/webp",
	}
	expectedMIME, ok := formatMIME[card.ImageFormat]
	if !ok {
		return fmt.Errorf("unsupported image format %q", card.ImageFormat)
	}
	if card.ImageMIME != expectedMIME {
		return fmt.Errorf(
			"image format %q requires mime %q, got %q",
			card.ImageFormat,
			expectedMIME,
			card.ImageMIME,
		)
	}
	return nil
}

func validateHTTPSURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("url must use https and include a host")
	}
	if parsed.User != nil {
		return errors.New("url must not contain user information")
	}
	return nil
}

// Select returns one stable seeded candidate per supported game without
// mutating cards. It then shuffles the six selected games so callers do not
// accidentally depend on manifest order.
func Select(cards []Card, seed int64, count int) ([]Card, error) {
	if count != expectedGameCount {
		return nil, fmt.Errorf("selecting fixtures: count %d, want %d", count, expectedGameCount)
	}

	grouped := make(map[string][]Card, expectedGameCount)
	for _, card := range cards {
		grouped[card.GameSlug] = append(grouped[card.GameSlug], card)
	}
	random := splitMix64(uint64(seed))
	selected := make([]Card, 0, expectedGameCount)
	for _, game := range expectedGameSlugs {
		candidates := slices.Clone(grouped[game.slug])
		if len(candidates) == 0 {
			return nil, fmt.Errorf("selecting fixtures: game %q has no candidates", game.name)
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].Source != candidates[j].Source {
				return candidates[i].Source < candidates[j].Source
			}
			return candidates[i].SourceID < candidates[j].SourceID
		})
		selected = append(selected, candidates[random.intn(len(candidates))])
	}
	for i := len(selected) - 1; i > 0; i-- {
		j := random.intn(i + 1)
		selected[i], selected[j] = selected[j], selected[i]
	}
	return slices.Clone(selected), nil
}

type splitMix64 uint64

func (s *splitMix64) next() uint64 {
	*s += 0x9e3779b97f4a7c15
	z := uint64(*s)
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func (s *splitMix64) intn(n int) int {
	if n <= 0 {
		panic("splitmix64: non-positive bound")
	}
	bound := uint64(n)
	limit := ^uint64(0) - (^uint64(0) % bound)
	value := s.next()
	for value >= limit {
		value = s.next()
	}
	return int(value % bound)
}
