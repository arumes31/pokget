package source

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"pokget/internal/catalog"
)

// UnionArenaOfficialProvider ingests Union Arena cards from Bandai's official website.
type UnionArenaOfficialProvider struct {
	HTTP    HTTPOptions
	BaseURL string
}

func (p *UnionArenaOfficialProvider) ID() string         { return "unionarena_official" }
func (p *UnionArenaOfficialProvider) Game() catalog.Game { return catalog.GameUnionArena }

type unionArenaSeries struct {
	ID   string
	Name string
	Code string
}

func (p *UnionArenaOfficialProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://www.unionarena-tcg.com/na/cardlist"
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

	series := make([]unionArenaSeries, 0, 64)
	document.Find(`select[name="series"] option[value]`).Each(func(_ int, selection *goquery.Selection) {
		id, _ := selection.Attr("value")
		id = strings.TrimSpace(id)
		if id == "" || strings.EqualFold(id, "ALL") {
			return
		}
		name := onePieceSeriesDisplayName(selection.Text())
		code := id
		if match := bracketedSetCode.FindStringSubmatch(name); len(match) == 2 {
			code = match[1]
		}
		series = append(series, unionArenaSeries{ID: id, Name: name, Code: code})
	})
	// Fallback if cards are directly on the page (e.g. single set mock in tests)
	if len(series) == 0 && (document.Find("li.cardImgCol").Length() > 0 || document.Find("dl.modalCol[id]").Length() > 0) {
		series = append(series, unionArenaSeries{ID: "", Name: "Union Arena", Code: "UA"})
	}
	if len(series) == 0 {
		return result, fmt.Errorf("union arena official: no series options found; upstream layout may have changed")
	}

	seen := make(map[string]struct{})
	for index, item := range series {
		if index > 0 {
			if err := waitRequestDelay(ctx, p.HTTP.RequestDelay); err != nil {
				return result, err
			}
		}

		page := document
		seriesURL := rootURL
		if item.ID != "" {
			seriesURL = baseURL + "/?search=true&series=" + url.QueryEscape(item.ID)
			p, _, err := getDocument(ctx, p.HTTP, seriesURL, catalog.FetchRequest{})
			if err != nil {
				return result, fmt.Errorf("union arena official series %s: %w", item.ID, err)
			}
			page = p
		}

		var parseErr error

		// Format A: Modal-based cardlist (identical to One Piece)
		if page.Find("dl.modalCol[id]").Length() > 0 {
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
				if collectorNumber == "" {
					collectorNumber = visualID
				}
				rarity := strings.TrimSpace(spans.Eq(1).Text())
				name := strings.TrimSpace(card.Find(".cardName").First().Text())
				imagePath, _ := card.Find(".frontCol img[data-src]").First().Attr("data-src")
				if imagePath == "" {
					imagePath, _ = card.Find(".frontCol img[src]").First().Attr("src")
				}
				imageURL := resolveURL(seriesURL, imagePath)

				variant := "Normal"
				if strings.Contains(visualID, "_p") {
					variant = "Parallel"
				}

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
					Variant:          variant,
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
		} else {
			// Format B: Grid card list (li.cardImgCol)
			page.Find("li.cardImgCol").EachWithBreak(func(_ int, card *goquery.Selection) bool {
				a := card.Find("a.modalCardDataOpen")
				img := card.Find("img")
				alt, _ := img.Attr("alt")
				alt = strings.TrimSpace(alt)

				imagePath, _ := img.Attr("data-src")
				if imagePath == "" {
					imagePath, _ = img.Attr("src")
				}
				imageURL := resolveURL(seriesURL, imagePath)

				href, _ := a.Attr("href")
				cardNo := ""
				if href != "" {
					if u, err := url.Parse(href); err == nil {
						cardNo = u.Query().Get("card_no")
					}
				}

				collectorNumber := ""
				name := ""
				if alt != "" {
					parts := strings.SplitN(alt, " ", 2)
					collectorNumber = parts[0]
					if len(parts) > 1 {
						name = strings.TrimSpace(parts[1])
					}
				}
				if collectorNumber == "" {
					collectorNumber = cardNo
				}
				if collectorNumber == "" {
					return true
				}
				if name == "" {
					name = collectorNumber
				}

				visualID := cardNo
				if visualID == "" {
					visualID = collectorNumber
				}
				if _, exists := seen[visualID]; exists {
					return true
				}
				seen[visualID] = struct{}{}

				baseCollectorNumber := collectorNumber
				if idx := strings.Index(baseCollectorNumber, "_"); idx != -1 {
					baseCollectorNumber = baseCollectorNumber[:idx]
				}

				variant := "Normal"
				if strings.Contains(visualID, "_p") {
					variant = "Parallel"
				}

				setCode := item.Code
				if setCode == "" || setCode == item.ID {
					if idx := strings.Index(baseCollectorNumber, "/"); idx != -1 {
						setCode = baseCollectorNumber[:idx]
					} else {
						setCode = "UA"
					}
				}

				record := catalog.CardRecord{
					SourceCardID:    visualID,
					Name:            name,
					SetCode:         setCode,
					SetName:         item.Name,
					CollectorNumber: baseCollectorNumber,
					Language:        "en",
					Metadata:        rawMetadata(map[string]string{"series_id": item.ID, "visual_id": visualID}),
				}
				if imageURL != "" {
					record.Images = []catalog.ImageRecord{{SourceImageID: visualID + ":front", Kind: "front", URL: imageURL}}
				}
				record.Printings = []catalog.PrintingRecord{{
					SourcePrintingID: visualID,
					SetCode:          setCode,
					SetName:          item.Name,
					CollectorNumber:  baseCollectorNumber,
					Language:         "en",
					Variant:          variant,
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
		}

		if parseErr != nil {
			return result, parseErr
		}
	}

	return result, nil
}
