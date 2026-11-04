package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"pokget/internal/catalog"
)

func TestYGOPRODeckAttachesPrintingScan(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ygopro/checkDBVer.php":
			_, _ = w.Write([]byte(`[{"database_version":"1","last_update":"2026-01-01"}]`))
		case "/ygopro/cardinfo.php":
			_, _ = w.Write([]byte(`{"data":[{"id":123,"name":"Aqua Madoor",` +
				`"card_sets":[{"set_name":"Legend of Blue Eyes","set_code":"LOB-027","set_rarity":"Rare"}],` +
				`"card_images":[{"id":123,"image_url":"https://cdn.example/ygopro.jpg"}]}]}`))
		case "/tcg/2/groups":
			_, _ = w.Write([]byte(`{"results":[{"groupId":330,"abbreviation":"LOB"}]}`))
		case "/tcg/2/330/products":
			_, _ = w.Write([]byte(`{"results":[{"imageUrl":"https://cdn.example/scan_200w.jpg",` +
				`"extendedData":[{"name":"Number","value":"LOB-027"}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := &YGOPRODeckProvider{
		BaseURL:       server.URL + "/ygopro",
		TCGCSVBaseURL: server.URL + "/tcg",
	}
	var records []catalog.CardRecord
	if _, err := provider.Fetch(context.Background(), catalog.FetchRequest{}, func(record catalog.CardRecord) error {
		records = append(records, record)
		return nil
	}); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one record, got %d", len(records))
	}
	if len(records[0].Images) < 2 {
		t.Fatalf("expected the printing scan plus the generic artwork, got %d images", len(records[0].Images))
	}
	if got := records[0].Images[0].URL; got != "https://cdn.example/scan_in_1000x1000.jpg" {
		t.Errorf("primary image = %q, want the per-printing scan", got)
	}
}
