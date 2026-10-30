package source

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"pokget/internal/catalog"
)

type SorceryProvider struct {
	HTTP    HTTPOptions
	BaseURL string
}

func (p *SorceryProvider) ID() string         { return "sorcery" }
func (p *SorceryProvider) Game() catalog.Game { return catalog.GameSorcery }

type sorceryCard struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Engine struct {
		Type     string `json:"type"`
		Category string `json:"category"`
		Rarity   string `json:"rarity"`
		Slot     string `json:"slot"`
		Rules    string `json:"rules"`
	} `json:"engine"`
	Printings []sorceryPrinting `json:"printings"`
}

type sorceryPrinting struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	PrintedAt string `json:"printedAt"`
	Set       struct {
		Name       string `json:"name"`
		Code       string `json:"code"`
		ReleasedAt string `json:"releasedAt"`
	} `json:"set"`
	Meta struct {
		Finish   string `json:"finish"`
		Product  string `json:"product"`
		Typeline string `json:"typeline"`
		Flavor   string `json:"flavor"`
		Artist   struct {
			Name string `json:"name"`
		} `json:"artist"`
	} `json:"meta"`
}

// FourCores alternative schema
type fourCoresCard struct {
	Name      string `json:"name"`
	Rarity    string `json:"rarity"`
	Category  string `json:"category"`
	RulesText string `json:"rulesText"`
	Printings []struct {
		ID       int    `json:"id"`
		SetCode  string `json:"setCode"`
		Artist   string `json:"artist"`
		Finishes []struct {
			Type string `json:"type"`
		} `json:"finishes"`
	} `json:"printings"`
}

func (p *SorceryProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.sorcerytcg.com"
	}

	endpoint := baseURL
	if !strings.HasSuffix(endpoint, ".json") && !strings.Contains(endpoint, "/cards") {
		endpoint = baseURL + "/api/cards"
	}

	var raw json.RawMessage
	meta, err := getJSON(ctx, p.HTTP, endpoint, request, &raw)
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

	// Try parsing official format first
	var officialCards []sorceryCard
	if err := json.Unmarshal(raw, &officialCards); err == nil && len(officialCards) > 0 && (officialCards[0].Engine.Rarity != "" || len(officialCards[0].Printings) > 0) {
		for _, card := range officialCards {
			if card.Name == "" {
				continue
			}

			if len(card.Printings) == 0 {
				record := catalog.CardRecord{
					SourceCardID: card.ID,
					Name:         card.Name,
					SetName:      "Sorcery: Contested Realm",
					Language:     "en",
					Rarity:       card.Engine.Rarity,
					Variant:      "Normal",
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
				sourceID := printing.ID
				if sourceID == "" {
					sourceID = printing.Slug
				}
				if sourceID == "" {
					sourceID = card.ID
				}

				setName := printing.Set.Name
				if setName == "" {
					setName = printing.Set.Code
				}
				if setName == "" {
					setName = "Sorcery: Contested Realm"
				}

				setCode := printing.Set.Code

				collectorNum := printing.Slug
				if collectorNum == "" {
					collectorNum = card.Slug
				}

				rarity := card.Engine.Rarity
				if rarity == "" {
					rarity = "Ordinary"
				}

				variant := "Normal"
				if strings.EqualFold(printing.Meta.Finish, "foil") {
					variant = "Foil"
				}

				imageURL := ""
				if printing.Slug != "" {
					imageURL = fmt.Sprintf("https://d27a44hjr9gen3.cloudfront.net/cards/%s.png", printing.Slug)
				}

				record := catalog.CardRecord{
					SourceCardID:    sourceID,
					Name:            card.Name,
					SetCode:         setCode,
					SetName:         setName,
					CollectorNumber: collectorNum,
					Language:        "en",
					Rarity:          rarity,
					Variant:         variant,
					Metadata:        rawMetadata(card),
				}

				if imageURL != "" {
					record.Images = []catalog.ImageRecord{{
						SourceImageID: sourceID + ":front",
						Kind:          "front",
						URL:           imageURL,
					}}
				}

				record.Printings = []catalog.PrintingRecord{{
					SourcePrintingID: sourceID,
					SetCode:          setCode,
					SetName:          setName,
					CollectorNumber:  collectorNum,
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

	// Fallback to FourCores format
	var fourCores []fourCoresCard
	if err := json.Unmarshal(raw, &fourCores); err == nil && len(fourCores) > 0 {
		for _, card := range fourCores {
			if card.Name == "" {
				continue
			}

			if len(card.Printings) == 0 {
				record := catalog.CardRecord{
					SourceCardID: card.Name,
					Name:         card.Name,
					SetName:      "Sorcery: Contested Realm",
					Language:     "en",
					Rarity:       card.Rarity,
					Variant:      "Normal",
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
				sourceID := strconv.Itoa(printing.ID)
				setName := printing.SetCode
				if setName == "" {
					setName = "Sorcery: Contested Realm"
				}

				imageURL := fmt.Sprintf("https://fourcores-home.netlify.app/images/cards/%d.webp", printing.ID)

				record := catalog.CardRecord{
					SourceCardID:    sourceID,
					Name:            card.Name,
					SetCode:         printing.SetCode,
					SetName:         setName,
					CollectorNumber: strconv.Itoa(printing.ID),
					Language:        "en",
					Rarity:          card.Rarity,
					Variant:         "Normal",
					Metadata:        rawMetadata(card),
				}

				record.Images = []catalog.ImageRecord{{
					SourceImageID: sourceID + ":front",
					Kind:          "front",
					URL:           imageURL,
				}}

				record.Printings = []catalog.PrintingRecord{{
					SourcePrintingID: sourceID,
					SetCode:          printing.SetCode,
					SetName:          setName,
					CollectorNumber:  strconv.Itoa(printing.ID),
					Rarity:           card.Rarity,
					Language:         "en",
					Variant:          "Normal",
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

	return result, fmt.Errorf("sorcery: unrecognized card data format")
}
