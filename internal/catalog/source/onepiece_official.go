package source

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"pokget/internal/catalog"
)

type OnePieceOfficialProvider struct {
	HTTP    HTTPOptions
	BaseURL string
}

func (p *OnePieceOfficialProvider) ID() string         { return "onepiece_official" }
func (p *OnePieceOfficialProvider) Game() catalog.Game { return catalog.GameOnePiece }

type onePieceSeries struct {
	ID   string
	Name string
	Code string
}

var bracketedSetCode = regexp.MustCompile(`\[([^\]]+)]`)

func (p *OnePieceOfficialProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://en.onepiece-cardgame.com/cardlist"
	}
	rootURL := baseURL + "/"
	document, meta, err := getDocument(ctx, p.HTTP, rootURL, request)
	if err != nil {
		return catalog.FetchResult{}, err
	}
	result := catalog.FetchResult{ETag: meta.ETag, LastModified: meta.LastModified, CompleteSnapshot: true, NotModified: meta.NotModified}
	if meta.NotModified {
		return result, nil
	}

	series := make([]onePieceSeries, 0, 64)
	document.Find(`select[name="series"] option[value]`).Each(func(_ int, selection *goquery.Selection) {
		id, _ := selection.Attr("value")
		id = strings.TrimSpace(id)
		if id == "" || strings.EqualFold(id, "ALL") {
			return
		}
		name := strings.Join(strings.Fields(selection.Text()), " ")
		code := id
		if match := bracketedSetCode.FindStringSubmatch(name); len(match) == 2 {
			code = match[1]
		}
		series = append(series, onePieceSeries{ID: id, Name: name, Code: code})
	})
	if len(series) == 0 {
		return result, fmt.Errorf("one piece official: no series options found; upstream layout may have changed")
	}

	seen := make(map[string]struct{})
	for index, item := range series {
		if index > 0 {
			if err := waitRequestDelay(ctx, p.HTTP.RequestDelay); err != nil {
				return result, err
			}
		}
		seriesURL := rootURL + "?series=" + url.QueryEscape(item.ID)
		page, _, err := getDocument(ctx, p.HTTP, seriesURL, catalog.FetchRequest{})
		if err != nil {
			return result, fmt.Errorf("one piece official series %s: %w", item.ID, err)
		}
		var parseErr error
		page.Find("dl.modalCol[id]").EachWithBreak(func(_ int, card *goquery.Selection) bool {
			visualID, _ := card.Attr("id")
			visualID = strings.TrimSpace(visualID)
			if visualID == "" {
				return true
			}
			if _, exists := seen[visualID]; exists {
				return true
			}
			seen[visualID] = struct{}{}
			spans := card.Find(".infoCol span")
			collectorNumber := strings.TrimSpace(spans.Eq(0).Text())
			rarity := strings.TrimSpace(spans.Eq(1).Text())
			name := strings.TrimSpace(card.Find(".cardName").First().Text())
			imagePath, _ := card.Find(".frontCol img[data-src]").First().Attr("data-src")
			imageURL := resolveURL(seriesURL, imagePath)
			record := catalog.CardRecord{
				SourceCardID:    visualID,
				Name:            name,
				SetCode:         item.Code,
				SetName:         item.Name,
				CollectorNumber: collectorNumber,
				Language:        "en",
				Rarity:          rarity,
				Metadata:        rawMetadata(map[string]string{"series_id": item.ID, "visual_id": visualID}),
			}
			if imageURL != "" {
				record.Images = []catalog.ImageRecord{{SourceImageID: visualID + ":front", Kind: "front", URL: imageURL}}
			}
			record.Printings = []catalog.PrintingRecord{{
				SourcePrintingID: visualID,
				SetCode:          item.Code,
				SetName:          item.Name,
				CollectorNumber:  collectorNumber,
				Rarity:           rarity,
				Language:         "en",
				SourceImageIDs:   imageIDs(record.Images),
			}}
			if err := record.Validate(); err != nil {
				parseErr = err
				return false
			}
			if err := emit(record); err != nil {
				parseErr = err
				return false
			}
			result.Count++
			return true
		})
		if parseErr != nil {
			return result, parseErr
		}
	}
	return result, nil
}

func resolveURL(base, reference string) string {
	if strings.TrimSpace(reference) == "" {
		return ""
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return ""
	}
	referenceURL, err := url.Parse(reference)
	if err != nil {
		return ""
	}
	return baseURL.ResolveReference(referenceURL).String()
}
