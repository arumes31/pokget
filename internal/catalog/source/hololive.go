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

// HololiveProvider imports cards for the hololive Official Card Game (by Cover Corp & Bushiroad).
type HololiveProvider struct {
	HTTP     HTTPOptions
	BaseURL  string
	MaxPages int
}

func (p *HololiveProvider) ID() string         { return "hololive" }
func (p *HololiveProvider) Game() catalog.Game { return catalog.GameHololive }

var holoMaxPagePattern = regexp.MustCompile(`max_page\s*=\s*(\d+)`)

func (p *HololiveProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://hololive-official-cardgame.com"
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
		return result, fmt.Errorf("hololive: serialize first page: %w", err)
	}
	pageCount := 1
	if match := holoMaxPagePattern.FindStringSubmatch(html); len(match) == 2 {
		pageCount, _ = strconv.Atoi(match[1])
	}
	if pageCount < 1 {
		return result, fmt.Errorf("hololive: invalid page count")
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
	if err := p.emitHoloPage(document, firstURL, seen, emit, &result); err != nil {
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
				return result, fmt.Errorf("hololive page %d: %w", page, res.err)
			}
			return result, fmt.Errorf("hololive page %d: missing page response", page)
		}
		if err := p.emitHoloPage(res.doc, res.url, seen, emit, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (p *HololiveProvider) emitHoloPage(document *goquery.Document, pageURL string, seen map[string]struct{}, emit func(catalog.CardRecord) error, result *catalog.FetchResult) error {
	var emitErr error
	document.Find("li").EachWithBreak(func(_ int, item *goquery.Selection) bool {
		href, _ := item.Find(`a[href*="cardlist"]`).First().Attr("href")
		cardNumber := strings.TrimSpace(item.Find(".number").First().Text())
		if cardNumber == "" && href != "" {
			if parsed, err := url.Parse(resolveURL(pageURL, href)); err == nil {
				cardNumber = strings.TrimSpace(parsed.Query().Get("id"))
			}
		}
		if cardNumber == "" {
			return true
		}
		if _, exists := seen[cardNumber]; exists {
			return true
		}
		seen[cardNumber] = struct{}{}

		name := strings.TrimSpace(item.Find(".name").First().Text())
		if name == "" {
			name = strings.TrimSpace(item.Find(".ttl").First().Text())
		}
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
		rarity := holoDefinitionValue(item, "レアリティ")
		if rarity == "" {
			rarity = "Normal"
		}

		setCode := holoSetCode(cardNumber)
		record := catalog.CardRecord{
			SourceCardID:    cardNumber,
			Name:            name,
			SetCode:         setCode,
			SetName:         setCode,
			CollectorNumber: cardNumber,
			Language:        "ja",
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
			Language:         "ja",
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

func holoDefinitionValue(item *goquery.Selection, label string) string {
	value := ""
	item.Find("dl").EachWithBreak(func(_ int, definition *goquery.Selection) bool {
		definition.Find("dt").EachWithBreak(func(i int, dt *goquery.Selection) bool {
			if strings.TrimSpace(dt.Text()) == label {
				value = strings.TrimSpace(dt.NextFiltered("dd").Text())
				return false
			}
			return true
		})
		return value == ""
	})
	return value
}

func holoSetCode(cardNumber string) string {
	if idx := strings.Index(cardNumber, "-"); idx > 0 {
		return cardNumber[:idx]
	}
	return "hololive"
}
