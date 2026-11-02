package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTCGCSVBulkSourceEmitsCategoryPrices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/1/groups":
			_, _ = writer.Write([]byte(`{"results":[{"groupId":77,"abbreviation":"OP-01","name":"One Piece OP-01"}]}`))
		case "/1/77/products":
			_, _ = writer.Write([]byte(`{"results":[
				{"productId":1001,"extendedData":[{"name":"Number","value":"016"}]},
				{"productId":1002,"extendedData":[{"name":"Number","value":"017"}]}
			]}`))
		case "/1/77/prices":
			_, _ = writer.Write([]byte(`{"results":[{"productId":1001,"marketPrice":2.5},{"productId":1002,"marketPrice":0}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	source := NewTCGCSVBulkSource().(*tcgcsvBulkSource)
	source.BaseURL = server.URL
	source.HTTP = server.Client()
	source.Categories = map[string]int{"one_piece": 1}
	source.Concurrency = 1

	var updates []BulkPriceUpdate
	if err := source.FetchPrices(context.Background(), func(update BulkPriceUpdate) error {
		updates = append(updates, update)
		return nil
	}); err != nil {
		t.Fatalf("FetchPrices: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("updates = %d, want 1 (zero prices are skipped)", len(updates))
	}
	update := updates[0]
	if update.SourceID != "tcgcsv" || update.SourceCardID != "1001" || update.Game != "one_piece" {
		t.Fatalf("update = %+v", update)
	}
	if update.SetCode != "OP-01" || update.Number != "016" || update.USD != 2.5 {
		t.Fatalf("update = %+v", update)
	}
}

func TestTCGCSVBulkSourceReportsGroupErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/1/groups" {
			_, _ = writer.Write([]byte(`{"results":[{"groupId":77,"abbreviation":"OP-01"}]}`))
			return
		}
		http.Error(writer, "broken", http.StatusInternalServerError)
	}))
	defer server.Close()

	source := NewTCGCSVBulkSource().(*tcgcsvBulkSource)
	source.BaseURL = server.URL
	source.HTTP = server.Client()
	source.Categories = map[string]int{"one_piece": 1}
	source.Concurrency = 1
	if err := source.FetchPrices(context.Background(), func(BulkPriceUpdate) error { return nil }); err == nil {
		t.Fatal("expected an error for a broken group endpoint")
	}
}
