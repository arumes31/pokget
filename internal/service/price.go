// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pokget/internal/models"

	"github.com/chromedp/chromedp"
	"github.com/gocolly/colly/v2"
)

// PriceClient defines the interface for fetching market data
type PriceClient interface {
	FetchPrice(card models.Card) (usd float64, eur float64, err error)
	ApplyMultiplier(price float64, condition string, multipliers map[string]float64) float64
}

// MockPriceClient for testing
type MockPriceClient struct {
	FixedUSD float64
	FixedEUR float64
	Err      error
}

func (m *MockPriceClient) FetchPrice(_ models.Card) (float64, float64, error) {
	return m.FixedUSD, m.FixedEUR, m.Err
}

func (m *MockPriceClient) ApplyMultiplier(price float64, _ string, _ map[string]float64) float64 {
	return price
}

// MockMailer for testing
type MockMailer struct {
	Err error
}

func (m *MockMailer) SendConfirmationEmail(_, _ string) error {
	return m.Err
}

// MockLLMClient for testing
type MockLLMClient struct {
	Response string
	Err      error
}

func (m *MockLLMClient) FuzzyMatchCard(_ string, _ []models.Card) (string, error) {
	return m.Response, m.Err
}

var scraperUserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.0.0 Safari/537.36",
}

// CardmarketScraper handles scraping logic for Cardmarket.com
type CardmarketScraper struct {
	BaseURL string
}

// parseCardmarketPrice parses a Cardmarket price string written in the German
// locale (e.g. "0,30 €" or "1.234,56 €") into a float64. The '.' character is a
// thousands separator and ',' is the decimal separator, so a naive comma->dot
// replacement corrupts any price >= 1000. This strips the currency symbol and
// any grouping dots before parsing.
func parseCardmarketPrice(text string) (float64, error) {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9', r == '.', r == ',':
			return r
		default:
			return -1
		}
	}, text)

	if strings.Contains(cleaned, ",") {
		// German locale: '.' groups thousands, ',' is the decimal separator.
		cleaned = strings.ReplaceAll(cleaned, ".", "")
		cleaned = strings.ReplaceAll(cleaned, ",", ".")
	} else if isGermanThousandsDot(cleaned) {
		// German locale without comma: a dot followed by exactly 3 digits
		// at the end indicates a thousands separator (e.g. "1.234" = 1234).
		cleaned = strings.ReplaceAll(cleaned, ".", "")
	}

	if cleaned == "" {
		return 0, fmt.Errorf("no numeric price found in %q", text)
	}
	return strconv.ParseFloat(cleaned, 64)
}

// isGermanThousandsDot detects whether dots in the cleaned string are
// thousands separators in German locale (e.g. "1.234" meaning 1234 EUR,
// not 1.234). A dot is a thousands separator if it is followed by exactly
// 3 digits and no comma appears after it.
func isGermanThousandsDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			// Check if there are exactly 3 digits after the dot
			remaining := s[i+1:]
			if len(remaining) == 3 && isAllDigits(remaining) {
				return true
			}
			// Also match patterns like "1.234.567" where each group is 3 digits
			if len(remaining) > 3 && isAllDigits(remaining[:3]) && (remaining[3] == '.' || remaining[3] == ',') {
				return true
			}
		}
	}
	return false
}

func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Scrape fetches the current price from Cardmarket
func (s *CardmarketScraper) Scrape(card models.Card) (float64, error) {
	return s.ScrapeWithRetry(card, 3)
}

// BUG-M10 FIX: ScrapeWithRetry implements exponential backoff with retry
// for 429 (Too Many Requests) responses. Previously, the price sync service
// didn't handle 429 status codes, potentially getting IP-banned by the API.
// Now, on 429 responses, it waits with exponential backoff before retrying.
func (s *CardmarketScraper) ScrapeWithRetry(card models.Card, maxRetries int) (float64, error) {
	var eur float64
	var scrapeErr error
	var found bool
	var got429 bool

	c := colly.NewCollector()
	c.SetRequestTimeout(15 * time.Second)
	if err := c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 2,
		Delay:       2 * time.Second,
	}); err != nil {
		slog.Error("CardmarketScraper: Failed to set limit rule", "error", err)
	}
	// ⚡ Bolt: Use package-level scraperUserAgents to avoid slice allocation on every call.
	c.UserAgent = scraperUserAgents[time.Now().UnixNano()%int64(len(scraperUserAgents))]

	var gameSegment string
	switch strings.ToLower(card.Game) {
	case "one piece":
		gameSegment = "One-Piece-Card-Game"
	case "lorcana":
		gameSegment = "Lorcana"
	case "weiss schwarz":
		gameSegment = "Weiss-Schwarz"
	case "magic", "mtg":
		gameSegment = "Magic-The-Gathering"
	default:
		gameSegment = "Pokemon"
	}

	cmURL := fmt.Sprintf("%s/en/%s/Products/Singles/%s/%s",
		s.BaseURL,
		gameSegment,
		url.PathEscape(strings.ReplaceAll(card.Set, " ", "-")),
		url.PathEscape(strings.ReplaceAll(card.Name, " ", "-")))

	slog.Info("CardmarketScraper: Scraping", "url", cmURL)

	c.OnHTML(".price-container .color-primary", func(e *colly.HTMLElement) {
		parsed, err := parseCardmarketPrice(e.Text)
		if err != nil {
			slog.Error("CardmarketScraper: Failed to parse price", "text", e.Text, "error", err)
			scrapeErr = err
			return
		}
		eur = parsed
		found = true
	})

	c.OnError(func(r *colly.Response, err error) {
		if r != nil {
			// BUG-M10 FIX: Detect 429 Too Many Requests and signal retry
			if r.StatusCode == 429 {
				got429 = true
				retryAfter := r.Headers.Get("Retry-After")
				slog.Warn("CardmarketScraper: Rate limited (429)", "url", cmURL, "retry_after", retryAfter)
				return
			}
			if r.Request != nil {
				slog.Error("CardmarketScraper: Request failed", "url", r.Request.URL, "status", r.StatusCode, "error", err)
			}
		} else {
			slog.Error("CardmarketScraper: Request failed", "error", err)
		}
		scrapeErr = err
	})

	if err := c.Visit(cmURL); err != nil {
		slog.Error("CardmarketScraper: Failed to visit URL", "url", cmURL, "error", err)
		scrapeErr = err
	}

	// BUG-M10 FIX: Handle 429 with exponential backoff retry
	if got429 && maxRetries > 0 {
		backoff := 5 * time.Second * time.Duration(4-maxRetries) // Exponential: 5s, 10s, 15s
		slog.Warn("CardmarketScraper: Rate limited, backing off before retry", "card", card.Name, "backoff", backoff, "retries_left", maxRetries-1)
		time.Sleep(backoff)
		return s.ScrapeWithRetry(card, maxRetries-1)
	}

	// If the request succeeded but no price element matched, surface an error
	// instead of silently returning 0 (which would otherwise overwrite a valid
	// stored price with zero downstream).
	if scrapeErr == nil && !found {
		scrapeErr = fmt.Errorf("price element not found for %q", card.Name)
	}

	return eur, scrapeErr
}

