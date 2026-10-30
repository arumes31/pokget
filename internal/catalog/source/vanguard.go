package source

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"pokget/internal/catalog"
)

// VanguardProvider imports cards for Cardfight!! Vanguard (by Bushiroad).
type VanguardProvider struct {
	HTTP     HTTPOptions
	BaseURL  string
	MaxPages int
}

func (p *VanguardProvider) ID() string         { return "cardfight_vanguard" }
func (p *VanguardProvider) Game() catalog.Game { return catalog.GameCardfightVanguard }

var vgMaxPagePattern = regexp.MustCompile(`max_page\s*=\s*(\d+)`)

func (p *VanguardProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://en.cf-vanguard.com"
	}
	firstURL := baseURL + "/cardlist/cardsearch/"
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
		return result, fmt.Errorf("vanguard: serialize first page: %w", err)
	}
	pageCount := 1
	if match := vgMaxPagePattern.FindStringSubmatch(html); len(match) == 2 {
		pageCount, _ = strconv.Atoi(match[1])
	}
	if pageCount < 1 {
		return result, fmt.Errorf("vanguard: invalid page count")
	}

	maxPages := p.MaxPages
	if maxPages <= 0 && request.Mode == catalog.SyncModeIncremental {
		maxPages = 25
	}
	if maxPages > 0 && pageCount > maxPages {
		pageCount = maxPages
		result.CompleteSnapshot = false
	}

	seen := make(map[string]struct{})
	if err := p.emitVGPage(document, firstURL, seen, emit, &result); err != nil {
		return result, err
	}
	if pageCount <= 1 {
		return result, nil
	}

	type pageResult struct {
		page int
		doc  *goquery.Document
		url  string
		err  error
	}

	concurrency := 5
	if pageCount-1 < concurrency {
		concurrency = pageCount - 1
	}

	pageChan := make(chan int, pageCount-1)
	resChan := make(chan pageResult, pageCount-1)
	var wg sync.WaitGroup

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for page := range pageChan {
				if ctx.Err() != nil {
					resChan <- pageResult{page: page, err: ctx.Err()}
					continue
				}
				if p.HTTP.RequestDelay > 0 {
					_ = waitRequestDelay(ctx, p.HTTP.RequestDelay)
				}
				endpoint := fmt.Sprintf("%s/cardlist/cardsearch_ex?view=text&page=%d", baseURL, page)
				doc, _, err := getDocument(ctx, p.HTTP, endpoint, catalog.FetchRequest{})
				resChan <- pageResult{page: page, doc: doc, url: endpoint, err: err}
			}
		}()
	}

	for page := 2; page <= pageCount; page++ {
		pageChan <- page
	}
	close(pageChan)

	go func() {
		wg.Wait()
		close(resChan)
	}()

	pageDocs := make(map[int]pageResult, pageCount-1)
	for res := range resChan {
		pageDocs[res.page] = res
	}

	for page := 2; page <= pageCount; page++ {
		res, exists := pageDocs[page]
		if !exists || res.err != nil {
			if res.err != nil {
				return result, fmt.Errorf("vanguard page %d: %w", page, res.err)
			}
			return result, fmt.Errorf("vanguard page %d: missing page response", page)
		}
		if err := p.emitVGPage(res.doc, res.url, seen, emit, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (p *VanguardProvider) emitVGPage(document *goquery.Document, pageURL string, seen map[string]struct{}, emit func(catalog.CardRecord) error, result *catalog.FetchResult) error {
	var emitErr error
	document.Find("li").EachWithBreak(func(_ int, item *goquery.Selection) bool {
		href, _ := item.Find(`a[href*="cardno="]`).First().Attr("href")
		cardNumber := strings.TrimSpace(item.Find(".number").First().Text())
		if cardNumber == "" && href != "" {
			if parsed, err := url.Parse(resolveURL(pageURL, href)); err == nil {
				cardNumber = strings.TrimSpace(parsed.Query().Get("cardno"))
			}
		}
		if cardNumber == "" {
			return true
		}
		if _, exists := seen[cardNumber]; exists {
			return true
		}
		seen[cardNumber] = struct{}{}

		name := strings.TrimSpace(item.Find("h5").First().Text())
		if name == "" {
			name, _ = item.Find("img").First().Attr("title")
		}
		if name == "" {
			name, _ = item.Find("img").First().Attr("alt")
		}
		if name == "" {
			name = cardNumber
		}

		imagePath, _ := item.Find("img").First().Attr("src")
		rarity := extractVGRarity(item.Find(".status").Text())

		setCode := vgSetCode(cardNumber)
		record := catalog.CardRecord{
			SourceCardID:    cardNumber,
			Name:            name,
			SetCode:         setCode,
			SetName:         setCode,
			CollectorNumber: cardNumber,
			Language:        "en",
			Rarity:          rarity,
			Metadata:        rawMetadata(map[string]string{"detail_href": href}),
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

func extractVGRarity(statusText string) string {
	parts := strings.Split(statusText, "｜")
	for _, part := range parts {
		p := strings.TrimSpace(part)
		for _, r := range []string{"SEC", "FFR", "FR", "DSR", "SP", "RRR", "RR", "R", "C", "PR", "ORR", "SR", "Re"} {
			if strings.EqualFold(p, r) {
				return r
			}
		}
	}
	return "Normal"
}

func vgSetCode(cardNumber string) string {
	if idx := strings.Index(cardNumber, "/"); idx > 0 {
		return cardNumber[:idx]
	}
	if idx := strings.Index(cardNumber, "-"); idx > 0 {
		return cardNumber[:idx]
	}
	return "Vanguard"
}
