package source

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"pokget/internal/catalog"
)

type SWUDBProvider struct {
	HTTP    HTTPOptions
	BaseURL string
}

func (p *SWUDBProvider) ID() string         { return "swudb" }
func (p *SWUDBProvider) Game() catalog.Game { return catalog.GameStarWarsUnlimited }

type swuSetBrief struct {
	SetID       string `json:"setId"`
	FullName    string `json:"fullName"`
	ReleaseDate string `json:"releaseDate"`
	NumberCards int    `json:"numberCards"`
}

type swuCard struct {
	Set         string `json:"Set"`
	Number      string `json:"Number"`
	Name        string `json:"Name"`
	Type        string `json:"Type"`
	Rarity      string `json:"Rarity"`
	FrontArt    string `json:"FrontArt"`
	VariantType string `json:"VariantType"`
	CID         string `json:"cid"`
	MarketPrice string `json:"MarketPrice"`
	LowPrice    string `json:"LowPrice"`
}

type swuCardsResponse struct {
	Data []swuCard `json:"data"`
}

func parseSWUDate(value string) *time.Time {
	if value == "" {
		return nil
	}
	if parsed := parseDate(value); parsed != nil {
		return parsed
	}
	for _, layout := range []string{"1/2/06", "01/02/06", "1/2/2006", "01/02/2006"} {
		if t, err := time.Parse(layout, value); err == nil {
			return &t
		}
	}
	return nil
}

func (p *SWUDBProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.swu-db.com"
	}

	var sets []swuSetBrief
	meta, err := getJSON(ctx, p.HTTP, baseURL+"/sets", request, &sets)
	if err != nil {
		return catalog.FetchResult{}, err
	}
	result := catalog.FetchResult{ETag: meta.ETag, LastModified: meta.LastModified, CompleteSnapshot: true, NotModified: meta.NotModified}
	if meta.NotModified {
		return result, nil
	}

	for index, set := range sets {
		if index > 0 {
			if err := waitRequestDelay(ctx, p.HTTP.RequestDelay); err != nil {
				return result, err
			}
		}
		var response swuCardsResponse
		endpoint := fmt.Sprintf("%s/cards/%s", baseURL, set.SetID)
		if _, err := getJSON(ctx, p.HTTP, endpoint, catalog.FetchRequest{}, &response); err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			slog.Warn("Catalog: skipping swudb set due to fetch error", "set", set.SetID, "endpoint", endpoint, "error", err)
			continue
		}

		for _, card := range response.Data {
			if card.Name == "" || card.Number == "" {
				continue
			}
			sourceCardID := fmt.Sprintf("%s-%s", card.Set, card.Number)
			if card.CID != "" {
				sourceCardID = card.CID
			}
			record := catalog.CardRecord{
				SourceCardID:    sourceCardID,
				Name:            card.Name,
				SetCode:         card.Set,
				SetName:         set.FullName,
				CollectorNumber: card.Number,
				Rarity:          card.Rarity,
				Language:        "en",
				ReleasedAt:      parseSWUDate(set.ReleaseDate),
				Metadata:        rawMetadata(card),
			}
			if card.FrontArt != "" {
				record.Images = []catalog.ImageRecord{{
					SourceImageID: sourceCardID + ":front",
					Kind:          "front",
					URL:           card.FrontArt,
				}}
			}
			variant := card.VariantType
			if variant == "" {
				variant = "Normal"
			}
			record.Printings = []catalog.PrintingRecord{{
				SourcePrintingID: sourceCardID,
				SetCode:          card.Set,
				SetName:          set.FullName,
				CollectorNumber:  card.Number,
				Rarity:           card.Rarity,
				Language:         "en",
				Variant:          variant,
				ReleasedAt:       record.ReleasedAt,
				SourceImageIDs:   imageIDs(record.Images),
				Metadata:         rawMetadata(card),
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
