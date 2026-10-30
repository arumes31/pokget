package source

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"pokget/internal/catalog"
)

// GodzillaProvider imports cards for the Godzilla Card Game (by Toho & Bushiroad).
type GodzillaProvider struct {
	HTTP         HTTPOptions
	BaseURL      string
	ImageBaseURL string
}

func (p *GodzillaProvider) ID() string         { return "godzilla" }
func (p *GodzillaProvider) Game() catalog.Game { return catalog.GameGodzilla }

type godzillaCard struct {
	ID            int    `json:"id"`
	Picture       string `json:"picture"`
	CardNumber    string `json:"card_number"`
	CardName      string `json:"card_name"`
	CardKind      string `json:"card_kind"`
	Rare          string `json:"rare"`
	Expansion     string `json:"expansion"`
	ExpansionName string `json:"expansion_name"`
	Color         string `json:"color"`
	Text          string `json:"text"`
	StepIcon      any    `json:"step_icon"`
}

type godzillaResponse struct {
	Page    int            `json:"page"`
	PerPage int            `json:"per_page"`
	Total   int            `json:"total"`
	Items   []godzillaCard `json:"items"`
}

func (p *GodzillaProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://en.godzilla-cardgame.com"
	}
	imageBaseURL := strings.TrimRight(p.ImageBaseURL, "/")
	if imageBaseURL == "" {
		imageBaseURL = baseURL + "/wordpress/wp-content/images/cardlist"
	}

	result := catalog.FetchResult{CompleteSnapshot: true}
	seen := make(map[string]struct{})

	page := 1
	perPage := 100

	for {
		if page > 1 && p.HTTP.RequestDelay > 0 {
			if err := waitRequestDelay(ctx, p.HTTP.RequestDelay); err != nil {
				return result, err
			}
		}

		endpoint := fmt.Sprintf("%s/manage/card-list-user/list?page=%d&per_page=%d", baseURL, page, perPage)
		req, err := p.HTTP.newRequest(ctx, http.MethodGet, endpoint, request)
		if err != nil {
			return result, err
		}
		if strings.TrimSpace(p.HTTP.UserAgent) == "" {
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		}

		var resp godzillaResponse
		meta, err := getJSON(ctx, p.HTTP, endpoint, request, &resp)
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, fmt.Errorf("godzilla page %d: %w", page, err)
		}

		if page == 1 {
			result.ETag = meta.ETag
			result.LastModified = meta.LastModified
			result.NotModified = meta.NotModified
			if meta.NotModified {
				return result, nil
			}
		}

		if len(resp.Items) == 0 {
			break
		}

		for _, card := range resp.Items {
			num := strings.TrimSpace(card.CardNumber)
			if num == "" {
				continue
			}
			if _, exists := seen[num]; exists {
				continue
			}
			seen[num] = struct{}{}

			name := strings.TrimSpace(card.CardName)
			if name == "" {
				name = num
			}

			setCode := strings.TrimSpace(card.Expansion)
			if setCode == "" {
				setCode = "Godzilla"
			}
			setName := strings.TrimSpace(card.ExpansionName)
			if setName == "" {
				setName = setCode
			}

			rarity := strings.TrimSpace(card.Rare)
			if rarity == "" {
				rarity = "Normal"
			}

			record := catalog.CardRecord{
				SourceCardID:    num,
				Name:            name,
				SetCode:         setCode,
				SetName:         setName,
				CollectorNumber: num,
				Language:        "en",
				Rarity:          rarity,
				Metadata:        rawMetadata(card),
			}

			if card.Picture != "" {
				imgURL := imageBaseURL + "/" + strings.TrimPrefix(card.Picture, "/")
				record.Images = []catalog.ImageRecord{{
					SourceImageID: num + ":front",
					Kind:          "front",
					URL:           imgURL,
				}}
			}

			record.Printings = []catalog.PrintingRecord{{
				SourcePrintingID: num,
				SetCode:          setCode,
				SetName:          setName,
				CollectorNumber:  num,
				Language:         "en",
				Rarity:           rarity,
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

		if page*perPage >= resp.Total || len(resp.Items) < perPage {
			break
		}
		page++
	}

	return result, nil
}
