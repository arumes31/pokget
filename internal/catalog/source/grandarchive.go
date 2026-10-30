package source

import (
	"context"
	"fmt"
	"strings"

	"pokget/internal/catalog"
)

type GATCGProvider struct {
	HTTP    HTTPOptions
	BaseURL string
}

func (p *GATCGProvider) ID() string         { return "gatcg" }
func (p *GATCGProvider) Game() catalog.Game { return catalog.GameGrandArchive }

type gaSearchResponse struct {
	Page       int       `json:"page"`
	TotalPages int       `json:"total_pages"`
	HasMore    bool      `json:"has_more"`
	Data       []gaCard  `json:"data"`
}

type gaCard struct {
	UUID     string      `json:"uuid"`
	Name     string      `json:"name"`
	Slug     string      `json:"slug"`
	Editions []gaEdition `json:"editions"`
}

type gaEdition struct {
	UUID            string `json:"uuid"`
	CardID          string `json:"card_id"`
	CollectorNumber string `json:"collector_number"`
	Image           string `json:"image"`
	Rarity          int    `json:"rarity"`
	Set             gaSet  `json:"set"`
}

type gaSet struct {
	ID       string `json:"id"`
	Prefix   string `json:"prefix"`
	Name     string `json:"name"`
	Language string `json:"language"`
}

func mapGARarity(rarity int) string {
	switch rarity {
	case 1:
		return "Common"
	case 2:
		return "Uncommon"
	case 3:
		return "Rare"
	case 4:
		return "Super Rare"
	case 5:
		return "Ultra Rare"
	case 6:
		return "Collector Super Rare"
	case 7:
		return "Collector Ultra Rare"
	case 8:
		return "Collector Promo Rare"
	default:
		if rarity > 0 {
			return fmt.Sprintf("Rarity %d", rarity)
		}
		return "Normal"
	}
}

func (p *GATCGProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.gatcg.com"
	}

	result := catalog.FetchResult{CompleteSnapshot: true}

	page := 1
	for {
		if page > 1 {
			if err := waitRequestDelay(ctx, p.HTTP.RequestDelay); err != nil {
				return result, err
			}
		}

		endpoint := fmt.Sprintf("%s/cards/search?page=%d", baseURL, page)
		var response gaSearchResponse
		meta, err := getJSON(ctx, p.HTTP, endpoint, catalog.FetchRequest{}, &response)
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, fmt.Errorf("gatcg: fetch page %d: %w", page, err)
		}

		if page == 1 {
			result.ETag = meta.ETag
			result.LastModified = meta.LastModified
		}

		for _, card := range response.Data {
			if card.Name == "" {
				continue
			}

			if len(card.Editions) == 0 {
				record := catalog.CardRecord{
					SourceCardID: card.UUID,
					Name:         card.Name,
					SetName:      "Uncatalogued",
					Language:     "en",
					Metadata:     rawMetadata(card),
				}
				if err := record.Validate(); err != nil {
					return result, err
				}
				if err := emit(record); err != nil {
					return result, err
				}
				result.Count++
				continue
			}

			for _, edition := range card.Editions {
				editionID := edition.UUID
				if editionID == "" {
					editionID = fmt.Sprintf("%s-%s-%s", card.UUID, edition.Set.Prefix, edition.CollectorNumber)
				}

				setName := edition.Set.Name
				if setName == "" {
					setName = edition.Set.Prefix
				}
				if setName == "" {
					setName = "Grand Archive TCG"
				}

				lang := strings.ToLower(strings.TrimSpace(edition.Set.Language))
				if lang == "" {
					lang = "en"
				}

				rarity := mapGARarity(edition.Rarity)

				record := catalog.CardRecord{
					SourceCardID:    editionID,
					Name:            card.Name,
					SetCode:         edition.Set.Prefix,
					SetName:         setName,
					CollectorNumber: edition.CollectorNumber,
					Language:        lang,
					Rarity:          rarity,
					Variant:         "Normal",
					Metadata:        rawMetadata(card),
				}

				if edition.Image != "" {
					imageURL := edition.Image
					if !strings.HasPrefix(imageURL, "http://") && !strings.HasPrefix(imageURL, "https://") {
						imageURL = baseURL + "/" + strings.TrimLeft(imageURL, "/")
					}
					record.Images = []catalog.ImageRecord{{
						SourceImageID: editionID + ":front",
						Kind:          "front",
						URL:           imageURL,
					}}
				}

				record.Printings = []catalog.PrintingRecord{{
					SourcePrintingID: editionID,
					SetCode:          edition.Set.Prefix,
					SetName:          setName,
					CollectorNumber:  edition.CollectorNumber,
					Rarity:           rarity,
					Language:         lang,
					Variant:          "Normal",
					SourceImageIDs:   imageIDs(record.Images),
					Metadata:         rawMetadata(edition),
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

		if !response.HasMore || (response.TotalPages > 0 && page >= response.TotalPages) || len(response.Data) == 0 {
			break
		}
		page++
	}

	return result, nil
}
