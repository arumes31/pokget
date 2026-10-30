package source

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"pokget/internal/catalog"
)

// GundamOfficialProvider ingests Gundam Card Game cards from Bandai's official website.
type GundamOfficialProvider struct {
	HTTP    HTTPOptions
	BaseURL string
}

func (p *GundamOfficialProvider) ID() string         { return "gundam_official" }
func (p *GundamOfficialProvider) Game() catalog.Game { return catalog.GameGundam }

type gundamPackage struct {
	ID   string
	Name string
	Code string
}

func (p *GundamOfficialProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://www.gundam-gcg.com/en/cards"
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

	packages := make([]gundamPackage, 0, 64)
	document.Find(`.js-selectBtn-package[data-val], select[name="package"] option[value]`).Each(func(_ int, sel *goquery.Selection) {
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
		packages = append(packages, gundamPackage{ID: val, Name: name, Code: code})
	})
	// Fallback if cards are directly on the page (e.g. single package mock in tests)
	if len(packages) == 0 && document.Find("li.cardItem").Length() > 0 {
		packages = append(packages, gundamPackage{ID: "", Name: "Gundam Card Game", Code: "GD01"})
	}
	if len(packages) == 0 {
		return result, fmt.Errorf("gundam official: no package options found; upstream layout may have changed")
	}

	seen := make(map[string]struct{})
	for index, item := range packages {
		if index > 0 {
			if err := waitRequestDelay(ctx, p.HTTP.RequestDelay); err != nil {
				return result, err
			}
		}

		page := document
		packageURL := rootURL
		if item.ID != "" {
			packageURL = baseURL + "/index.php?package=" + url.QueryEscape(item.ID)
			p, _, err := getDocument(ctx, p.HTTP, packageURL, catalog.FetchRequest{})
			if err != nil {
				return result, fmt.Errorf("gundam official package %s: %w", item.ID, err)
			}
			page = p
		}

		var parseErr error
		page.Find("li.cardItem").EachWithBreak(func(_ int, card *goquery.Selection) bool {
			a := card.Find("a.cardStr")
			img := card.Find("img")
			name := strings.TrimSpace(img.AttrOr("alt", ""))

			imagePath, _ := img.Attr("data-src")
			if imagePath == "" {
				imagePath, _ = img.Attr("src")
			}
			imageURL := resolveURL(packageURL, imagePath)

			dataSrc, _ := a.Attr("data-src")
			if dataSrc == "" {
				dataSrc, _ = a.Attr("href")
			}

			// Extract card identifier from data-src (e.g. detail.php?detailSearch=GD01-001 or GD01-001_p1)
			cardID := ""
			if dataSrc != "" {
				if u, err := url.Parse(dataSrc); err == nil {
					cardID = u.Query().Get("detailSearch")
					if cardID == "" {
						cardID = u.Query().Get("card_no")
					}
				}
			}
			if cardID == "" {
				// Fallback: parse from image URL filename
				if imagePath != "" {
					parts := strings.Split(imagePath, "/")
					filename := parts[len(parts)-1]
					cardID = strings.TrimSuffix(strings.TrimSuffix(filename, ".webp"), ".png")
					if idx := strings.Index(cardID, "?"); idx != -1 {
						cardID = cardID[:idx]
					}
				}
			}
			if cardID == "" {
				return true
			}

			visualID := cardID
			if _, exists := seen[visualID]; exists {
				return true
			}
			seen[visualID] = struct{}{}

			collectorNumber := visualID
			if idx := strings.Index(collectorNumber, "_"); idx != -1 {
				collectorNumber = collectorNumber[:idx]
			}

			if name == "" {
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
					setCode = "GD"
				}
			}

			record := catalog.CardRecord{
				SourceCardID:    visualID,
				Name:            name,
				SetCode:         setCode,
				SetName:         item.Name,
				CollectorNumber: collectorNumber,
				Language:        "en",
				Metadata:        rawMetadata(map[string]string{"package_id": item.ID, "visual_id": visualID}),
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
