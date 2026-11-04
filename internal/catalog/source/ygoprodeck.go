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
	// TCGCSVBaseURL points at the TCGplayer CSV mirror used to attach a
	// per-printing scan (the YGOPRODeck artwork is a generic replica without the
	// printed set code, so the detector cannot read a number from it).
	TCGCSVBaseURL string
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
	primaryCodes := make([]string, 0, len(response.Data))
	for _, card := range response.Data {
		if len(card.Sets) > 0 {
			primaryCodes = append(primaryCodes, card.Sets[0].Code)
		}
	}
	printingImages := p.printingImages(ctx, primaryCodes)
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
		if url := printingImages[strings.ToUpper(strings.TrimSpace(record.CollectorNumber))]; url != "" {
			// The primary image is the actual printing scan; the generic
			// YGOPRODeck artwork stays available as a second image.
			record.Images = append([]catalog.ImageRecord{{
				SourceImageID: sourceID + ":printing",
				Kind:          "front",
				URL:           url,
			}}, record.Images...)
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

type ygoTCGCSVGroupsResponse struct {
	Results []struct {
		GroupID      int    `json:"groupId"`
		Abbreviation string `json:"abbreviation"`
	} `json:"results"`
}

type ygoTCGCSVProductsResponse struct {
	Results []struct {
		ImageURL string `json:"imageUrl"`
		Extended []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"extendedData"`
	} `json:"results"`
}

// printingImages maps a printed set code ("LOB-005") to the TCGplayer scanning
// image for that printing. It is best effort: a TCGCSV outage must not fail the
// Yu-Gi-Oh catalog sync, it only leaves the generic artwork in place.
func (p *YGOPRODeckProvider) printingImages(ctx context.Context, codes []string) map[string]string {
	if len(codes) == 0 {
		return nil
	}
	base := strings.TrimRight(p.TCGCSVBaseURL, "/")
	if base == "" {
		base = "https://tcgcsv.com/tcgplayer"
	}
	var groups ygoTCGCSVGroupsResponse
	if _, err := getJSON(ctx, p.HTTP, base+"/2/groups", catalog.FetchRequest{}, &groups); err != nil {
		return nil
	}
	groupByPrefix := make(map[string]int, len(groups.Results))
	for _, group := range groups.Results {
		groupByPrefix[strings.ToUpper(group.Abbreviation)] = group.GroupID
	}
	groupCodes := make(map[int][]string)
	for _, code := range codes {
		prefix := code
		if index := strings.Index(code, "-"); index > 0 {
			prefix = code[:index]
		}
		if groupID, ok := groupByPrefix[strings.ToUpper(prefix)]; ok {
			groupCodes[groupID] = append(groupCodes[groupID], code)
		}
	}
	images := make(map[string]string, len(codes))
	for groupID := range groupCodes {
		var products ygoTCGCSVProductsResponse
		url := fmt.Sprintf("%s/2/%d/products", base, groupID)
		if _, err := getJSON(ctx, p.HTTP, url, catalog.FetchRequest{}, &products); err != nil {
			continue
		}
		for _, product := range products.Results {
			if product.ImageURL == "" {
				continue
			}
			for _, extended := range product.Extended {
				if strings.EqualFold(extended.Name, "Number") {
					images[strings.ToUpper(strings.TrimSpace(extended.Value))] = highResolutionTCGPlayerImage(product.ImageURL)
				}
			}
		}
	}
	return images
}

func highResolutionTCGPlayerImage(url string) string {
	return strings.Replace(url, "_200w.jpg", "_in_1000x1000.jpg", 1)
}
