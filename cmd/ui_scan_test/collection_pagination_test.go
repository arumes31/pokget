package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"pokget/internal/models"

	"github.com/chromedp/chromedp"
	"github.com/shopspring/decimal"
)

// The browser fixture models paged responses; handler tests cover actual SQL,
// authorization, global totals, and page-boundary normalization.
func collectionFixturePage(request *http.Request, original map[string]any, card models.Card) map[string]any {
	var itemsKey string
	switch request.URL.Path {
	case "/dashboard", "/vault/test":
		itemsKey = "Portfolio"
	case "/binders/test":
		itemsKey = "Cards"
	default:
		return original
	}
	data := make(map[string]any, len(original)+3)
	for key, value := range original {
		data[key] = value
	}
	items, _ := original[itemsKey].([]models.PortfolioItem)
	state, _ := request.Cookie("ui-fixture-collection")
	if request.URL.Query().Get("collection") == "1" || state != nil && state.Value == "1" {
		items = make([]models.PortfolioItem, 50)
		for index := range items {
			entryCard := card
			entryCard.ID = fmt.Sprintf("catalog-%02d", index+1)
			entryCard.Name = fmt.Sprintf("Collection card %02d", index+1)
			items[index] = models.PortfolioItem{ID: fmt.Sprintf("item-%02d", index+1), BinderID: "test", Card: entryCard, Condition: "NM", IsPublic: true}
		}
	}
	page, _ := strconv.Atoi(request.URL.Query().Get("page"))
	page = min(max(1, page), max(1, (len(items)+23)/24))
	start, end := min((page-1)*24, len(items)), min(page*24, len(items))
	pageURL := func(number int) string {
		values := url.Values{"page": {strconv.Itoa(number)}}
		switch request.URL.Path {
		case "/dashboard":
			values.Set("view", "home")
		case "/binders/test":
			values.Set("view", "binders")
			values.Set("binder", "test")
		default:
			return "/vault/test?" + values.Encode()
		}
		return "/?" + values.Encode()
	}
	fragmentURL := func(number int) string { return request.URL.Path + "?page=" + strconv.Itoa(number) }
	pagination := map[string]any{"Page": page, "PreviousURL": "", "NextURL": "", "PreviousFragmentURL": "", "NextFragmentURL": ""}
	if page > 1 {
		pagination["PreviousURL"] = pageURL(page - 1)
		pagination["PreviousFragmentURL"] = fragmentURL(page - 1)
	}
	if end < len(items) {
		pagination["NextURL"] = pageURL(page + 1)
		pagination["NextFragmentURL"] = fragmentURL(page + 1)
	}
	priced := 0
	for _, item := range items {
		if item.CustomPrice != nil || item.Card.PriceEURValid {
			priced++
		}
	}
	data[itemsKey], data["TotalCards"], data["PricedCardCount"], data["Pagination"] = items[start:end], len(items), priced, pagination
	return data
}

func pricingFixturePage(request *http.Request, original map[string]any, card models.Card) map[string]any {
	state, _ := request.Cookie("ui-fixture-collection")
	if state == nil || state.Value != "prices" {
		return original
	}
	data := make(map[string]any, len(original))
	for key, value := range original {
		data[key] = value
	}
	items := make([]models.PortfolioItem, 3)
	zero := 0.0
	for index, prefix := range []string{"Unpriced card", "Known zero card", "Custom zero card"} {
		entryCard := card
		entryCard.ID = fmt.Sprintf("price-%d", index)
		entryCard.Name = prefix + " — Scarlet and Violet special illustration rare Japanese collector edition with commemorative stamped artwork"
		entryCard.PriceEUR, entryCard.PriceUSD = decimal.Zero, decimal.Zero
		entryCard.PriceEURValid, entryCard.PriceUSDValid = index == 1, index == 1
		items[index] = models.PortfolioItem{ID: fmt.Sprintf("price-item-%d", index), BinderID: "test", Card: entryCard, Condition: "NM", IsPublic: true}
		if index == 2 {
			items[index].CustomPrice = &zero
		}
	}
	switch request.URL.Path {
	case "/dashboard", "/vault/test":
		data["Portfolio"] = items
		data["TotalValuation"] = 0.0
	case "/binders/test":
		data["Cards"] = items
	case "/wantlist":
		grails := make([]map[string]any, 2)
		for index, item := range items[:2] {
			grails[index] = map[string]any{"ID": item.ID, "Card": item.Card, "TargetPrice": 20.0, "PriceEUR": 0.0, "PriceUSD": 0.0, "ProgressEUR": 0.0, "ProgressUSD": 0.0}
		}
		data["Items"] = grails
	}
	return data
}

