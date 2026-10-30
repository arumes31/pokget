package source

import (
	"context"
	"fmt"
	"strings"

	"pokget/internal/catalog"
)

type FabCubeProvider struct {
	HTTP    HTTPOptions
	BaseURL string
}

func (p *FabCubeProvider) ID() string         { return "fabcube" }
func (p *FabCubeProvider) Game() catalog.Game { return catalog.GameFleshAndBlood }

type fabCubeCard struct {
	UniqueID            string            `json:"unique_id"`
	Name                string            `json:"name"`
	Pitch               string            `json:"pitch"`
	Cost                string            `json:"cost"`
	Power               string            `json:"power"`
	Defense             string            `json:"defense"`
	Health              string            `json:"health"`
	Intelligence        string            `json:"intelligence"`
	Types               []string          `json:"types"`
	CardKeywords        []string          `json:"card_keywords"`
	FunctionalTextPlain string            `json:"functional_text_plain"`
	TypeText            string            `json:"type_text"`
	Printings           []fabCubePrinting `json:"printings"`
}

type fabCubePrinting struct {
	UniqueID            string   `json:"unique_id"`
	SetPrintingUniqueID string   `json:"set_printing_unique_id"`
	ID                  string   `json:"id"`
	SetID               string   `json:"set_id"`
	Edition             string   `json:"edition"`
	Foiling             string   `json:"foiling"`
	Rarity              string   `json:"rarity"`
	Artists             []string `json:"artists"`
	ImageURL            string   `json:"image_url"`
	TCGPlayerProductID  string   `json:"tcgplayer_product_id"`
	TCGPlayerURL        string   `json:"tcgplayer_url"`
}

type fabCubeSet struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Printings []struct {
		InitialReleaseDate string `json:"initial_release_date"`
	} `json:"printings"`
}

func mapFABFoiling(foiling string) string {
	switch strings.ToUpper(foiling) {
	case "R":
		return "Rainbow Foil"
	case "C":
		return "Cold Foil"
	case "G":
		return "Gold Foil"
	default:
		return "Normal"
	}
}

func mapFABRarity(rarity string) string {
	switch strings.ToUpper(rarity) {
	case "T":
		return "Token"
	case "C":
		return "Common"
	case "R":
		return "Rare"
	case "S":
		return "Super Rare"
	case "M":
		return "Majestic"
	case "L":
		return "Legendary"
	case "F":
		return "Fabled"
	case "P":
		return "Promo"
	case "V":
		return "Marvel"
	default:
		return rarity
	}
}

func (p *FabCubeProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://raw.githubusercontent.com/the-fab-cube/flesh-and-blood-cards/refs/heads/main"
	}

	cardEndpoint := baseURL
	if !strings.HasSuffix(cardEndpoint, ".json") {
		if strings.HasSuffix(cardEndpoint, "/json/english") {
			cardEndpoint += "/card.json"
		} else {
			cardEndpoint += "/json/english/card.json"
		}
	}

	setNames := make(map[string]string)
	setEndpoint := baseURL
	if !strings.HasSuffix(setEndpoint, ".json") {
		if strings.HasSuffix(setEndpoint, "/json/english") {
			setEndpoint += "/set.json"
		} else {
			setEndpoint += "/json/english/set.json"
		}
		var sets []fabCubeSet
		if _, err := getJSON(ctx, p.HTTP, setEndpoint, catalog.FetchRequest{}, &sets); err == nil {
			for _, s := range sets {
				if s.ID != "" && s.Name != "" {
					setNames[s.ID] = s.Name
				}
			}
		}
	}

	var cards []fabCubeCard
	meta, err := getJSON(ctx, p.HTTP, cardEndpoint, request, &cards)
	if err != nil {
		return catalog.FetchResult{}, err
	}

	result := catalog.FetchResult{
		ETag:             meta.ETag,
		LastModified:     meta.LastModified,
		CompleteSnapshot: true,
		NotModified:      meta.NotModified,
	}
	if meta.NotModified {
		return result, nil
	}

	for _, card := range cards {
		if card.Name == "" {
			continue
		}

		if len(card.Printings) == 0 {
			record := catalog.CardRecord{
				SourceCardID: card.UniqueID,
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

		for _, printing := range card.Printings {
			sourcePrintingID := printing.UniqueID
			if sourcePrintingID == "" {
				sourcePrintingID = fmt.Sprintf("%s-%s-%s", card.UniqueID, printing.SetID, printing.ID)
			}

			setName := setNames[printing.SetID]
			if setName == "" {
				setName = printing.SetID
			}
			if setName == "" {
				setName = "Uncatalogued"
			}

			variant := mapFABFoiling(printing.Foiling)
			rarity := mapFABRarity(printing.Rarity)

			record := catalog.CardRecord{
				SourceCardID:    sourcePrintingID,
				Name:            card.Name,
				SetCode:         printing.SetID,
				SetName:         setName,
				CollectorNumber: printing.ID,
				Language:        "en",
				Variant:         variant,
				Rarity:          rarity,
				Metadata:        rawMetadata(card),
			}

			if printing.ImageURL != "" {
				record.Images = []catalog.ImageRecord{{
					SourceImageID: sourcePrintingID + ":front",
					Kind:          "front",
					URL:           printing.ImageURL,
				}}
			}

			record.Printings = []catalog.PrintingRecord{{
				SourcePrintingID: sourcePrintingID,
				SetCode:          printing.SetID,
				SetName:          setName,
				CollectorNumber:  printing.ID,
				Rarity:           rarity,
				Language:         "en",
				Variant:          variant,
				SourceImageIDs:   imageIDs(record.Images),
				Metadata:         rawMetadata(printing),
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
