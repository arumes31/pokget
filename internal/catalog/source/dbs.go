package source

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"pokget/internal/catalog"
)

// DBSMastersOfficialProvider ingests Dragon Ball Super Card Game (Masters)
// cards from Bandai's official website.
type DBSMastersOfficialProvider struct {
	HTTP    HTTPOptions
	BaseURL string
}

func (p *DBSMastersOfficialProvider) ID() string         { return "dbs_official" }
func (p *DBSMastersOfficialProvider) Game() catalog.Game { return catalog.GameDragonBallSuper }

type dbsSeries struct {
	ID   string
	Name string
	Code string
}

func (p *DBSMastersOfficialProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://www.dbs-cardgame.com/us-en/cardlist"
	}
	rootURL := baseURL + "/?search=true"
	document, meta, err := getDocument(ctx, p.HTTP, rootURL, request)
	if err != nil {
		return catalog.FetchResult{}, err
	}
	result := catalog.FetchResult{ETag: meta.ETag, LastModified: meta.LastModified, CompleteSnapshot: true, NotModified: meta.NotModified}
	if meta.NotModified {
		return result, nil
	}

	series := make([]dbsSeries, 0, 64)
	document.Find(`select[name="category_exp"] option[value]`).Each(func(_ int, sel *goquery.Selection) {
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
		series = append(series, dbsSeries{ID: id, Name: name, Code: code})
	})
	if len(series) == 0 {
		document.Find(`#snaviList a[href*="category="]`).Each(func(_ int, sel *goquery.Selection) {
			href, _ := sel.Attr("href")
			u, err := url.Parse(href)
			if err != nil {
				return
			}
			id := u.Query().Get("category")
			if id == "" {
				return
			}
			name := onePieceSeriesDisplayName(sel.Text())
			code := id
			if match := bracketedSetCode.FindStringSubmatch(name); len(match) == 2 {
				code = match[1]
			}
			series = append(series, dbsSeries{ID: id, Name: name, Code: code})
		})
	}
	// Fallback if cards are already on the current page (e.g. single set mock in tests)
	if len(series) == 0 && document.Find("dl.cardListCol").Length() > 0 {
		series = append(series, dbsSeries{ID: "", Name: "Dragon Ball Super Card Game", Code: "DBS"})
	}
	if len(series) == 0 {
		return result, fmt.Errorf("dbs official: no series options found; upstream layout may have changed")
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
			seriesURL = baseURL + "/?search=true&category=" + url.QueryEscape(item.ID)
			p, _, err := getDocument(ctx, p.HTTP, seriesURL, catalog.FetchRequest{})
			if err != nil {
				return result, fmt.Errorf("dbs official series %s: %w", item.ID, err)
			}
			page = p
		}

		var parseErr error
		page.Find("dl.cardListCol").EachWithBreak(func(_ int, card *goquery.Selection) bool {
			cardNum := strings.TrimSpace(card.Find("dt.cardNumber").First().Text())
			if cardNum == "" {
				return true
			}
			name := strings.TrimSpace(card.Find("dd.cardName").First().Text())
			if name == "" {
				return true
			}
			visualID := cardNum
			if _, exists := seen[visualID]; exists {
				return true
			}
			seen[visualID] = struct{}{}

			imagePath, _ := card.Find(".cardimg img").First().Attr("src")
			imageURL := resolveURL(seriesURL, imagePath)
			rarity := strings.TrimSpace(card.Find("dl.rarityCol dd").First().Text())
			seriesName := strings.TrimSpace(card.Find("dl.seriesCol dd").First().Text())
			if seriesName == "" {
				seriesName = item.Name
			}

			setCode := item.Code
			if setCode == "" || setCode == item.ID {
				if idx := strings.Index(cardNum, "-"); idx != -1 {
					setCode = cardNum[:idx]
				} else {
					setCode = "DBS"
				}
			}

			collectorNumber := cardNum
			if idx := strings.Index(collectorNumber, "_"); idx != -1 {
				collectorNumber = collectorNumber[:idx]
			}

			variant := "Normal"
			if strings.Contains(cardNum, "_") {
				variant = "Parallel"
			}

			record := catalog.CardRecord{
				SourceCardID:    visualID,
				Name:            name,
				SetCode:         setCode,
				SetName:         seriesName,
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
				SetName:          seriesName,
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

// DBSFusionWorldOfficialProvider ingests Dragon Ball Super Card Game Fusion World
// cards from Bandai's official website.
type DBSFusionWorldOfficialProvider struct {
	HTTP    HTTPOptions
	BaseURL string
}

func (p *DBSFusionWorldOfficialProvider) ID() string         { return "dbs_fusion_world_official" }
func (p *DBSFusionWorldOfficialProvider) Game() catalog.Game { return catalog.GameDragonBallFusionWorld }

func (p *DBSFusionWorldOfficialProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://www.dbs-cardgame.com/fw/en/cardlist"
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

	categories := make([]dbsSeries, 0, 64)
	document.Find(`.filterListItems li a[data-val], select[name="category[]"] option[value]`).Each(func(_ int, sel *goquery.Selection) {
		val, _ := sel.Attr("data-val")
		if val == "" {
			val, _ = sel.Attr("value")
		}
		val = strings.TrimSpace(val)
		if val == "" || strings.EqualFold(val, "ALL") {
			return
		}
		name := onePieceSeriesDisplayName(sel.Text())
		code := val
		if match := bracketedSetCode.FindStringSubmatch(name); len(match) == 2 {
			code = match[1]
		}
		categories = append(categories, dbsSeries{ID: val, Name: name, Code: code})
	})
	// Fallback if cards are directly on the page
	if len(categories) == 0 && document.Find("li.cardItem").Length() > 0 {
		categories = append(categories, dbsSeries{ID: "", Name: "Fusion World", Code: "FB01"})
	}
	if len(categories) == 0 {
		return result, fmt.Errorf("dbs fusion world: no category options found; upstream layout may have changed")
	}

	seen := make(map[string]struct{})
	for index, item := range categories {
		if index > 0 {
			if err := waitRequestDelay(ctx, p.HTTP.RequestDelay); err != nil {
				return result, err
			}
		}

		page := document
		pageURL := rootURL
		if item.ID != "" {
			pageURL = baseURL + "/?search=true&category%5B0%5D=" + url.QueryEscape(item.ID)
			p, _, err := getDocument(ctx, p.HTTP, pageURL, catalog.FetchRequest{})
			if err != nil {
				return result, fmt.Errorf("dbs fusion world category %s: %w", item.ID, err)
			}
			page = p
		}

		var parseErr error
		page.Find("li.cardItem").EachWithBreak(func(_ int, card *goquery.Selection) bool {
			a := card.Find("a.cardStr")
			img := card.Find("img")
			alt, _ := img.Attr("alt")
			alt = strings.TrimSpace(alt)

			imagePath, _ := img.Attr("data-src")
			if imagePath == "" {
				imagePath, _ = img.Attr("src")
			}
			imageURL := resolveURL(pageURL, imagePath)

			detailHref, _ := a.Attr("data-src")
			if detailHref == "" {
				detailHref, _ = a.Attr("href")
			}

			// Extract collector number and name from alt (e.g. "FB11-123 Bardock")
			collectorNumber := ""
			name := ""
			if alt != "" {
				parts := strings.SplitN(alt, " ", 2)
				collectorNumber = parts[0]
				if len(parts) > 1 {
					name = strings.TrimSpace(parts[1])
				}
			}

			// If alt didn't supply number, check detailHref (e.g. detail.php?card_no=FB11-123)
			pSuffix := ""
			if detailHref != "" {
				u, err := url.Parse(detailHref)
				if err == nil {
					q := u.Query()
					if cn := q.Get("card_no"); cn != "" && collectorNumber == "" {
						collectorNumber = cn
					}
					pSuffix = q.Get("p")
				}
			}
			if collectorNumber == "" {
				return true
			}
			if name == "" {
				name = collectorNumber
			}

			visualID := collectorNumber
			if pSuffix != "" && !strings.HasSuffix(visualID, pSuffix) {
				visualID += pSuffix
			}
			if _, exists := seen[visualID]; exists {
				return true
			}
			seen[visualID] = struct{}{}

			setCode := item.Code
			if setCode == "" || setCode == item.ID {
				if idx := strings.Index(collectorNumber, "-"); idx != -1 {
					setCode = collectorNumber[:idx]
				} else {
					setCode = "FB"
				}
			}

			variant := "Normal"
			if pSuffix != "" || strings.Contains(visualID, "_p") {
				variant = "Parallel"
			}

			record := catalog.CardRecord{
				SourceCardID:    visualID,
				Name:            name,
				SetCode:         setCode,
				SetName:         item.Name,
				CollectorNumber: collectorNumber,
				Language:        "en",
				Metadata:        rawMetadata(map[string]string{"category_id": item.ID, "visual_id": visualID}),
			}
			if imageURL != "" {
				record.Images = []catalog.ImageRecord{{SourceImageID: visualID + ":front", Kind: "front", URL: imageURL}}
			}
			record.Printings = []catalog.PrintingRecord{{
				SourcePrintingID: visualID,
				SetCode:          setCode,
				SetName:          item.Name,
				CollectorNumber:  collectorNumber,
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
