package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pokget/internal/models"
)

func TestParsePriceAcceptsUSAndGermanNotation(t *testing.T) {
	cases := map[string]float64{
		"$1,299.99":  1299.99,
		"$219.14":    219.14,
		"1.299,99 €": 1299.99,
		"0,30 €":     0.3,
		"2,55 €":     2.55,
		"1,234":      1234,
		"1.234":      1234,
		"17.97":      17.97,
	}
	for input, want := range cases {
		got, err := ParsePrice(input)
		if err != nil {
			t.Fatalf("ParsePrice(%q) error: %v", input, err)
		}
		if got != want {
			t.Fatalf("ParsePrice(%q) = %v, want %v", input, got, want)
		}
	}
	if _, err := ParsePrice("not a price"); err == nil {
		t.Fatal("ParsePrice accepted a value without digits")
	}
}

func TestTCGdexPriceClientReadsBothMarkets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/en/cards/swsh1-1" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte(`{"pricing":{"cardmarket":{"unit":"EUR","avg":2.55,"low":0.5},"tcgplayer":{"unit":"USD","market":3.1,"avg":3.0}}}`))
	}))
	defer server.Close()

	client := &TCGdexPriceClient{BaseURL: server.URL, Language: "en", HTTP: server.Client()}
	usd, eur, err := client.FetchPrice(models.Card{Name: "Celebi V", SourceID: "tcgdex", SourceCardID: "swsh1-1"})
	if err != nil {
		t.Fatalf("FetchPrice: %v", err)
	}
	if usd != 3.1 || eur != 2.55 {
		t.Fatalf("usd/eur = %v/%v, want 3.1/2.55", usd, eur)
	}

	if _, _, err := client.FetchPrice(models.Card{Name: "Celebi V"}); err == nil {
		t.Fatal("expected an error for a card without a TCGdex id")
	}
	if _, _, err := client.FetchPrice(models.Card{Name: "Celebi V", SourceID: "scryfall", SourceCardID: "swsh1-1"}); err == nil {
		t.Fatal("expected an error for a card from another catalog source")
	}
}

func TestTCGdexPriceClientResolvesLegacyCardBySearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/en/cards":
			if request.URL.Query().Get("name") != "Alakazam" {
				http.NotFound(writer, request)
				return
			}
			_, _ = writer.Write([]byte(`[{"id":"base5-1","localId":"1","name":"Dark Alakazam"},{"id":"base1-1","localId":"1","name":"Alakazam"}]`))
		case "/en/cards/base1-1":
			_, _ = writer.Write([]byte(`{"pricing":{"cardmarket":{"unit":"EUR","avg":48.3,"low":10},"tcgplayer":{"unit":"USD","holofoil":{"marketPrice":120.5}}}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := &TCGdexPriceClient{BaseURL: server.URL, Language: "en", HTTP: server.Client()}
	// Legacy row: no source id at all, resolved through the name search.
	usd, eur, err := client.FetchPrice(models.Card{Name: "Alakazam", CollectorNumber: "1"})
	if err != nil {
		t.Fatalf("FetchPrice: %v", err)
	}
	if usd != 120.5 || eur != 48.3 {
		t.Fatalf("usd/eur = %v/%v, want 120.5/48.3", usd, eur)
	}
}

func TestScryfallPriceClientParsesEurAndUsd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/cards/named" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte(`{"prices":{"usd":"0.85","eur":"1.91"}}`))
	}))
	defer server.Close()

	client := &ScryfallPriceClient{BaseURL: server.URL, HTTP: server.Client()}
	usd, eur, err := client.FetchPrice(models.Card{Name: "Lightning Bolt"})
	if err != nil {
		t.Fatalf("FetchPrice: %v", err)
	}
	if usd != 0.85 || eur != 1.91 {
		t.Fatalf("usd/eur = %v/%v, want 0.85/1.91", usd, eur)
	}
}

func TestYGOPRODeckPriceClientParsesPrices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/cardinfo.php" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte(`{"data":[{"name":"Dark Magician","card_prices":[{"cardmarket_price":"0.02","tcgplayer_price":"0.33"}]}]}`))
	}))
	defer server.Close()

	client := &YGOPRODeckPriceClient{BaseURL: server.URL, HTTP: server.Client()}
	usd, eur, err := client.FetchPrice(models.Card{Name: "Dark Magician"})
	if err != nil {
		t.Fatalf("FetchPrice: %v", err)
	}
	if usd != 0.33 || eur != 0.02 {
		t.Fatalf("usd/eur = %v/%v, want 0.33/0.02", usd, eur)
	}
}

func TestPriceClientsReportMissingPrices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer server.Close()

	if _, _, err := (&ScryfallPriceClient{BaseURL: server.URL, HTTP: server.Client()}).FetchPrice(models.Card{Name: "Unknown"}); err == nil {
		t.Fatal("scryfall accepted an empty payload")
	}
	if _, _, err := (&YGOPRODeckPriceClient{BaseURL: server.URL, HTTP: server.Client()}).FetchPrice(models.Card{Name: "Unknown"}); err == nil {
		t.Fatal("ygoprodeck accepted an empty payload")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (&TCGdexPriceClient{BaseURL: server.URL, HTTP: server.Client()}).FetchPriceContext(ctx, models.Card{SourceCardID: "swsh1-1"}); err == nil {
		t.Fatal("tcgdex ignored a cancelled context")
	}
}

func TestCardmarketProductURLHandlesMissingSet(t *testing.T) {
	withSet := cardmarketProductURL("https://www.cardmarket.com/", "Pokemon", models.Card{Name: "Charizard", Set: "Base Set"})
	if withSet != "https://www.cardmarket.com/en/Pokemon/Products/Singles/Base-Set/Charizard" {
		t.Fatalf("product url = %q", withSet)
	}

	withoutSet := cardmarketProductURL("https://www.cardmarket.com", "Pokemon", models.Card{Name: "Charizard"})
	if withoutSet != "https://www.cardmarket.com/en/Pokemon/Products/Search?searchString=Charizard" {
		t.Fatalf("search url = %q", withoutSet)
	}
	if strings.Contains(withoutSet, "Singles//") {
		t.Fatalf("search url contains an empty set segment: %q", withoutSet)
	}
}

func TestTCGPlayerQueriesPreferPreciseSearch(t *testing.T) {
	queries := tcgPlayerQueries(models.Card{Name: "Charizard", SetCode: "BS", CollectorNumber: "4"})
	want := []string{"Charizard BS 4", "Charizard BS", "Charizard"}
	if len(queries) != len(want) {
		t.Fatalf("queries = %v, want %v", queries, want)
	}
	for index := range want {
		if queries[index] != want[index] {
			t.Fatalf("queries[%d] = %q, want %q", index, queries[index], want[index])
		}
	}

	if got := tcgPlayerQueries(models.Card{Name: "Charizard"}); len(got) != 1 || got[0] != "Charizard" {
		t.Fatalf("queries without set data = %v", got)
	}
	if got := tcgPlayerQueries(models.Card{}); got != nil {
		t.Fatalf("queries without a name = %v", got)
	}
}
