package source

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAlteredProviderEmptyNameFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/META/card_sets_en.json":
			_, _ = w.Write([]byte(`{
				"hydra:member": [
					{
						"id": "set-fugue",
						"reference": "FUGUE",
						"name": "Neverending Journey",
						"code": "NEJ"
					}
				]
			}`))
		case "/SETS/FUGUE/FUGUE_DE.json":
			_, _ = w.Write([]byte(`[
				{
					"id": "alt-card-de-1",
					"reference": "ALT_FUGUE_B_AX_133_C",
					"name": "",
					"collectorNumber": "NEJ-003-C-DE",
					"cardSet": {
						"reference": "FUGUE",
						"name": "Neverending Journey",
						"code": "NEJ"
					},
					"rarity": {
						"reference": "COMMON",
						"name": "Common"
					}
				}
			]`))
		case "/SETS/FUGUE/FUGUE_EN.json":
			_, _ = w.Write([]byte(`[
				{
					"id": "alt-card-en-1",
					"reference": "ALT_FUGUE_B_AX_133_C",
					"name": "Axiom Recoverer",
					"collectorNumber": "NEJ-003-C-EN",
					"cardSet": {
						"reference": "FUGUE",
						"name": "Neverending Journey",
						"code": "NEJ"
					},
					"rarity": {
						"reference": "COMMON",
						"name": "Common"
					}
				}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	records, result := fetchRecords(t, &AlteredProvider{HTTP: testHTTP(server), BaseURL: server.URL, Language: "de"})
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Name != "Axiom Recoverer" {
		t.Fatalf("expected English name fallback 'Axiom Recoverer', got %q", records[0].Name)
	}
	if records[0].Language != "de" {
		t.Fatalf("expected language de, got %q", records[0].Language)
	}
	if result.Count != 1 {
		t.Fatalf("unexpected count: %d", result.Count)
	}
}
