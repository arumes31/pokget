package source

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"pokget/internal/catalog"
)

type AlteredProvider struct {
	HTTP     HTTPOptions
	BaseURL  string
	Language string
}

func (p *AlteredProvider) ID() string         { return "altered" }
func (p *AlteredProvider) Game() catalog.Game { return catalog.GameAltered }

type alteredSetMetadata struct {
	HydraMember []struct {
		ID        string `json:"id"`
		Reference string `json:"reference"`
		Name      string `json:"name"`
		Code      string `json:"code"`
	} `json:"hydra:member"`
}

type alteredCard struct {
	ID              string `json:"id"`
	Reference       string `json:"reference"`
	Name            string `json:"name"`
	CollectorNumber string `json:"collectorNumber"`
	ImagePath       string `json:"imagePath"`
	CardSet         struct {
		ID        string `json:"id"`
		Reference string `json:"reference"`
		Name      string `json:"name"`
		Code      string `json:"code"`
	} `json:"cardSet"`
	Rarity struct {
		ID        string `json:"id"`
		Reference string `json:"reference"`
		Name      string `json:"name"`
	} `json:"rarity"`
	AllImagePath map[string]string `json:"allImagePath"`
}

func (p *AlteredProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	languages := catalogLanguages(p.Language)
	if len(languages) > 1 {
		var supported []string
		for _, language := range languages {
			switch language {
			case "en", "fr", "de", "it", "es":
				supported = append(supported, language)
			}
		}
		if len(supported) == 0 {
			supported = []string{"en"}
		}
		if len(supported) > 1 {
			return fetchLanguages(ctx, supported, request, func(ctx context.Context, language string, request catalog.FetchRequest) (catalog.FetchResult, error) {
				localized := *p
				localized.Language = language
				return localized.Fetch(ctx, request, emit)
			})
		}
		languages = supported
	}

	language := "en"
	if len(languages) > 0 {
		language = languages[0]
	}

	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://raw.githubusercontent.com/PolluxTroy0/Altered-TCG-Card-Database/main"
	}

	result := catalog.FetchResult{CompleteSnapshot: true}

	// Direct JSON document check (e.g. test files)
	if strings.HasSuffix(baseURL, ".json") {
		var raw json.RawMessage
		meta, err := getJSON(ctx, p.HTTP, baseURL, request, &raw)
		if err != nil {
			return result, err
		}
		result.ETag = meta.ETag
		result.LastModified = meta.LastModified
		if meta.NotModified {
			result.NotModified = true
			return result, nil
		}

		cards, err := parseAlteredCards(raw)
		if err != nil {
			return result, fmt.Errorf("altered: parse cards from %s: %w", baseURL, err)
		}
		for _, card := range cards {
			record := buildAlteredRecord(card, language, "")
			if err := record.Validate(); err != nil {
				return result, err
			}
			if err := emit(record); err != nil {
				return result, err
			}
			result.Count++
		}
		return result, nil
	}

	// Otherwise, fetch sets metadata or fallback to default sets
	setRefs := make(map[string]string) // ref -> name
	metaURL := baseURL + "/META/card_sets_en.json"
	var setMeta alteredSetMetadata
	if _, err := getJSON(ctx, p.HTTP, metaURL, catalog.FetchRequest{}, &setMeta); err == nil && len(setMeta.HydraMember) > 0 {
		for _, s := range setMeta.HydraMember {
			if s.Reference != "" {
				setRefs[s.Reference] = s.Name
			}
		}
	}

	if len(setRefs) == 0 {
		defaultSets := []string{
			"CORE", "COREKS", "ALIZE", "BISE", "CYCLONE", "DUSTER", "DUSTERCB", "DUSTEROP",
			"DUSTERTOP", "EOLE", "EOLECB", "EOLEOP", "EOLETOP", "FUGUE", "MUSUBI", "JUDGE",
			"TCS3", "WCF25", "WCQ25", "WCS25", "WCS26",
		}
		for _, s := range defaultSets {
			setRefs[s] = s
		}
	}

	langUpper := strings.ToUpper(language)
	for setRef, setName := range setRefs {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		setFileURL := fmt.Sprintf("%s/SETS/%s/%s_%s.json", baseURL, setRef, setRef, langUpper)
		var raw json.RawMessage
		_, err := getJSON(ctx, p.HTTP, setFileURL, catalog.FetchRequest{}, &raw)
		if err != nil {
			// Some promotional or sub-sets may only exist in English
			if language != "en" {
				continue
			}
			return result, fmt.Errorf("altered: fetch set %s: %w", setRef, err)
		}

		cards, err := parseAlteredCards(raw)
		if err != nil {
			return result, fmt.Errorf("altered: parse set %s: %w", setRef, err)
		}

		for _, card := range cards {
			record := buildAlteredRecord(card, language, setName)
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

func parseAlteredCards(raw json.RawMessage) ([]alteredCard, error) {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var cards []alteredCard
		if err := json.Unmarshal(raw, &cards); err != nil {
			return nil, err
		}
		return cards, nil
	}

	var hydra struct {
		Member []alteredCard `json:"hydra:member"`
		Data   []alteredCard `json:"data"`
	}
	if err := json.Unmarshal(raw, &hydra); err != nil {
		return nil, err
	}
	if len(hydra.Member) > 0 {
		return hydra.Member, nil
	}
	return hydra.Data, nil
}

func buildAlteredRecord(card alteredCard, language, fallbackSetName string) catalog.CardRecord {
	sourceID := card.Reference
	if sourceID == "" {
		sourceID = card.ID
	}

	setRef := card.CardSet.Reference
	if setRef == "" {
		setRef = card.CardSet.Code
	}

	setName := card.CardSet.Name
	if setName == "" {
		setName = fallbackSetName
	}
	if setName == "" {
		setName = setRef
	}
	if setName == "" {
		setName = "Altered TCG"
	}

	collectorNum := card.CollectorNumber
	if collectorNum == "" {
		collectorNum = card.Reference
	}

	rarity := card.Rarity.Name
	if rarity == "" {
		rarity = card.Rarity.Reference
	}
	if rarity == "" {
		rarity = "Common"
	}

	imageURL := fmt.Sprintf("https://cdn.alteredcore.org/cards/%s/%s/%s.webp", strings.ToLower(language), setRef, card.Reference)
	if card.Reference == "" && card.ImagePath != "" {
		imageURL = card.ImagePath
	}

	record := catalog.CardRecord{
		SourceCardID:    sourceID,
		Name:            card.Name,
		SetCode:         setRef,
		SetName:         setName,
		CollectorNumber: collectorNum,
		Language:        strings.ToLower(language),
		Rarity:          rarity,
		Variant:         "Normal",
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
		SetCode:          setRef,
		SetName:          setName,
		CollectorNumber:  collectorNum,
		Rarity:           rarity,
		Language:         strings.ToLower(language),
		Variant:          "Normal",
		SourceImageIDs:   imageIDs(record.Images),
		Metadata:         rawMetadata(card),
	}}

	return record
}
