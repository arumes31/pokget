package source

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"pokget/internal/catalog"
)

type WeissProvider struct {
	HTTP     HTTPOptions
	BaseURL  string
	MaxPages int
}

func (p *WeissProvider) ID() string         { return "weiss_official" }
func (p *WeissProvider) Game() catalog.Game { return catalog.GameWeissSchwarz }

var weissMaxPagePattern = regexp.MustCompile(`max_page\s*=\s*(\d+)`)

func (p *WeissProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://en.ws-tcg.com"
	}
	firstURL := baseURL + "/cardlist/searchresults/?sort=new"
	document, meta, err := getDocument(ctx, p.HTTP, firstURL, request)
	if err != nil {
		return catalog.FetchResult{}, err
	}
	result := catalog.FetchResult{ETag: meta.ETag, LastModified: meta.LastModified, CompleteSnapshot: true, NotModified: meta.NotModified}
	if meta.NotModified {
		return result, nil
	}
	html, err := document.Html()
	if err != nil {
		return result, fmt.Errorf("weiss: serialize first page: %w", err)
	}
	pageCount := 1
	if match := weissMaxPagePattern.FindStringSubmatch(html); len(match) == 2 {
		pageCount, _ = strconv.Atoi(match[1])
	}
	if pageCount < 1 {
		return result, fmt.Errorf("weiss: invalid page count")
	}
	if p.MaxPages > 0 && pageCount > p.MaxPages {
		pageCount = p.MaxPages
		result.CompleteSnapshot = false
	}

	seen := make(map[string]struct{})
	if err := p.emitWeissPage(document, firstURL, seen, emit, &result); err != nil {
		return result, err
	}
	for page := 2; page <= pageCount; page++ {
		if err := waitRequestDelay(ctx, p.HTTP.RequestDelay); err != nil {
			return result, err
		}
		endpoint := fmt.Sprintf("%s/cardlist/cardsearch_ex?view=text&page=%d", baseURL, page)
		document, _, err := getDocument(ctx, p.HTTP, endpoint, catalog.FetchRequest{})
		if err != nil {
			return result, fmt.Errorf("weiss page %d: %w", page, err)
		}
		if err := p.emitWeissPage(document, endpoint, seen, emit, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (p *WeissProvider) emitWeissPage(document *goquery.Document, pageURL string, seen map[string]struct{}, emit func(catalog.CardRecord) error, result *catalog.FetchResult) error {
	var emitErr error
	document.Find("li").EachWithBreak(func(_ int, item *goquery.Selection) bool {
		href, exists := item.Find(`a[href*="cardno="]`).First().Attr("href")
		if !exists {
			return true
		}
		parsed := resolveURL(pageURL, href)
		cardURL, err := url.Parse(parsed)
		if err != nil {
			return true
		}
		cardNumber := strings.TrimSpace(cardURL.Query().Get("cardno"))
		if cardNumber == "" {
			cardNumber = strings.TrimSpace(item.Find(".number").First().Text())
		}
		if cardNumber == "" {
			return true
		}
		if _, exists := seen[cardNumber]; exists {
			return true
		}
		seen[cardNumber] = struct{}{}
		name := strings.TrimSpace(item.Find(".ttl").First().Text())
		imagePath, _ := item.Find("img").First().Attr("src")
		rarity := weissDefinitionValue(item, "Rarity")
		setCode := weissSetCode(cardNumber)
		record := catalog.CardRecord{
			SourceCardID:    cardNumber,
			Name:            name,
			SetCode:         setCode,
			SetName:         setCode,
			CollectorNumber: cardNumber,
			Language:        "en",
			Rarity:          rarity,
			Metadata:        rawMetadata(map[string]string{"detail_url": parsed}),
		}
		if imageURL := resolveURL(pageURL, imagePath); imageURL != "" {
			record.Images = []catalog.ImageRecord{{SourceImageID: cardNumber + ":front", Kind: "front", URL: imageURL}}
		}
		record.Printings = []catalog.PrintingRecord{{
			SourcePrintingID: cardNumber,
			SetCode:          setCode,
			SetName:          setCode,
			CollectorNumber:  cardNumber,
			Rarity:           rarity,
			Language:         "en",
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

func weissDefinitionValue(item *goquery.Selection, label string) string {
	value := ""
	item.Find("dl").EachWithBreak(func(_ int, definition *goquery.Selection) bool {
		if strings.EqualFold(strings.TrimSpace(definition.Find("dt").Text()), label) {
			value = strings.TrimSpace(definition.Find("dd").Text())
			return false
		}
		return true
	})
	return value
}

func weissSetCode(cardNumber string) string {
	if index := strings.LastIndex(cardNumber, "-"); index > 0 {
		return cardNumber[:index]
	}
	return cardNumber
}
