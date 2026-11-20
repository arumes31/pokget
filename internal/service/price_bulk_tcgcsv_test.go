package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
	source.LanguageCategories = nil
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
	// The group name backs the fallback match for games whose catalog set codes
	// differ from the TCGplayer abbreviations.
	if update.SetName != "One Piece OP-01" {
		t.Fatalf("SetName = %q, want the TCGCSV group name", update.SetName)
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
	source.LanguageCategories = nil
	source.Concurrency = 1
	if err := source.FetchPrices(context.Background(), func(BulkPriceUpdate) error { return nil }); err == nil {
		t.Fatal("expected an error for a broken group endpoint")
	}
}

// One broken category must not cost the others their prices: the good category's
// rows are delivered and the failure is still returned.
func TestTCGCSVBulkSourceContinuesPastAFailingCategory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/85/groups":
			_, _ = writer.Write([]byte(`{"results":[{"groupId":7,"abbreviation":"SV3"}]}`))
		case "/85/7/products":
			_, _ = writer.Write([]byte(`{"results":[{"productId":1,"extendedData":[{"name":"Number","value":"001"}]}]}`))
		case "/85/7/prices":
			_, _ = writer.Write([]byte(`{"results":[{"productId":1,"marketPrice":0.4}]}`))
		default:
			http.Error(writer, "broken", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	source := NewTCGCSVBulkSource().(*tcgcsvBulkSource)
	source.BaseURL = server.URL
	source.HTTP = server.Client()
	source.Categories = map[string]int{"one_piece": 1}
	source.LanguageCategories = []tcgcsvLanguageCategory{{Game: "pokemon", Languages: []string{"ja"}, Category: 85}}
	source.Concurrency = 1

	var updates []BulkPriceUpdate
	err := source.FetchPrices(context.Background(), func(update BulkPriceUpdate) error {
		updates = append(updates, update)
		return nil
	})
	if err == nil {
		t.Fatal("expected the failing category to be reported")
	}
	if len(updates) != 1 || updates[0].Game != "pokemon" {
		t.Fatalf("updates = %+v, want the healthy category's row", updates)
	}
}

// A failed write is not a broken set: it must reach the caller even when other
// groups of the category succeed.
func TestTCGCSVBulkSourceSurfacesEmitErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/1/groups":
			_, _ = writer.Write([]byte(`{"results":[{"groupId":7},{"groupId":8}]}`))
		case "/1/7/products", "/1/8/products":
			_, _ = writer.Write([]byte(`{"results":[{"productId":1}]}`))
		case "/1/7/prices", "/1/8/prices":
			_, _ = writer.Write([]byte(`{"results":[{"productId":1,"marketPrice":0.4}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	source := NewTCGCSVBulkSource().(*tcgcsvBulkSource)
	source.BaseURL = server.URL
	source.HTTP = server.Client()
	source.Categories = map[string]int{"one_piece": 1}
	source.LanguageCategories = nil
	source.Concurrency = 1

	calls := 0
	err := source.FetchPrices(context.Background(), func(BulkPriceUpdate) error {
		calls++
		if calls == 1 {
			return errors.New("database down")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "database down") {
		t.Fatalf("err = %v, want the emit error", err)
	}
}

// TCGplayer keeps Japanese Pokémon in its own category, so those rows must carry
// the language to keep them off the English printings.
func TestTCGCSVBulkSourceTagsLanguageCategories(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/85/groups":
			_, _ = writer.Write([]byte(`{"results":[{"groupId":23609,"abbreviation":"SV3","name":"SV3: Ruler of the Black Flame"}]}`))
		case "/85/23609/products":
			_, _ = writer.Write([]byte(`{"results":[{"productId":566955,"name":"Oddish","extendedData":[{"name":"Number","value":"001/108"}]}]}`))
		case "/85/23609/prices":
			_, _ = writer.Write([]byte(`{"results":[{"productId":566955,"marketPrice":0.42,"subTypeName":"Normal"}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	source := NewTCGCSVBulkSource().(*tcgcsvBulkSource)
	source.BaseURL = server.URL
	source.HTTP = server.Client()
	source.Categories = map[string]int{}
	source.Concurrency = 1

	var updates []BulkPriceUpdate
	if err := source.FetchPrices(context.Background(), func(update BulkPriceUpdate) error {
		updates = append(updates, update)
		return nil
	}); err != nil {
		t.Fatalf("FetchPrices: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(updates))
	}
	update := updates[0]
	if update.Game != "pokemon" || len(update.Languages) != 4 {
		t.Fatalf("update = %+v, want the Asian Pokémon languages", update)
	}
	if update.Languages[0] != "ja" || update.Languages[1] != "ko" {
		t.Fatalf("Languages = %v", update.Languages)
	}
	if update.SetCode != "SV3" || update.Number != "001/108" || update.USD != 0.42 {
		t.Fatalf("update = %+v", update)
	}
	if update.ProductName != "Oddish" {
		t.Fatalf("ProductName = %q, want the marketplace card name", update.ProductName)
	}
}
