package source

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"pokget/internal/catalog"
)

type TCGdexProvider struct {
	HTTP     HTTPOptions
	BaseURL  string
	Language string
}

func (p *TCGdexProvider) ID() string         { return "tcgdex" }
func (p *TCGdexProvider) Game() catalog.Game { return catalog.GamePokemon }

type tcgdexSetBrief struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type tcgdexSet struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	ReleaseDate string       `json:"releaseDate"`
	Cards       []tcgdexCard `json:"cards"`
}

type tcgdexCard struct {
	ID      string `json:"id"`
	LocalID string `json:"localId"`
	Name    string `json:"name"`
	Image   string `json:"image"`
}

func (p *TCGdexProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.tcgdex.net/v2"
	}
	language := p.Language
	if language == "" {
		language = "en"
	}

	var sets []tcgdexSetBrief
	meta, err := getJSON(ctx, p.HTTP, fmt.Sprintf("%s/%s/sets", baseURL, url.PathEscape(language)), request, &sets)
	if err != nil {
		return catalog.FetchResult{}, err
	}
	result := catalog.FetchResult{ETag: meta.ETag, LastModified: meta.LastModified, CompleteSnapshot: true, NotModified: meta.NotModified}
	if meta.NotModified {
		return result, nil
	}

	for index, brief := range sets {
		if index > 0 {
			if err := waitRequestDelay(ctx, p.HTTP.RequestDelay); err != nil {
				return result, err
			}
		}
		var set tcgdexSet
		endpoint := fmt.Sprintf("%s/%s/sets/%s", baseURL, url.PathEscape(language), url.PathEscape(brief.ID))
		if _, err := getJSON(ctx, p.HTTP, endpoint, catalog.FetchRequest{}, &set); err != nil {
			return result, err
		}
		for _, card := range set.Cards {
			record := catalog.CardRecord{
				SourceCardID:    card.ID,
				Name:            card.Name,
				SetCode:         set.ID,
				SetName:         set.Name,
				CollectorNumber: card.LocalID,
				Language:        language,
				ReleasedAt:      parseDate(set.ReleaseDate),
				Metadata:        rawMetadata(card),
			}
			if card.Image != "" {
				record.Images = []catalog.ImageRecord{{SourceImageID: card.ID + ":front", Kind: "front", URL: card.Image + "/high.webp"}}
			}
			record.Printings = []catalog.PrintingRecord{{
				SourcePrintingID: card.ID,
				SetCode:          set.ID,
				SetName:          set.Name,
				CollectorNumber:  card.LocalID,
				Language:         language,
				ReleasedAt:       record.ReleasedAt,
				SourceImageIDs:   imageIDs(record.Images),
			}}
			if err := record.Validate(); err != nil {
				return result, err
			}
			if err := emit(record); err != nil {
				return result, err
			}
			result.Count++
		}
	}
	return result, nil
}

func imageIDs(images []catalog.ImageRecord) []string {
	ids := make([]string, 0, len(images))
	for _, image := range images {
		ids = append(ids, image.SourceImageID)
	}
	return ids
}
