// Package catalog defines the source-neutral card catalog ingestion domain.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Game string

const (
	GamePokemon      Game = "pokemon"
	GameMagic        Game = "magic"
	GameOnePiece     Game = "one_piece"
	GameLorcana      Game = "lorcana"
	GameWeissSchwarz Game = "weiss_schwarz"
	GameYuGiOh       Game = "yugioh"
)

func (g Game) Valid() bool {
	switch g {
	case GamePokemon, GameMagic, GameOnePiece, GameLorcana, GameWeissSchwarz, GameYuGiOh:
		return true
	default:
		return false
	}
}

type SyncMode string

const (
	SyncModeFull        SyncMode = "full"
	SyncModeIncremental SyncMode = "incremental"
)

func (m SyncMode) Valid() bool {
	return m == SyncModeFull || m == SyncModeIncremental
}

type FetchRequest struct {
	Mode            SyncMode
	Cursor          string
	ETag            string
	LastModified    string
	UpstreamVersion string
}

type FetchResult struct {
	Cursor           string
	ETag             string
	LastModified     string
	UpstreamVersion  string
	CompleteSnapshot bool
	NotModified      bool
	Count            int64
}

type SourceState struct {
	SourceID        string
	Cursor          string
	ETag            string
	LastModified    string
	UpstreamVersion string
	LastSuccessAt   *time.Time
	LastFullSyncAt  *time.Time
	LastRecordCount int64
	LastError       string
}

func (s SourceState) FetchRequest(mode SyncMode) FetchRequest {
	return FetchRequest{
		Mode:            mode,
		Cursor:          s.Cursor,
		ETag:            s.ETag,
		LastModified:    s.LastModified,
		UpstreamVersion: s.UpstreamVersion,
	}
}

type ImageRecord struct {
	SourceImageID string
	Kind          string
	URL           string
}

type PrintingRecord struct {
	SourcePrintingID string
	SetCode          string
	SetName          string
	CollectorNumber  string
	Rarity           string
	Language         string
	Variant          string
	ReleasedAt       *time.Time
	Metadata         json.RawMessage
	SourceImageIDs   []string
}

type CardRecord struct {
	SourceCardID    string
	Name            string
	SetCode         string
	SetName         string
	CollectorNumber string
	Language        string
	Variant         string
	Rarity          string
	ReleasedAt      *time.Time
	SourceUpdatedAt *time.Time
	Images          []ImageRecord
	Printings       []PrintingRecord
	Metadata        json.RawMessage
}

func (r CardRecord) Validate() error {
	switch {
	case strings.TrimSpace(r.SourceCardID) == "":
		return fmt.Errorf("catalog: source card id is required")
	case strings.TrimSpace(r.Name) == "":
		return fmt.Errorf("catalog: card name is required")
	case strings.TrimSpace(r.SetName) == "":
		return fmt.Errorf("catalog: set name is required")
	case strings.TrimSpace(r.Language) == "":
		return fmt.Errorf("catalog: card language is required")
	case len(r.Metadata) > 0 && !json.Valid(r.Metadata):
		return fmt.Errorf("catalog: card metadata is not valid json")
	}

	imageIDs := make(map[string]struct{}, len(r.Images))
	for _, image := range r.Images {
		if err := image.validate(); err != nil {
			return fmt.Errorf("catalog: validating card %q: %w", r.SourceCardID, err)
		}
		if _, exists := imageIDs[image.SourceImageID]; exists {
			return fmt.Errorf("catalog: duplicate source image id %q", image.SourceImageID)
		}
		imageIDs[image.SourceImageID] = struct{}{}
	}

	printingIDs := make(map[string]struct{}, len(r.Printings))
	for _, printing := range r.Printings {
		if err := printing.validate(r.Language, imageIDs); err != nil {
			return fmt.Errorf("catalog: validating card %q: %w", r.SourceCardID, err)
		}
		identity := printing.SourcePrintingID + "\x00" + defaultString(printing.Language, r.Language) + "\x00" + defaultVariant(printing.Variant)
		if _, exists := printingIDs[identity]; exists {
			return fmt.Errorf("catalog: duplicate source printing id %q", printing.SourcePrintingID)
		}
		printingIDs[identity] = struct{}{}
	}

	return nil
}

func (r ImageRecord) validate() error {
	switch {
	case strings.TrimSpace(r.SourceImageID) == "":
		return fmt.Errorf("source image id is required")
	case strings.TrimSpace(r.URL) == "":
		return fmt.Errorf("image url is required")
	default:
		return nil
	}
}

func (r PrintingRecord) validate(cardLanguage string, imageIDs map[string]struct{}) error {
	switch {
	case strings.TrimSpace(r.SourcePrintingID) == "":
		return fmt.Errorf("source printing id is required")
	case strings.TrimSpace(r.SetName) == "":
		return fmt.Errorf("printing set name is required")
	case strings.TrimSpace(defaultString(r.Language, cardLanguage)) == "":
		return fmt.Errorf("printing language is required")
	case len(r.Metadata) > 0 && !json.Valid(r.Metadata):
		return fmt.Errorf("printing metadata is not valid json")
	}

	for _, sourceImageID := range r.SourceImageIDs {
		if _, exists := imageIDs[sourceImageID]; !exists {
			return fmt.Errorf("printing references unknown source image id %q", sourceImageID)
		}
	}

	return nil
}

type Provider interface {
	ID() string
	Game() Game
	Fetch(context.Context, FetchRequest, func(CardRecord) error) (FetchResult, error)
}

type ChangeCounts struct {
	CardsInserted        int64
	CardsUpdated         int64
	PrintingsInserted    int64
	PrintingsUpdated     int64
	ImagesInserted       int64
	ImagesUpdated        int64
	CardsDeactivated     int64
	PrintingsDeactivated int64
}

func (c *ChangeCounts) Add(other ChangeCounts) {
	c.CardsInserted += other.CardsInserted
	c.CardsUpdated += other.CardsUpdated
	c.PrintingsInserted += other.PrintingsInserted
	c.PrintingsUpdated += other.PrintingsUpdated
	c.ImagesInserted += other.ImagesInserted
	c.ImagesUpdated += other.ImagesUpdated
	c.CardsDeactivated += other.CardsDeactivated
	c.PrintingsDeactivated += other.PrintingsDeactivated
}

type Batch struct {
	RunID    string
	SourceID string
	Game     Game
	Records  []CardRecord
}

type Completion struct {
	Fetch   FetchResult
	Changes ChangeCounts
}

type Repository interface {
	BeginRun(context.Context, string, SyncMode) (string, error)
	UpsertBatch(context.Context, Batch) (ChangeCounts, error)
	CompleteRun(context.Context, string, Completion) (ChangeCounts, error)
	FailRun(context.Context, string, error) error
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func defaultVariant(variant string) string {
	if variant == "" {
		return "Normal"
	}
	return variant
}
