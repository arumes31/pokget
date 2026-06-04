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

// ScraperPriceClient fetches prices via Web Scraping (No API key needed)
type ScraperPriceClient struct {
	BaseURL string
}

func NewScraperPriceClient() *ScraperPriceClient {
	return &ScraperPriceClient{
		BaseURL: "https://www.cardmarket.com",
	}
}

func (s *ScraperPriceClient) FetchPrice(card models.Card) (float64, float64, error) {
	var usd, eur float64
	var scrapeErr error

	// User Agent Rotation for Resilience
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Mobile/15E148 Safari/604.1",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.0.0 Safari/537.36",
	}

	c := colly.NewCollector()
	c.SetRequestTimeout(15 * time.Second)
	if err := c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 2,
		Delay:       2 * time.Second,
	}); err != nil {
		slog.Error("Scraper: Failed to set limit rule", "error", err)
	}
	c.UserAgent = userAgents[time.Now().UnixNano()%int64(len(userAgents))]

	// --- 1. Scrape Cardmarket (EUR) ---
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

	slog.Info("Worker: Scraping Cardmarket", "url", cmURL)

	c.OnHTML(".price-container .color-primary", func(e *colly.HTMLElement) {
		val := strings.Trim(e.Text, " €")
		val = strings.Replace(val, ",", ".", -1)
		parsed, err := strconv.ParseFloat(val, 64)
		if err != nil {
			slog.Error("Scraper: Failed to parse price", "val", val, "error", err)
			scrapeErr = err
			return
		}
		eur = parsed
	})

	c.OnError(func(r *colly.Response, err error) {
		if r != nil && r.Request != nil {
			slog.Error("Scraper: Request failed", "url", r.Request.URL, "status", r.StatusCode, "error", err)
		} else {
			slog.Error("Scraper: Request failed", "error", err)
		}
		scrapeErr = err
	})

	if err := c.Visit(cmURL); err != nil {
		slog.Error("Scraper: Failed to visit URL", "url", cmURL, "error", err)
		scrapeErr = err
	}

	// --- 2. Headless Fallback for TCGPlayer (USD) ---
	if usd == 0 {
		headlessPrice, err := s.fetchPriceHeadless(card)
		if err == nil {
			usd = headlessPrice
		} else {
			slog.Warn("Scraper: Headless fallback failed", "card", card.Name, "error", err)
		}
	}

	return usd, eur, scrapeErr
}

func (s *ScraperPriceClient) fetchPriceHeadless(card models.Card) (float64, error) {
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
