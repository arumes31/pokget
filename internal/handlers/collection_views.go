package handlers

import (
	"net/http"
	"net/url"
	"strconv"

	"pokget/internal/models"

	"github.com/shopspring/decimal"
)

const collectionPageSize = 24

type collectionPagination struct {
	Page                                 int
	PreviousURL, NextURL                 string
	PreviousFragmentURL, NextFragmentURL string
}

func requestedCollectionPage(r *http.Request) int {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		return 1
	}
	return page
}

func boundedCollectionPage(r *http.Request, total int) int {
	lastPage := 1
	if total > 0 {
		lastPage = (total-1)/collectionPageSize + 1
	}
	return min(requestedCollectionPage(r), lastPage)
}

func collectionPageLinks(page int, hasNext bool, path, fragment string, query url.Values) collectionPagination {
	links := collectionPagination{Page: page}
	pageURLs := func(number int) (string, string) {
		query.Set("page", strconv.Itoa(number))
		pageURL := path + "?" + query.Encode()
		fragmentURL := ""
		if fragment != "" {
			fragmentURL = fragment + "?page=" + strconv.Itoa(number)
		}
		return pageURL, fragmentURL
	}
	if page > 1 {
		links.PreviousURL, links.PreviousFragmentURL = pageURLs(page - 1)
	}
	if hasNext {
		links.NextURL, links.NextFragmentURL = pageURLs(page + 1)
	}
	return links
}

func setCardMarketPrices(card *models.Card, usd, eur decimal.NullDecimal) {
	card.PriceUSD, card.PriceUSDValid = usd.Decimal, usd.Valid
	card.PriceEUR, card.PriceEURValid = eur.Decimal, eur.Valid
}
