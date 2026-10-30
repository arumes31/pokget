package source

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"pokget/internal/catalog"
)

// BattleSpiritsSagaOfficialProvider ingests Battle Spirits Saga cards from Bandai's official website.
type BattleSpiritsSagaOfficialProvider struct {
	HTTP    HTTPOptions
	BaseURL string
}

func (p *BattleSpiritsSagaOfficialProvider) ID() string         { return "battlespirits_official" }
func (p *BattleSpiritsSagaOfficialProvider) Game() catalog.Game { return catalog.GameBattleSpiritsSaga }

type bssSeries struct {
	ID   string
	Name string
	Code string
}

func (p *BattleSpiritsSagaOfficialProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://battlespirits-saga.com/cards"
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

	series := make([]bssSeries, 0, 64)
	document.Find(`select[name="category"] option[value]`).Each(func(_ int, sel *goquery.Selection) {
		id, _ := sel.Attr("value")
		id = strings.TrimSpace(id)
		if id == "" || strings.EqualFold(id, "ALL") {
			return
		}
		name := onePieceSeriesDisplayName(sel.Text())
		code := id
		if match := bracketedSetCode.FindStringSubmatch(name); len(match) == 2 {
			code = match[1]
		}
		series = append(series, bssSeries{ID: id, Name: name, Code: code})
	})
	// Fallback if cards are directly on the page (e.g. single set mock in tests)
	if len(series) == 0 && document.Find("li.cardImg").Length() > 0 {
		series = append(series, bssSeries{ID: "", Name: "Battle Spirits Saga", Code: "BSS01"})
	}
	if len(series) == 0 {
		return result, fmt.Errorf("battle spirits official: no series options found; upstream layout may have changed")
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
			seriesURL = baseURL + "/?search=true&category=" + url.QueryEscape(item.ID) + "&p_flg=on"
			p, _, err := getDocument(ctx, p.HTTP, seriesURL, catalog.FetchRequest{})
			if err != nil {
				return result, fmt.Errorf("battle spirits official series %s: %w", item.ID, err)
			}
			page = p
		}

		var parseErr error
		page.Find("li.cardImg").EachWithBreak(func(_ int, card *goquery.Selection) bool {
			a := card.Find("a")
			img := card.Find("img")

			imagePath, _ := img.Attr("src")
			if imagePath == "" {
				imagePath, _ = img.Attr("data-src")
			}
			imageURL := resolveURL(seriesURL, imagePath)

			detailHref, _ := a.Attr("data-src")
			if detailHref == "" {
				detailHref, _ = a.Attr("href")
			}

			cardNo := ""
			if detailHref != "" {
				if u, err := url.Parse(detailHref); err == nil {
					cardNo = u.Query().Get("card_no")
				}
			}
			if cardNo == "" && imagePath != "" {
				parts := strings.Split(imagePath, "/")
				filename := parts[len(parts)-1]
				cardNo = strings.TrimSuffix(strings.TrimSuffix(filename, ".png"), ".webp")
			}
			if cardNo == "" {
				return true
			}

			visualID := cardNo
			if _, exists := seen[visualID]; exists {
				return true
			}
			seen[visualID] = struct{}{}

			collectorNumber := visualID
			if idx := strings.Index(collectorNumber, "_"); idx != -1 {
				collectorNumber = collectorNumber[:idx]
			}

			name := strings.TrimSpace(img.AttrOr("alt", ""))
			rarity := ""

			// If alt is generic like "CARD" or empty, fetch the card detail page
			if strings.EqualFold(name, "CARD") || name == "" {
				if detailHref != "" {
					detailURL := resolveURL(seriesURL, detailHref)
					detailDoc, _, err := getDocument(ctx, p.HTTP, detailURL, catalog.FetchRequest{})
					if err == nil && detailDoc != nil {
						dn := strings.TrimSpace(detailDoc.Find(".cardName").First().Text())
						if dn != "" {
							name = dn
						}
						spans := detailDoc.Find(".infoCol span")
						if spans.Length() > 1 {
							rarity = strings.TrimSpace(spans.Eq(1).Text())
						}
					}
				}
			}
			if name == "" || strings.EqualFold(name, "CARD") {
				name = collectorNumber
			}

			variant := "Normal"
			if strings.Contains(visualID, "_p") {
				variant = "Parallel"
			}

			setCode := item.Code
			if setCode == "" || setCode == item.ID {
				if idx := strings.Index(collectorNumber, "-"); idx != -1 {
					setCode = collectorNumber[:idx]
				} else {
					setCode = "BSS"
				}
			}

			record := catalog.CardRecord{
				SourceCardID:    visualID,
				Name:            name,
				SetCode:         setCode,
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
				SetCode:          setCode,
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
		if parseErr != nil {
			return result, parseErr
		}
	}

	return result, nil
}
