package source

import (
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pokget/internal/catalog"
)

func TestTCGdexProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/en/sets":
			_, _ = w.Write([]byte(`[{"id":"set1","name":"Set One"}]`))
		case "/en/sets/set1":
			_, _ = w.Write([]byte(`{"id":"set1","name":"Set One","releaseDate":"2026-01-02","cards":[{"id":"set1-7","localId":"7","name":"Example","image":"https://images.example/card"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := &TCGdexProvider{HTTP: testHTTP(server), BaseURL: server.URL, Language: "en"}
	records, result := fetchRecords(t, provider)
	if result.Count != 1 || len(records) != 1 {
		t.Fatalf("count = %d, records = %d, want 1", result.Count, len(records))
	}
	if records[0].SourceCardID != "set1-7" || records[0].SetName != "Set One" {
		t.Fatalf("unexpected record: %+v", records[0])
	}
	if got := records[0].Images[0].URL; got != "https://images.example/card/high.webp" {
		t.Fatalf("image URL = %q", got)
	}
}

func TestLorcastProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sets":
			_, _ = w.Write([]byte(`{"results":[{"id":"s1","code":"1","name":"First","released_at":"2023-08-18"}]}`))
		case "/sets/1/cards":
			_, _ = w.Write([]byte(`[{"id":"c1","name":"Elsa","version":"Spirit","released_at":"2023-08-18","collector_number":"207","lang":"en","rarity":"Legendary","set":{"id":"s1","code":"1","name":"First"},"image_uris":{"digital":{"normal":"https://images.example/c1.avif"}}}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	records, _ := fetchRecords(t, &LorcastProvider{HTTP: testHTTP(server), BaseURL: server.URL})
	if len(records) != 1 || records[0].Name != "Elsa Spirit" || records[0].CollectorNumber != "207" {
		t.Fatalf("unexpected records: %+v", records)
	}
}

func TestLorcanaJSONProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/en/allCards.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"metadata":{"formatVersion":"2.3.5","generatedOn":"2026-08-02T19:16:26","language":"en"},"sets":{"1":{"name":"The First Chapter","releaseDate":"2023-09-01"}},"cards":[{"id":42,"fullName":"Elsa - Spirit of Winter","number":42,"setCode":"1","rarity":"Legendary","images":{"full":"https://images.example/elsa.jpg"}}]}`))
	}))
	defer server.Close()

	records, result := fetchRecords(t, &LorcanaJSONProvider{HTTP: testHTTP(server), BaseURL: server.URL, Language: "en"})
	if len(records) != 1 || records[0].Name != "Elsa - Spirit of Winter" || records[0].Images[0].URL != "https://images.example/elsa.jpg" {
		t.Fatalf("unexpected records: %+v", records)
	}
	if result.UpstreamVersion != "2.3.5:2026-08-02T19:16:26" {
		t.Fatalf("unexpected version: %q", result.UpstreamVersion)
	}
}

func TestYGOPRODeckProviderKeepsArtAndPrintingsUnlinked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checkDBVer.php":
			_, _ = w.Write([]byte(`[{"database_version":"1.2","last_update":"2026-01-02"}]`))
		case "/cardinfo.php":
			_, _ = w.Write([]byte(`{"data":[{"id":42,"name":"Dragon","card_sets":[{"set_name":"Tin","set_code":"TIN-001","set_rarity":"Rare"}],"card_images":[{"id":42,"image_url":"https://images.example/42.jpg"},{"id":43,"image_url":"https://images.example/43.jpg"}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	records, result := fetchRecords(t, &YGOPRODeckProvider{HTTP: testHTTP(server), BaseURL: server.URL})
	if result.UpstreamVersion != "1.2:2026-01-02" || len(records) != 1 {
		t.Fatalf("unexpected result: %+v records=%d", result, len(records))
	}
	if len(records[0].Images) != 2 || len(records[0].Printings) != 1 {
		t.Fatalf("unexpected YGO record: %+v", records[0])
	}
	if len(records[0].Printings[0].SourceImageIDs) != 0 {
		t.Fatal("YGOPRODeck does not provide an artwork-to-printing mapping")
	}
}

func TestOPTCGAPIProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/allSetCards/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"card_name":"Luffy","set_name":"Romance Dawn","set_id":"OP-01","rarity":"L","card_set_id":"OP01-001","card_image_id":"OP01-001_p1","card_image":"https://images.example/op.png","date_scraped":"2026-01-02"}]`))
	}))
	defer server.Close()

	records, _ := fetchRecords(t, &OPTCGAPIProvider{HTTP: testHTTP(server), BaseURL: server.URL, Endpoints: []string{"allSetCards"}})
	if len(records) != 1 || records[0].SourceCardID != "OP01-001_p1" {
		t.Fatalf("unexpected records: %+v", records)
	}
}

func TestScryfallProviderStreamsGzipJSONLAndFiltersDigital(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bulk-data":
			_, _ = fmt.Fprintf(w, `{"data":[{"type":"all_cards","updated_at":"v1","jsonl_download_uri":%q}]}`, server.URL+"/cards.jsonl.gz")
		case "/cards.jsonl.gz":
			writer := gzip.NewWriter(w)
			_, _ = writer.Write([]byte("{\"id\":\"paper\",\"name\":\"Lotus\",\"lang\":\"en\",\"set\":\"vma\",\"set_name\":\"Vintage\",\"collector_number\":\"4\",\"rarity\":\"rare\",\"released_at\":\"2020-01-01\",\"games\":[\"paper\"],\"image_uris\":{\"normal\":\"https://images.example/lotus.jpg\"}}\n"))
			_, _ = writer.Write([]byte("{\"id\":\"digital\",\"name\":\"Digital\",\"lang\":\"en\",\"set\":\"x\",\"set_name\":\"Digital\",\"collector_number\":\"1\",\"games\":[\"arena\"]}\n"))
			_ = writer.Close()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	records, result := fetchRecords(t, &ScryfallProvider{HTTP: testHTTP(server), ManifestURL: server.URL + "/bulk-data"})
	if result.Count != 1 || result.UpstreamVersion != "v1" || records[0].SourceCardID != "paper" {
		t.Fatalf("unexpected result=%+v records=%+v", result, records)
	}
}

func TestOnePieceOfficialProviderParsesAlternateArt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("series") == "1" {
			_, _ = w.Write([]byte(`<dl class="modalCol" id="ST01-001_p1"><dt><div class="infoCol"><span>ST01-001</span><span>L</span></div><div class="cardName">Luffy</div></dt><dd><div class="frontCol"><img data-src="/images/ST01-001_p1.png"></div></dd></dl>`))
			return
		}
		_, _ = w.Write([]byte(`<select name="series"><option value="">Recording</option><option value="1">Starter Deck [ST-01]</option></select>`))
	}))
	defer server.Close()

	records, _ := fetchRecords(t, &OnePieceOfficialProvider{HTTP: testHTTP(server), BaseURL: server.URL})
	if len(records) != 1 || records[0].SourceCardID != "ST01-001_p1" || records[0].SetCode != "ST-01" {
		t.Fatalf("unexpected records: %+v", records)
	}
	if records[0].Images[0].URL != server.URL+"/images/ST01-001_p1.png" {
		t.Fatalf("unexpected image URL: %q", records[0].Images[0].URL)
	}
}

func TestWeissProviderPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		card := func(number, name string) string {
			return fmt.Sprintf(`<li><a href="/cardlist/?cardno=%s"><img src="/images/%s.png"></a><p class="number">%s</p><p class="ttl">%s</p><dl><dt>Rarity</dt><dd>RR</dd></dl></li>`, number, number, number, name)
		}
		if strings.Contains(r.URL.Path, "cardsearch_ex") {
			_, _ = w.Write([]byte(card("SET/W01-002", "Second")))
			return
		}
		_, _ = w.Write([]byte(`<script>var max_page = 2;</script>` + card("SET/W01-001", "First")))
	}))
	defer server.Close()

	records, result := fetchRecords(t, &WeissProvider{HTTP: testHTTP(server), BaseURL: server.URL})
	if result.Count != 2 || len(records) != 2 || records[1].SourceCardID != "SET/W01-002" {
		t.Fatalf("unexpected result=%+v records=%+v", result, records)
	}
}

func TestProviderConditionalNotModified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != `"v1"` {
			t.Errorf("If-None-Match = %q", r.Header.Get("If-None-Match"))
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	provider := &TCGdexProvider{HTTP: testHTTP(server), BaseURL: server.URL, Language: "en"}
	result, err := provider.Fetch(context.Background(), catalog.FetchRequest{ETag: `"v1"`}, func(catalog.CardRecord) error { return nil })
	if err != nil || !result.NotModified {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func testHTTP(server *httptest.Server) HTTPOptions {
	return HTTPOptions{Client: server.Client(), UserAgent: "pokget-test/1.0", MaxBodyBytes: 4 << 20}
}

func fetchRecords(t *testing.T, provider catalog.Provider) ([]catalog.CardRecord, catalog.FetchResult) {
	t.Helper()
	records := make([]catalog.CardRecord, 0)
	result, err := provider.Fetch(context.Background(), catalog.FetchRequest{Mode: catalog.SyncModeFull}, func(record catalog.CardRecord) error {
		records = append(records, record)
		return nil
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	return records, result
}
