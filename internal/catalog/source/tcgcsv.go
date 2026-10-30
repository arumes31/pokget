package source

import (
	"context"
	"fmt"
	"strings"

	"pokget/internal/catalog"
)

// TCGCSVProvider is a generic provider for games cataloged via TCGplayer's open TCGCSV mirror.
type TCGCSVProvider struct {
	HTTP         HTTPOptions
	BaseURL      string
	ImageBaseURL string
	CategoryID   int
	ProviderID   string
	GameType     catalog.Game
	DefaultSet   string
	MaxGroups    int
}

func (p *TCGCSVProvider) ID() string         { return p.ProviderID }
func (p *TCGCSVProvider) Game() catalog.Game { return p.GameType }

type tcgcsvGroup struct {
	GroupID      int    `json:"groupId"`
	Name         string `json:"name"`
	Abbreviation string `json:"abbreviation"`
}

type tcgcsvGroupsResponse struct {
	Results []tcgcsvGroup `json:"results"`
}

type tcgcsvExtendedData struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type tcgcsvProduct struct {
	ProductID    int                  `json:"productId"`
	Name         string               `json:"name"`
	CleanName    string               `json:"cleanName"`
	ImageURL     string               `json:"imageUrl"`
	GroupID      int                  `json:"groupId"`
	ExtendedData []tcgcsvExtendedData `json:"extendedData"`
}

type tcgcsvProductsResponse struct {
	Results []tcgcsvProduct `json:"results"`
}

func (p *TCGCSVProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://tcgcsv.com/tcgplayer"
	}

	result := catalog.FetchResult{CompleteSnapshot: true}

	groupsEndpoint := fmt.Sprintf("%s/%d/groups", baseURL, p.CategoryID)
	var groupsResp tcgcsvGroupsResponse
	meta, err := getJSON(ctx, p.HTTP, groupsEndpoint, request, &groupsResp)
	if err != nil {
		return result, fmt.Errorf("tcgcsv groups: %w", err)
	}

	result.ETag = meta.ETag
	result.LastModified = meta.LastModified
	result.NotModified = meta.NotModified
	if meta.NotModified {
		return result, nil
	}

	groups := groupsResp.Results
	if len(groups) == 0 {
		return result, nil
	}

	maxGroups := p.MaxGroups
	if maxGroups <= 0 && request.Mode == catalog.SyncModeIncremental {
		maxGroups = 3
	}
	if maxGroups > 0 && len(groups) > maxGroups {
		groups = groups[:maxGroups]
		result.CompleteSnapshot = false
	}

	seen := make(map[string]struct{})
	for _, group := range groups {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if p.HTTP.RequestDelay > 0 {
			if err := waitRequestDelay(ctx, p.HTTP.RequestDelay); err != nil {
				return result, err
			}
		}

		productsEndpoint := fmt.Sprintf("%s/%d/%d/products", baseURL, p.CategoryID, group.GroupID)
		var productsResp tcgcsvProductsResponse
		_, err := getJSON(ctx, p.HTTP, productsEndpoint, catalog.FetchRequest{}, &productsResp)
		if err != nil {
			return result, fmt.Errorf("tcgcsv products group %d: %w", group.GroupID, err)
		}

		for _, prod := range productsResp.Results {
			sourceID := fmt.Sprintf("%d", prod.ProductID)
			if sourceID == "" || sourceID == "0" {
				continue
			}
			if _, exists := seen[sourceID]; exists {
				continue
			}
			seen[sourceID] = struct{}{}

			name := strings.TrimSpace(prod.CleanName)
			if name == "" {
				name = strings.TrimSpace(prod.Name)
			}
			if name == "" {
				name = sourceID
			}

			rarity := ""
			collectorNum := ""
			for _, ext := range prod.ExtendedData {
				switch strings.ToLower(ext.Name) {
				case "rarity":
					rarity = strings.TrimSpace(ext.Value)
				case "number":
					collectorNum = strings.TrimSpace(ext.Value)
				}
			}
			if rarity == "" {
				rarity = "Normal"
			}
			if collectorNum == "" {
				collectorNum = sourceID
			}

			setCode := strings.TrimSpace(group.Abbreviation)
			if setCode == "" {
				setCode = strings.TrimSpace(group.Name)
			}
			if setCode == "" {
				setCode = p.DefaultSet
			}
			setName := strings.TrimSpace(group.Name)
			if setName == "" {
				setName = setCode
			}

			record := catalog.CardRecord{
				SourceCardID:    sourceID,
				Name:            name,
				SetCode:         setCode,
				SetName:         setName,
				CollectorNumber: collectorNum,
				Language:        "en",
				Rarity:          rarity,
				Metadata:        rawMetadata(prod),
			}

			if prod.ImageURL != "" {
				record.Images = []catalog.ImageRecord{{
					SourceImageID: sourceID + ":front",
					Kind:          "front",
					URL:           prod.ImageURL,
				}}
			}

			record.Printings = []catalog.PrintingRecord{{
				SourcePrintingID: sourceID,
				SetCode:          setCode,
				SetName:          setName,
				CollectorNumber:  collectorNum,
				Language:         "en",
				Rarity:           rarity,
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

// NewCookieRunProvider returns a provider for CookieRun: Braverse TCG.
func NewCookieRunProvider(options HTTPOptions) *TCGCSVProvider {
	return &TCGCSVProvider{
		HTTP:        options,
		CategoryID:  90,
		ProviderID:  "cookierun_braverse",
		GameType:    catalog.GameCookieRun,
		DefaultSet:  "CookieRun",
	}
}

// NewForceOfWillProvider returns a provider for Force of Will TCG.
func NewForceOfWillProvider(options HTTPOptions) *TCGCSVProvider {
	return &TCGCSVProvider{
		HTTP:        options,
		CategoryID:  17,
		ProviderID:  "force_of_will",
		GameType:    catalog.GameForceOfWill,
		DefaultSet:  "ForceOfWill",
	}
}

// NewWIXOSSProvider returns a provider for WIXOSS TCG.
func NewWIXOSSProvider(options HTTPOptions) *TCGCSVProvider {
	return &TCGCSVProvider{
		HTTP:        options,
		CategoryID:  67,
		ProviderID:  "wixoss",
		GameType:    catalog.GameWIXOSS,
		DefaultSet:  "WIXOSS",
	}
}
