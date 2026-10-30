package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"pokget/internal/catalog"
)

// UniVersusProvider imports cards for the UniVersus Collectible Card Game (by UVS Games) from UVS Ultra.
type UniVersusProvider struct {
	HTTP         HTTPOptions
	BaseURL      string
	ImageBaseURL string
	MaxPages     int
}

func (p *UniVersusProvider) ID() string         { return "universus" }
func (p *UniVersusProvider) Game() catalog.Game { return catalog.GameUniVersus }

var (
	uvsTotalCountPattern = regexp.MustCompile(`(\d+)\s*(?:cartes|Results)`)
	uvsPreviewPattern    = regexp.MustCompile(`preview2\('([^']+)',\s*'([^']+)'`)
)

func (p *UniVersusProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://uvsultra.online"
	}
	imageBaseURL := strings.TrimRight(p.ImageBaseURL, "/")
	if imageBaseURL == "" {
		imageBaseURL = baseURL + "/images/extensions"
	}

	result := catalog.FetchResult{CompleteSnapshot: true}

	doc, meta, err := p.postListingPage(ctx, baseURL, 0, request)
	if err != nil {
		return result, err
	}
	result.ETag = meta.ETag
	result.LastModified = meta.LastModified
	result.NotModified = meta.NotModified
	if meta.NotModified {
		return result, nil
	}

	html, err := doc.Html()
	if err != nil {
		return result, fmt.Errorf("universus: serialize first page: %w", err)
	}

	totalItems := 0
	if match := uvsTotalCountPattern.FindStringSubmatch(html); len(match) == 2 {
		totalItems, _ = strconv.Atoi(match[1])
	}
	if totalItems < 1 {
		totalItems = 50
	}

	totalPages := (totalItems + 49) / 50
	if p.MaxPages > 0 && totalPages > p.MaxPages {
		totalPages = p.MaxPages
		result.CompleteSnapshot = false
	} else if request.Mode == catalog.SyncModeIncremental && p.MaxPages <= 0 {
		totalPages = 5 // In incremental mode, check first 5 pages (250 cards)
		result.CompleteSnapshot = false
	}

	seen := make(map[string]struct{})
	if err := p.emitUVSPage(doc, imageBaseURL, seen, emit, &result); err != nil {
		return result, err
	}

	for page := 1; page < totalPages; page++ {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if p.HTTP.RequestDelay > 0 {
			if err := waitRequestDelay(ctx, p.HTTP.RequestDelay); err != nil {
				return result, err
			}
		}

		pageDoc, _, err := p.postListingPage(ctx, baseURL, page, catalog.FetchRequest{})
		if err != nil {
			return result, fmt.Errorf("universus page %d: %w", page, err)
		}
		if err := p.emitUVSPage(pageDoc, imageBaseURL, seen, emit, &result); err != nil {
			return result, err
		}
	}

	return result, nil
}

func (p *UniVersusProvider) postListingPage(ctx context.Context, baseURL string, page int, fetch catalog.FetchRequest) (*goquery.Document, responseMeta, error) {
	endpoint := baseURL + "/listing_cards.php"
	data := url.Values{}
	data.Set("page", strconv.Itoa(page))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, responseMeta{}, fmt.Errorf("universus: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	userAgent := p.HTTP.UserAgent
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	}
	req.Header.Set("User-Agent", userAgent)
	if fetch.ETag != "" {
		req.Header.Set("If-None-Match", fetch.ETag)
	}
	if fetch.LastModified != "" {
		req.Header.Set("If-Modified-Since", fetch.LastModified)
	}

	resp, err := p.HTTP.client().Do(req)
	if err != nil {
		return nil, responseMeta{}, fmt.Errorf("universus: POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	meta := responseMeta{
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		NotModified:  resp.StatusCode == http.StatusNotModified,
	}
	if meta.NotModified {
		return nil, meta, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return nil, meta, fmt.Errorf("universus: POST %s returned status %s", endpoint, resp.Status)
	}

	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, p.HTTP.maxBodyBytes()+1))
	if err != nil {
		return nil, meta, fmt.Errorf("universus: parse HTML: %w", err)
	}
	return doc, meta, nil
}

func (p *UniVersusProvider) emitUVSPage(doc *goquery.Document, imageBaseURL string, seen map[string]struct{}, emit func(catalog.CardRecord) error, result *catalog.FetchResult) error {
	var emitErr error
	doc.Find("div.card").EachWithBreak(func(_ int, cardDiv *goquery.Selection) bool {
		onmouse, _ := cardDiv.Find("div[onmouseover*=\"preview\"]").First().Attr("onmouseover")
		ext := ""
		num := ""
		if match := uvsPreviewPattern.FindStringSubmatch(onmouse); len(match) == 3 {
			ext = strings.TrimSpace(match[1])
			num = strings.TrimSpace(match[2])
		}

		cardID := ""
		if ext != "" && num != "" {
			cardID = fmt.Sprintf("%s-%s", ext, num)
		} else {
			cardID = strings.TrimSpace(cardDiv.Find(".card_title h1").First().Text())
		}
		if cardID == "" {
			return true
		}
		if _, exists := seen[cardID]; exists {
			return true
		}
		seen[cardID] = struct{}{}

		name := strings.TrimSpace(cardDiv.Find(".card_title h1").First().Text())
		if name == "" {
			name = cardID
		}

		importantInfo := cardDiv.Find(".card-important-info").Text()
		rarity := extractUVSRarity(importantInfo)

		setCode := strings.ToUpper(ext)
		if setCode == "" {
			setCode = "UniVersus"
		}
		setName := setCode

		record := catalog.CardRecord{
			SourceCardID:    cardID,
			Name:            name,
			SetCode:         setCode,
			SetName:         setName,
			CollectorNumber: num,
			Language:        "en",
			Rarity:          rarity,
			Metadata:        rawMetadata(map[string]string{"extension": ext, "number": num}),
		}

		if ext != "" && num != "" {
			imgURL := fmt.Sprintf("%s/%s/%s-preview.jpg", imageBaseURL, ext, num)
			record.Images = []catalog.ImageRecord{{
				SourceImageID: cardID + ":front",
				Kind:          "front",
				URL:           imgURL,
			}}
		}

		record.Printings = []catalog.PrintingRecord{{
			SourcePrintingID: cardID,
			SetCode:          setCode,
			SetName:          setName,
			CollectorNumber:  num,
			Language:         "en",
			Rarity:           rarity,
			SourceImageIDs:   imageIDs(record.Images),
		}}

		if err := record.Validate(); err != nil {
			emitErr = err
			return false
		}
		if err := emit(record); err != nil {
			emitErr = err
			return false
		}
		result.Count++
		return true
	})
	return emitErr
}

func extractUVSRarity(info string) string {
	for _, r := range []string{"Ultra Rare", "Secret Rare", "Super Rare", "Extra Rare", "Starter Exclusive", "Rare", "Uncommon", "Common", "Promo"} {
		if strings.Contains(info, r) {
			return r
		}
	}
	return "Normal"
}
