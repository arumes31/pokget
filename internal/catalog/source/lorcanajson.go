package source

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"pokget/internal/catalog"
)

type LorcanaJSONProvider struct {
	HTTP     HTTPOptions
	BaseURL  string
	Language string
}

func (p *LorcanaJSONProvider) ID() string         { return "lorcanajson" }
func (p *LorcanaJSONProvider) Game() catalog.Game { return catalog.GameLorcana }

type lorcanaJSONDocument struct {
	Metadata struct {
		FormatVersion string `json:"formatVersion"`
		GeneratedOn   string `json:"generatedOn"`
		Language      string `json:"language"`
	} `json:"metadata"`
	Sets  map[string]lorcanaJSONSet `json:"sets"`
	Cards []lorcanaJSONCard         `json:"cards"`
}

type lorcanaJSONSet struct {
	Name        string `json:"name"`
	ReleaseDate string `json:"releaseDate"`
}

type lorcanaJSONCard struct {
	ID         int    `json:"id"`
	FullName   string `json:"fullName"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Number     int    `json:"number"`
	SetCode    string `json:"setCode"`
	Rarity     string `json:"rarity"`
	Identifier string `json:"fullIdentifier"`
	Images     struct {
		Full string `json:"full"`
	} `json:"images"`
}

func (p *LorcanaJSONProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://lorcanajson.org/files/current"
	}
	language := p.Language
	if language == "" {
		language = "en"
	}
	var document lorcanaJSONDocument
	meta, err := getJSON(ctx, p.HTTP, fmt.Sprintf("%s/%s/allCards.json", baseURL, language), request, &document)
	if err != nil {
		return catalog.FetchResult{}, err
	}
	result := catalog.FetchResult{
		ETag:             meta.ETag,
		LastModified:     meta.LastModified,
		UpstreamVersion:  document.Metadata.FormatVersion + ":" + document.Metadata.GeneratedOn,
		CompleteSnapshot: true,
		NotModified:      meta.NotModified,
	}
	if meta.NotModified {
		return result, nil
	}
	if request.UpstreamVersion != "" && request.UpstreamVersion == result.UpstreamVersion {
		result.NotModified = true
		return result, nil
	}
	if document.Metadata.Language != "" {
		language = document.Metadata.Language
	}
	for _, card := range document.Cards {
		set := document.Sets[card.SetCode]
		name := card.FullName
		if name == "" {
			name = strings.TrimSpace(card.Name + " " + card.Version)
		}
		record := catalog.CardRecord{
			SourceCardID:    strconv.Itoa(card.ID),
			Name:            name,
			SetCode:         card.SetCode,
			SetName:         set.Name,
			CollectorNumber: strconv.Itoa(card.Number),
			Language:        language,
			Rarity:          card.Rarity,
			ReleasedAt:      parseDate(set.ReleaseDate),
			Metadata:        rawMetadata(card),
		}
		if record.SetName == "" {
			record.SetName = card.SetCode
		}
		if card.Images.Full != "" {
			record.Images = []catalog.ImageRecord{{SourceImageID: record.SourceCardID + ":front", Kind: "front", URL: card.Images.Full}}
		}
		record.Printings = []catalog.PrintingRecord{{
			SourcePrintingID: record.SourceCardID,
			SetCode:          record.SetCode,
			SetName:          record.SetName,
			CollectorNumber:  record.CollectorNumber,
			Rarity:           record.Rarity,
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
	return result, nil
}
