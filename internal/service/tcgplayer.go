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
	"net/url"
	"strconv"
	"strings"
	"time"

	"pokget/internal/models"

	"github.com/chromedp/chromedp"
)

// TCGPlayerScraper handles headless scraping for TCGPlayer prices
type TCGPlayerScraper struct{}

// NewTCGPlayerScraper creates a new instance of TCGPlayerScraper
func NewTCGPlayerScraper() *TCGPlayerScraper {
	return &TCGPlayerScraper{}
}

// FetchPrice retrieves the market price from TCGPlayer using a headless browser
func (s *TCGPlayerScraper) FetchPrice(card models.Card) (float64, error) {
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
