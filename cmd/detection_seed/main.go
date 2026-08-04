// Command detection_seed installs the six immutable acceptance references into
// a catalog database when a full source backfill has not reached them yet.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"pokget/internal/catalog"
	"pokget/internal/db"
	"pokget/internal/detectiontest"
)

type identity struct {
	SourceID     string
	SourceCardID string
	Game         catalog.Game
}

type seededCard struct {
	Game     string `json:"game"`
	Source   string `json:"source"`
	CardID   string `json:"card_id"`
	Name     string `json:"name"`
	Inserted bool   `json:"inserted"`
}

func main() {
	if err := run(context.Background(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "detection reference seed failed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, output *os.File) error {
	database, err := db.Connect()
	if err != nil {
		return err
	}
	defer database.Close()
	migrations, err := filepath.Abs("migrations")
	if err != nil {
		return err
	}
	if err := db.ApplyMigrations(database, migrations); err != nil {
		return err
	}
	repository, err := catalog.NewPostgresRepository(database)
	if err != nil {
		return err
	}
	manifest, err := detectiontest.LoadManifest()
	if err != nil {
		return err
	}

	results := make([]seededCard, 0, len(manifest.Cards))
	for _, fixture := range manifest.Cards {
		key, err := identityFor(fixture)
		if err != nil {
			return err
		}
		cardID, found, err := findCard(ctx, database, key, fixture)
		if err != nil {
			return err
		}
		inserted := false
		if !found {
			cardID, err = upsertFixture(ctx, repository, key, fixture)
			if err != nil {
				return err
			}
			inserted = true
		}
		results = append(results, seededCard{
			Game: fixture.Game, Source: key.SourceID, CardID: cardID, Name: fixture.Name, Inserted: inserted,
		})
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(results)
}

func identityFor(card detectiontest.Card) (identity, error) {
	identities := map[string]identity{
		"pokemon":       {SourceID: "tcgdex", Game: catalog.GamePokemon},
		"magic":         {SourceID: "scryfall", Game: catalog.GameMagic},
		"one-piece":     {SourceID: "onepiece_official", Game: catalog.GameOnePiece},
		"lorcana":       {SourceID: "lorcanajson", SourceCardID: card.CollectorNumber, Game: catalog.GameLorcana},
		"weiss-schwarz": {SourceID: "weiss_official", Game: catalog.GameWeissSchwarz},
		"yu-gi-oh":      {SourceID: "ygoprodeck", Game: catalog.GameYuGiOh},
	}
	result, ok := identities[card.GameSlug]
	if !ok {
		return identity{}, fmt.Errorf("unsupported fixture game %q", card.GameSlug)
	}
	if card.GameSlug == "lorcana" && card.Source == "LorcanaJSON" {
		result.SourceCardID = card.SourceID
	} else if result.SourceCardID == "" {
		result.SourceCardID = card.SourceID
	}
	return result, nil
}

func findCard(ctx context.Context, database *sql.DB, key identity, card detectiontest.Card) (string, bool, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT id
		FROM cards
		WHERE source_id = $1 AND language = $2 AND name = $3
		  AND source_card_id = $4
		  AND catalog_active
		ORDER BY id`, key.SourceID, card.Language, card.Name, key.SourceCardID)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", false, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	if len(ids) > 1 {
		return "", false, fmt.Errorf("fixture %q matched %d catalog cards", card.Name, len(ids))
	}
	if len(ids) == 1 {
		return ids[0], true, nil
	}
	return "", false, nil
}

func upsertFixture(ctx context.Context, repository *catalog.PostgresRepository, key identity, card detectiontest.Card) (string, error) {
	runID, err := repository.BeginRun(ctx, key.SourceID, catalog.SyncModeIncremental)
	if err != nil {
		return "", err
	}
	record := catalog.CardRecord{
		SourceCardID:    key.SourceCardID,
		Name:            card.Name,
		SetCode:         card.SetID,
		SetName:         card.SetName,
		CollectorNumber: card.CollectorNumber,
		Language:        card.Language,
		Images: []catalog.ImageRecord{{
			SourceImageID: key.SourceCardID + ":front",
			Kind:          "front",
			URL:           card.ImageURL,
		}},
		Printings: []catalog.PrintingRecord{{
			SourcePrintingID: key.SourceCardID,
			SetCode:          card.SetID,
			SetName:          card.SetName,
			CollectorNumber:  card.CollectorNumber,
			Language:         card.Language,
			SourceImageIDs:   []string{key.SourceCardID + ":front"},
		}},
		Metadata: json.RawMessage(`{"acceptance_fixture":true}`),
	}
	changes, err := repository.UpsertBatch(ctx, catalog.Batch{
		RunID: runID, SourceID: key.SourceID, Game: key.Game, Records: []catalog.CardRecord{record},
	})
	if err != nil {
		_ = repository.FailRun(context.WithoutCancel(ctx), runID, err)
		return "", err
	}
	_, err = repository.CompleteRun(ctx, runID, catalog.Completion{
		Fetch: catalog.FetchResult{Count: 1, CompleteSnapshot: false}, Changes: changes,
	})
	if err != nil {
		_ = repository.FailRun(context.WithoutCancel(ctx), runID, err)
		return "", err
	}
	return catalog.CardID(key.SourceID, key.SourceCardID, card.Language)
}
