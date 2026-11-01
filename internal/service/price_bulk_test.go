package service

import (
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestScryfallBulkSourceStreamsPrices(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/bulk-data":
			_, _ = writer.Write([]byte(`{"data":[{"type":"oracle_cards","jsonl_download_uri":"` + server.URL + `/oracle.jsonl.gz"},` +
				`{"type":"default_cards","jsonl_download_uri":"` + server.URL + `/default.jsonl.gz"}]}`))
		case "/default.jsonl.gz":
			writer.Header().Set("Content-Type", "application/gzip")
			gz := gzip.NewWriter(writer)
			_, _ = gz.Write([]byte(`{"id":"card-a","prices":{"usd":"1.25","eur":"1.10"}}
{"id":"card-b","prices":{"usd":null,"eur":"2.00"}}
{"id":"card-c","prices":{"usd":null,"eur":null}}
not-json
`))
			_ = gz.Close()
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	source := NewScryfallBulkSource().(*scryfallBulkSource)
	source.BaseURL = server.URL
	source.HTTP = server.Client()

	var updates []BulkPriceUpdate
	if err := source.FetchPrices(context.Background(), func(update BulkPriceUpdate) error {
		updates = append(updates, update)
		return nil
	}); err != nil {
		t.Fatalf("FetchPrices: %v", err)
	}
	if len(updates) != 2 {
		t.Fatalf("updates = %d, want 2: %+v", len(updates), updates)
	}
	if updates[0].SourceCardID != "card-a" || updates[0].USD != 1.25 || updates[0].EUR != 1.10 {
		t.Fatalf("first update = %+v", updates[0])
	}
	if updates[1].SourceCardID != "card-b" || updates[1].USD != 0 || updates[1].EUR != 2.00 {
		t.Fatalf("second update = %+v", updates[1])
	}
}

func TestYGOPRODeckBulkSourceReadsPrices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/cardinfo.php" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte(`{"data":[
{"id":1234,"card_prices":[{"cardmarket_price":"0.02","tcgplayer_price":"0.33"}]},
{"id":5678,"card_prices":[{"cardmarket_price":"0.00","tcgplayer_price":"0.00"}]}
]}`))
	}))
	defer server.Close()

	source := NewYGOPRODeckBulkSource().(*ygoprodeckBulkSource)
	source.BaseURL = server.URL
	source.HTTP = server.Client()

	var updates []BulkPriceUpdate
	if err := source.FetchPrices(context.Background(), func(update BulkPriceUpdate) error {
		updates = append(updates, update)
		return nil
	}); err != nil {
		t.Fatalf("FetchPrices: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("updates = %d, want 1 (cards without a price are skipped)", len(updates))
	}
	if updates[0].SourceCardID != "1234" || updates[0].USD != 0.33 || updates[0].EUR != 0.02 {
		t.Fatalf("update = %+v", updates[0])
	}
}

func TestScryfallBulkSourceReportsMissingManifestEntry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"data":[{"type":"oracle_cards","jsonl_download_uri":"x"}]}`))
	}))
	defer server.Close()

	source := NewScryfallBulkSource().(*scryfallBulkSource)
	source.BaseURL = server.URL
	source.HTTP = server.Client()
	if err := source.FetchPrices(context.Background(), func(BulkPriceUpdate) error { return nil }); err == nil {
		t.Fatal("expected an error when default_cards is missing")
	}
}
