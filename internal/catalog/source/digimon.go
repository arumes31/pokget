package source

import (
	"context"
	"fmt"
	"strings"

	"pokget/internal/catalog"
)

type DigimonCardIOProvider struct {
	HTTP         HTTPOptions
	BaseURL      string
	ImageBaseURL string
}

func (p *DigimonCardIOProvider) ID() string         { return "digimoncardio" }
func (p *DigimonCardIOProvider) Game() catalog.Game { return catalog.GameDigimon }

type digimonCard struct {
	Name       string `json:"name"`
	CardNumber string `json:"cardnumber"`
}

func (p *DigimonCardIOProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://digimoncard.io/api-public"
	}

	endpoint := baseURL
	if !strings.Contains(endpoint, "getAllCards") {
		endpoint += "/getAllCards.php?sort=name&series=Digimon%20Card%20Game&sortdirection=asc"
	}

	imageBaseURL := strings.TrimRight(p.ImageBaseURL, "/")
	if imageBaseURL == "" {
		imageBaseURL = "https://images.digimoncard.io/images/cards"
	}

	var cards []digimonCard
	meta, err := getJSON(ctx, p.HTTP, endpoint, request, &cards)
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
		if card.Name == "" || card.CardNumber == "" {
			continue
		}

		setCode := card.CardNumber
		if idx := strings.Index(card.CardNumber, "-"); idx != -1 {
			setCode = card.CardNumber[:idx]
		}
		setName := setCode
		if setName == "" {
			setName = "Digimon Card Game"
		}

		imageURL := fmt.Sprintf("%s/%s.jpg", imageBaseURL, card.CardNumber)
		sourceCardID := card.CardNumber

		record := catalog.CardRecord{
			SourceCardID:    sourceCardID,
			Name:            card.Name,
			SetCode:         setCode,
			SetName:         setName,
			CollectorNumber: card.CardNumber,
			Language:        "en",
			Images: []catalog.ImageRecord{{
				SourceImageID: sourceCardID + ":front",
				Kind:          "front",
				URL:           imageURL,
			}},
			Printings: []catalog.PrintingRecord{{
				SourcePrintingID: sourceCardID,
				SetCode:          setCode,
				SetName:          setName,
				CollectorNumber:  card.CardNumber,
				Language:         "en",
				Variant:          "Normal",
				SourceImageIDs:   []string{sourceCardID + ":front"},
				Metadata:         rawMetadata(card),
			}},
			Metadata: rawMetadata(card),
		}

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
