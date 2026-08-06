package source

import (
	"context"
	"fmt"
	"strings"

	"pokget/internal/catalog"
)

type OPTCGAPIProvider struct {
	HTTP      HTTPOptions
	BaseURL   string
	Endpoints []string
}

func (p *OPTCGAPIProvider) ID() string         { return "optcgapi" }
func (p *OPTCGAPIProvider) Game() catalog.Game { return catalog.GameOnePiece }

type optcgCard struct {
	CardName    string `json:"card_name"`
	SetName     string `json:"set_name"`
	SetID       string `json:"set_id"`
	Rarity      string `json:"rarity"`
	CardSetID   string `json:"card_set_id"`
	CardImageID string `json:"card_image_id"`
	CardImage   string `json:"card_image"`
	DateScraped string `json:"date_scraped"`
	DonName     string `json:"optcg_don_name"`
}

func (p *OPTCGAPIProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://optcgapi.com/api"
	}
	endpoints := p.Endpoints
	if len(endpoints) == 0 {
		endpoints = []string{"allSetCards", "allSTCards", "allPromoCards", "allDonCards"}
	}
	result := catalog.FetchResult{CompleteSnapshot: true}
	seen := make(map[string]struct{})
	for index, endpoint := range endpoints {
		if index > 0 {
			if err := waitRequestDelay(ctx, p.HTTP.RequestDelay); err != nil {
				return result, err
			}
		}
		var cards []optcgCard
		fetch := catalog.FetchRequest{}
		if index == 0 {
			fetch = request
		}
		meta, err := getJSON(ctx, p.HTTP, fmt.Sprintf("%s/%s/", baseURL, endpoint), fetch, &cards)
		if err != nil {
			return result, fmt.Errorf("optcgapi %s: %w", endpoint, err)
		}
		if index == 0 {
			result.ETag = meta.ETag
			result.LastModified = meta.LastModified
			if meta.NotModified {
				result.NotModified = true
				return result, nil
			}
		}
		for cardIndex, card := range cards {
			sourceID := card.CardImageID
			if sourceID == "" {
				sourceID = card.CardSetID
			}
			if sourceID == "" {
				sourceID = endpoint + ":" + fmt.Sprint(cardIndex)
			}
			if _, exists := seen[sourceID]; exists {
				continue
			}
			seen[sourceID] = struct{}{}
			name := card.CardName
			if name == "" {
				name = card.DonName
			}
			setName := card.SetName
			if setName == "" {
				setName = endpointSetName(endpoint)
			}
			record := catalog.CardRecord{
				SourceCardID:    sourceID,
				Name:            name,
				SetCode:         card.SetID,
				SetName:         setName,
				CollectorNumber: card.CardSetID,
				Language:        "en",
				Rarity:          card.Rarity,
				SourceUpdatedAt: parseDate(card.DateScraped),
				Metadata:        rawMetadata(card),
			}
			if record.SetCode == "" {
				record.SetCode = endpoint
			}
			if record.CollectorNumber == "" {
				record.CollectorNumber = sourceID
			}
			if card.CardImage != "" {
				record.Images = []catalog.ImageRecord{{SourceImageID: sourceID + ":front", Kind: "front", URL: card.CardImage}}
			}
			record.Printings = []catalog.PrintingRecord{{
				SourcePrintingID: endpoint + ":" + sourceID,
				SetCode:          record.SetCode,
				SetName:          record.SetName,
				CollectorNumber:  record.CollectorNumber,
				Rarity:           record.Rarity,
				Language:         "en",
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

func endpointSetName(endpoint string) string {
	switch endpoint {
	case "allPromoCards":
		return "Promotion Cards"
	case "allDonCards":
		return "DON!! Cards"
	case "allSTCards":
		return "Starter Decks"
	default:
		return "Booster Sets"
	}
}
