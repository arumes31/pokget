package main

import (
	"testing"

	"pokget/internal/catalog"
	"pokget/internal/detectiontest"
)

func TestIdentityForAllAcceptanceGames(t *testing.T) {
	t.Parallel()

	manifest, err := detectiontest.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[catalog.Game]bool)
	for _, card := range manifest.Cards {
		identity, err := identityFor(card)
		if err != nil {
			t.Fatalf("identityFor(%q): %v", card.Game, err)
		}
		if !identity.Game.Valid() || identity.SourceID == "" || identity.SourceCardID == "" {
			t.Fatalf("identityFor(%q) = %+v", card.Game, identity)
		}
		if card.Source == "LorcanaJSON" && identity.SourceCardID != card.SourceID {
			t.Fatalf("identityFor(%q).SourceCardID = %q, want %q", card.Name, identity.SourceCardID, card.SourceID)
		}
		seen[identity.Game] = true
	}
	if len(seen) != 6 {
		t.Fatalf("covered games = %d, want 6", len(seen))
	}
}
