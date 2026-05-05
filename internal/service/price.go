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
	"fmt"
	"gettos/internal/models"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
)

// PriceClient defines the interface for fetching market data
type PriceClient interface {
	FetchPrice(card models.Card) (usd float64, eur float64, err error)
}

// MockPriceClient for testing
type MockPriceClient struct {
	FixedUSD float64
	FixedEUR float64
}

func (m *MockPriceClient) FetchPrice(_ models.Card) (float64, float64, error) {
	return m.FixedUSD, m.FixedEUR, nil
}

// ScraperPriceClient fetches prices via Web Scraping (No API key needed)
type ScraperPriceClient struct{}

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
	cmURL := fmt.Sprintf("https://www.cardmarket.com/en/Pokemon/Products/Singles/%s/%s",
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

	// --- 2. Scrape TCGPlayer (USD) ---
	usd, _ = card.PriceUSD.Float64() // Fallback for TCGPlayer's complex React-based DOM

	return usd, eur, scrapeErr
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