// TCGPlayerScraper handles headless scraping for TCGPlayer.com
type TCGPlayerScraper struct{}

// Scrape fetches the current price from TCGPlayer using headless browser
func (s *TCGPlayerScraper) Scrape(card models.Card) (float64, error) {
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var priceStr string
	var targetURL string

	switch strings.ToLower(card.Game) {
	case "pokemon":
		targetURL = fmt.Sprintf("https://www.tcgplayer.com/search/pokemon/product?q=%s", url.QueryEscape(card.Name))
	case "one piece":
		targetURL = fmt.Sprintf("https://www.tcgplayer.com/search/one-piece-card-game/product?q=%s", url.QueryEscape(card.Name))
	case "lorcana":
		targetURL = fmt.Sprintf("https://www.tcgplayer.com/search/lorcana/product?q=%s", url.QueryEscape(card.Name))
	case "weiss schwarz":
		targetURL = fmt.Sprintf("https://www.tcgplayer.com/search/weiss-schwarz/product?q=%s", url.QueryEscape(card.Name))
	case "magic", "mtg":
		targetURL = fmt.Sprintf("https://www.tcgplayer.com/search/magic/product?q=%s", url.QueryEscape(card.Name))
	default:
		return 0, fmt.Errorf("unsupported game for headless scrape: %s", card.Game)
	}

	err := chromedp.Run(ctx,
		chromedp.Navigate(targetURL),
		chromedp.WaitVisible(`.search-result__market-price--value`, chromedp.ByQuery),
		chromedp.Text(`.search-result__market-price--value`, &priceStr, chromedp.ByQuery),
	)

	if err != nil {
		return 0, err
	}

	priceStr = strings.TrimPrefix(priceStr, "$")
	return strconv.ParseFloat(priceStr, 64)
}

// ScraperPriceClient fetches prices via Web Scraping (No API key needed)
// It uses specialized scrapers for different markets.
type ScraperPriceClient struct {
	Cardmarket *CardmarketScraper
	TCGPlayer  *TCGPlayerScraper
}

// NewScraperPriceClient initializes a new ScraperPriceClient with its sub-scrapers.
func NewScraperPriceClient() *ScraperPriceClient {
	return &ScraperPriceClient{
		Cardmarket: &CardmarketScraper{
			BaseURL: "https://www.cardmarket.com",
		},
		TCGPlayer: &TCGPlayerScraper{},
	}
}

// FetchPrice fetches prices from multiple sources and returns them.
func (s *ScraperPriceClient) FetchPrice(card models.Card) (float64, float64, error) {
	if s.Cardmarket == nil || s.TCGPlayer == nil {
		return 0, 0, fmt.Errorf("scraper client not properly initialized")
	}

	eur, scrapeErr := s.Cardmarket.Scrape(card)

	var usd float64
	// TCGPlayer Scrape (USD)
	usd, err := s.TCGPlayer.Scrape(card)
	if err != nil {
		slog.Warn("Scraper: TCGPlayer scrape failed", "card", card.Name, "error", err)
	}

	return usd, eur, scrapeErr
}

func (s *ScraperPriceClient) ApplyMultiplier(price float64, condition string, multipliers map[string]float64) float64 {
	if multipliers == nil {
		// Default multipliers
		multipliers = map[string]float64{
			"NM":  1.0,
			"LP":  0.9,
			"MP":  0.7,
			"HP":  0.5,
			"DMG": 0.3,
		}
	}

	m, ok := multipliers[condition]
	if !ok {
		return price
	}
	return price * m
}

// DefaultPriceClient for production (can choose between Scraper or API)
type DefaultPriceClient struct {
	Scraper *ScraperPriceClient
}

func (d *DefaultPriceClient) FetchPrice(card models.Card) (float64, float64, error) {
	if d.Scraper == nil {
		return 0, 0, fmt.Errorf("nil ScraperPriceClient")
	}
	return d.Scraper.FetchPrice(card)
}

func (d *DefaultPriceClient) ApplyMultiplier(price float64, condition string, multipliers map[string]float64) float64 {
	if d.Scraper == nil {
		return price
	}
	return d.Scraper.ApplyMultiplier(price, condition, multipliers)
}
