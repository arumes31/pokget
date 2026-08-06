package detectiontest

import (
	"slices"
	"testing"
)

func TestLoadManifest(t *testing.T) {
	t.Parallel()

	manifest, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if manifest.Version != 2 {
		t.Errorf("Version = %d, want 2", manifest.Version)
	}
	if manifest.DefaultSeed != 20260804 {
		t.Errorf("DefaultSeed = %d, want 20260804", manifest.DefaultSeed)
	}
	const candidatesPerGame = 4
	if len(manifest.Cards) != expectedGameCount*candidatesPerGame {
		t.Fatalf("len(Cards) = %d, want %d", len(manifest.Cards), expectedGameCount*candidatesPerGame)
	}

	gameCounts := make(map[string]int, expectedGameCount)
	for _, card := range manifest.Cards {
		gameCounts[card.Game]++
	}
	expectedGames := []string{
		"Pokemon",
		"Magic",
		"One Piece",
		"Lorcana",
		"Weiss Schwarz",
		"Yu-Gi-Oh",
	}
	for _, game := range expectedGames {
		if gameCounts[game] != candidatesPerGame {
			t.Errorf("gameCounts[%q] = %d, want %d", game, gameCounts[game], candidatesPerGame)
		}
	}

	manifest.Cards[0].Name = "mutated"
	reloaded, err := LoadManifest()
	if err != nil {
		t.Fatalf("second LoadManifest() error = %v", err)
	}
	if reloaded.Cards[0].Name == "mutated" {
		t.Error("LoadManifest returned mutable shared storage")
	}
}

func TestValidateManifest(t *testing.T) {
	t.Parallel()

	valid, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{
			name: "missing game",
			mutate: func(manifest *Manifest) {
				cards := manifest.Cards[:0]
				for _, card := range manifest.Cards {
					if card.Game != "Yu-Gi-Oh" {
						cards = append(cards, card)
					}
				}
				manifest.Cards = cards
			},
		},
		{
			name: "duplicate game",
			mutate: func(manifest *Manifest) {
				manifest.Cards[1].Game = manifest.Cards[0].Game
			},
		},
		{
			name: "unsafe slug",
			mutate: func(manifest *Manifest) {
				manifest.Cards[0].FileSlug = "../escape"
			},
		},
		{
			name: "insecure url",
			mutate: func(manifest *Manifest) {
				manifest.Cards[0].ImageURL = "http://example.test/card.png"
			},
		},
		{
			name: "invalid hash",
			mutate: func(manifest *Manifest) {
				manifest.Cards[0].ImageSHA256 = "invalid"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := valid
			manifest.Cards = slices.Clone(valid.Cards)
			test.mutate(&manifest)
			if err := ValidateManifest(manifest); err == nil {
				t.Error("ValidateManifest() error = nil, want error")
			}
		})
	}
}

func TestSelect(t *testing.T) {
	t.Parallel()

	manifest, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	original := slices.Clone(manifest.Cards)

	first, err := Select(manifest.Cards, manifest.DefaultSeed, expectedGameCount)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	second, err := Select(manifest.Cards, manifest.DefaultSeed, expectedGameCount)
	if err != nil {
		t.Fatalf("second Select() error = %v", err)
	}
	if !slices.Equal(first, second) {
		t.Errorf("same-seed selections differ: %#v != %#v", first, second)
	}
	selectedGames := make(map[string]struct{}, expectedGameCount)
	for _, card := range first {
		selectedGames[card.GameSlug] = struct{}{}
	}
	if len(selectedGames) != expectedGameCount {
		t.Errorf("selected %d unique games, want %d", len(selectedGames), expectedGameCount)
	}
	if !slices.Equal(manifest.Cards, original) {
		t.Error("Select mutated its input")
	}

	different, err := Select(manifest.Cards, 42, expectedGameCount)
	if err != nil {
		t.Fatalf("different-seed Select() error = %v", err)
	}
	if slices.Equal(first, different) {
		t.Errorf("different seeds returned the same selection: %#v", first)
	}
}

func TestSelectRejectsInvalidCount(t *testing.T) {
	t.Parallel()

	manifest, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	for _, count := range []int{-1, 0, expectedGameCount - 1, expectedGameCount + 1, len(manifest.Cards) + 1} {
		if _, err := Select(manifest.Cards, 1, count); err == nil {
			t.Errorf("Select(count=%d) error = nil, want error", count)
		}
	}
}
