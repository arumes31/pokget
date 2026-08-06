package source

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"pokget/internal/catalog"
)

type LorcastProvider struct {
	HTTP    HTTPOptions
	BaseURL string
}

func (p *LorcastProvider) ID() string         { return "lorcast" }
func (p *LorcastProvider) Game() catalog.Game { return catalog.GameLorcana }

type lorcastSetList struct {
	Results []lorcastSet `json:"results"`
}

type lorcastSet struct {
	ID         string `json:"id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	ReleasedAt string `json:"released_at"`
}

type lorcastCard struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Version         string     `json:"version"`
	ReleasedAt      string     `json:"released_at"`
	CollectorNumber string     `json:"collector_number"`
	Language        string     `json:"lang"`
	Rarity          string     `json:"rarity"`
	Set             lorcastSet `json:"set"`
	ImageURIs       struct {
		Digital struct {
			Normal string `json:"normal"`
		} `json:"digital"`
	} `json:"image_uris"`
}

func (p *LorcastProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.lorcast.com/v0"
	}
	var list lorcastSetList
	meta, err := getJSON(ctx, p.HTTP, baseURL+"/sets", request, &list)
	if err != nil {
		return catalog.FetchResult{}, err
	}
	result := catalog.FetchResult{ETag: meta.ETag, LastModified: meta.LastModified, CompleteSnapshot: true, NotModified: meta.NotModified}
	if meta.NotModified {
		return result, nil
	}

	for index, set := range list.Results {
		if index > 0 {
			if err := waitRequestDelay(ctx, p.HTTP.RequestDelay); err != nil {
				return result, err
			}
		}
		var cards []lorcastCard
		endpoint := fmt.Sprintf("%s/sets/%s/cards", baseURL, url.PathEscape(set.Code))
		if _, err := getJSON(ctx, p.HTTP, endpoint, catalog.FetchRequest{}, &cards); err != nil {
			return result, err
		}
		for _, card := range cards {
			language := card.Language
			if language == "" {
				language = "en"
			}
			record := catalog.CardRecord{
				SourceCardID:    card.ID,
				Name:            strings.TrimSpace(card.Name + " " + card.Version),
				SetCode:         card.Set.Code,
				SetName:         card.Set.Name,
				CollectorNumber: card.CollectorNumber,
				Language:        language,
				Rarity:          card.Rarity,
				ReleasedAt:      parseDate(card.ReleasedAt),
				Metadata:        rawMetadata(card),
			}
			if record.SetCode == "" {
				record.SetCode = set.Code
			}
			if record.SetName == "" {
				record.SetName = set.Name
			}
			if card.ImageURIs.Digital.Normal != "" {
				record.Images = []catalog.ImageRecord{{SourceImageID: card.ID + ":front", Kind: "front", URL: card.ImageURIs.Digital.Normal}}
			}
			record.Printings = []catalog.PrintingRecord{{
				SourcePrintingID: card.ID,
				SetCode:          record.SetCode,
				SetName:          record.SetName,
				CollectorNumber:  record.CollectorNumber,
				Language:         language,
				Rarity:           card.Rarity,
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