func TestMobileCollectionsPaginateWithoutLosingTotals(t *testing.T) {
	for _, collection := range []struct{ name, route string }{
		{"vault", "/dashboard"}, {"binder", "/binders/test"}, {"public-vault", "/vault/test"},
	} {
		t.Run(collection.name, func(t *testing.T) {
			ctx := mobileHardeningPage(t, "/?filled=1&collection=1")
			if err := loadCollectionRoute(ctx, collection.route); err != nil {
				t.Fatal(err)
			}
			for page := 1; page <= 3; page++ {
				wantCount := 24
				if page == 3 {
					wantCount = 2
				}
				if err := chromedp.Run(ctx, chromedp.Poll(fmt.Sprintf(`document.querySelectorAll('[data-collection-item]').length===%d && document.body.innerText.includes('Collection card %02d')`, wantCount, (page-1)*24+1), nil, chromedp.WithPollingTimeout(3*time.Second))); err != nil {
					t.Fatalf("page%d does not render its bounded collection slice: %v", page, err)
				}
				var total string
				if err := chromedp.Run(ctx, chromedp.Text(`[data-collection-total]`, &total, chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
				if fields := strings.Fields(total); len(fields) == 0 || fields[0] != "50" {
					t.Errorf("page%d displays page size instead of collection total: %q", page, total)
				}
				if page < 3 {
					if err := clickCollectionPage(ctx, collection.name == "public-vault", "next"); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := clickCollectionPage(ctx, collection.name == "public-vault", "prev"); err != nil {
				t.Fatal(err)
			}
			if err := chromedp.Run(ctx,
				chromedp.Poll(`document.querySelectorAll('[data-collection-item]').length===24 && document.body.innerText.includes('Collection card 25')`, nil, chromedp.WithPollingTimeout(3*time.Second)),
			); err != nil {
				t.Fatalf("previous page did not return to the second collection slice: %v", err)
			}
		})
	}
}

func loadCollectionRoute(ctx context.Context, route string) error {
	if !strings.HasPrefix(route, "/vault/") {
		return chromedp.Run(ctx, loadReviewRoute(route))
	}
	var current string
	if err := chromedp.Run(ctx, chromedp.Location(&current)); err != nil {
		return err
	}
	base, err := url.Parse(current)
	if err != nil {
		return err
	}
	return chromedp.Run(ctx, chromedp.Navigate(base.Scheme+"://"+base.Host+route), chromedp.WaitVisible("main", chromedp.ByQuery))
}

func clickCollectionPage(ctx context.Context, standalone bool, relation string) error {
	action := chromedp.Click(`nav[aria-label="Collection pages"] a[rel="`+relation+`"]`, chromedp.ByQuery)
	if standalone {
		_, err := chromedp.RunResponse(ctx, action)
		return err
	}
	return chromedp.Run(ctx, action)
}

func TestMobileCollectionNamesAndMissingPricesRemainClear(t *testing.T) {
	for _, page := range []struct{ name, route string }{
		{"vault", "/dashboard"}, {"binder", "/binders/test"}, {"public-vault", "/vault/test"}, {"grails", "/wantlist"},
	} {
		t.Run(page.name, func(t *testing.T) {
			ctx := mobileHardeningPage(t, "/?filled=1&collection=prices")
			if err := loadCollectionRoute(ctx, page.route); err != nil {
				t.Fatal(err)
			}
			for _, font := range []int{16, 32} {
				var cards []struct {
					Name, Text, WhiteSpace                 string
					Width, ScrollWidth, Height, LineHeight float64
				}
				if err := chromedp.Run(ctx, chromedp.EmulateViewport(320, 844),
					chromedp.Evaluate(fmt.Sprintf(`document.documentElement.style.setProperty('font-size','%dpx','important')`, font), nil),
					chromedp.Evaluate(`(() => {return [...document.querySelectorAll('h3')].filter(e=>/^(Unpriced|Known zero|Custom zero) card/.test(e.textContent.trim())).map(e=>{const style=getComputedStyle(e); return {Name:e.textContent.trim(),Text:e.closest('[data-collection-item],article,.card').innerText,WhiteSpace:style.whiteSpace,Width:e.clientWidth,ScrollWidth:e.scrollWidth,Height:e.clientHeight,LineHeight:parseFloat(style.lineHeight)}})})()`, &cards),
				); err != nil {
					t.Fatal(err)
				}
				if len(cards) < 2 {
					t.Fatalf("price fixture is missing its contrasting cards: %+v", cards)
				}
				for _, card := range cards {
					compactText := strings.Join(strings.Fields(card.Text), "")
					if card.WhiteSpace == "nowrap" || card.ScrollWidth > card.Width+1 || card.Height < card.LineHeight*2 {
						t.Errorf("full name cannot be read at %dpx base font: %+v", font, card)
					}
					if strings.HasPrefix(card.Name, "Unpriced") {
						if !strings.Contains(card.Text, "Price unavailable") || strings.Contains(compactText, "€0.00") || strings.Contains(card.Text, "Market / target") {
							t.Errorf("missing market data is presented as a zero or ratio: %s", card.Text)
						}
					} else if strings.HasPrefix(card.Name, "Known zero") || strings.HasPrefix(card.Name, "Custom zero") && page.name != "public-vault" {
						if strings.Contains(card.Text, "Price unavailable") || !strings.Contains(compactText, "€0.00") {
							t.Errorf("explicit zero value was treated as missing: %s", card.Text)
						}
					}
				}
			}
		})
	}
}
