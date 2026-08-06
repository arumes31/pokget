package source

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"pokget/internal/catalog"
)

type YGOPRODeckProvider struct {
	HTTP    HTTPOptions
	BaseURL string
}

func (p *YGOPRODeckProvider) ID() string         { return "ygoprodeck" }
func (p *YGOPRODeckProvider) Game() catalog.Game { return catalog.GameYuGiOh }

type ygoVersion struct {
	DatabaseVersion string `json:"database_version"`
	LastUpdate      string `json:"last_update"`
}

type ygoResponse struct {
	Data []ygoCard `json:"data"`
}

type ygoCard struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Sets []struct {
		Name       string `json:"set_name"`
		Code       string `json:"set_code"`
		Rarity     string `json:"set_rarity"`
		RarityCode string `json:"set_rarity_code"`
	} `json:"card_sets"`
	Images []struct {
		ID  int64  `json:"id"`
		URL string `json:"image_url"`
	} `json:"card_images"`
}

func (p *YGOPRODeckProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://db.ygoprodeck.com/api/v7"
	}
	var versions []ygoVersion
	meta, err := getJSON(ctx, p.HTTP, baseURL+"/checkDBVer.php", request, &versions)
	if err != nil {
		return catalog.FetchResult{}, err
	}
	result := catalog.FetchResult{ETag: meta.ETag, LastModified: meta.LastModified, CompleteSnapshot: true, NotModified: meta.NotModified}
	if meta.NotModified {
		return result, nil
	}
	if len(versions) == 0 || versions[0].DatabaseVersion == "" {
		return result, fmt.Errorf("ygoprodeck: database version response is empty")
	}
	result.UpstreamVersion = versions[0].DatabaseVersion + ":" + versions[0].LastUpdate
	if request.UpstreamVersion == result.UpstreamVersion {
		result.NotModified = true
		return result, nil
	}

	var response ygoResponse
	if _, err := getJSON(ctx, p.HTTP, baseURL+"/cardinfo.php", catalog.FetchRequest{}, &response); err != nil {
		return result, err
	}
	for _, card := range response.Data {
		sourceID := strconv.FormatInt(card.ID, 10)
		record := catalog.CardRecord{
			SourceCardID: sourceID,
			Name:         card.Name,
			SetName:      "Uncatalogued",
			Language:     "en",
			Metadata:     rawMetadata(card),
		}
		for imageIndex, image := range card.Images {
			imageID := strconv.FormatInt(image.ID, 10)
			if imageID == sourceID && imageIndex > 0 {
				imageID += ":" + strconv.Itoa(imageIndex)
			}
			record.Images = append(record.Images, catalog.ImageRecord{
				SourceImageID: sourceID + ":art:" + imageID,
				Kind:          "front",
				URL:           image.URL,
			})
		}
		for setIndex, set := range card.Sets {
			variant := set.Rarity
			if variant == "" {
				variant = "Normal"
			}
			printingID := set.Code + ":" + set.Rarity
			if printingID == ":" {
				printingID = sourceID + ":" + strconv.Itoa(setIndex)
			}
			record.Printings = append(record.Printings, catalog.PrintingRecord{
				SourcePrintingID: printingID,
				SetCode:          set.Code,
				SetName:          set.Name,
				CollectorNumber:  set.Code,
				Rarity:           set.Rarity,
				Language:         "en",
				Variant:          variant,
				Metadata:         rawMetadata(set),
			})
		}
		if len(card.Sets) > 0 {
			record.SetCode = card.Sets[0].Code
			record.SetName = card.Sets[0].Name
			record.CollectorNumber = card.Sets[0].Code
			record.Rarity = card.Sets[0].Rarity
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
