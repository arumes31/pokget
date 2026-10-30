package source

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// Helper to LZW-compress string into comma-separated integers matching wow.js
func compressWoWLZW(input string) string {
	if input == "" {
		return ""
	}
	dict := make(map[string]int)
	for i := 0; i < 256; i++ {
		dict[string(rune(i))] = i
	}

	w := ""
	var result []string
	dictSize := 256

	for _, r := range input {
		c := string(r)
		wc := w + c
		if _, ok := dict[wc]; ok {
			w = wc
		} else {
			result = append(result, strconv.Itoa(dict[w]))
			dict[wc] = dictSize
			dictSize++
			w = c
		}
	}
	if w != "" {
		result = append(result, strconv.Itoa(dict[w]))
	}
	return strings.Join(result, ",")
}

func TestWoWTCGProviderLZW(t *testing.T) {
	jsonPayload := `{
		"2264": {
			"cardid": 2264,
			"name": "Spectral Tiger",
			"type": "Ally",
			"faction": "Neutral",
			"image": "spectral_tiger",
			"setname": "Fires of Outland",
			"setcode": "FoO",
			"rarity": "r",
			"no": 193,
			"artist": "Raven Mimura"
		},
		"6082": {
			"cardid": 6082,
			"name": "Spectral Tiger (Loot)",
			"type": "Ally",
			"faction": "Neutral",
			"image": "spectral_tiger_loot",
			"setname": "Fires of Outland (Loot)",
			"setcode": "FoO",
			"rarity": "l",
			"no": 193,
			"artist": "Raven Mimura"
		}
	}`

	lzwPayload := compressWoWLZW(jsonPayload)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dl/cards.dat":
			if r.Header.Get("If-None-Match") == `"test-etag"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"test-etag"`)
			_, _ = w.Write([]byte(lzwPayload))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := &WoWTCGProvider{
		HTTP:    testHTTP(server),
		BaseURL: server.URL,
	}

	records, result := fetchRecords(t, provider)
	if result.Count != 2 || len(records) != 2 {
		t.Fatalf("expected 2 records, got count=%d, len=%d", result.Count, len(records))
	}

	// First card: Spectral Tiger (normal)
	c1 := records[0]
	if c1.Name != "Spectral Tiger" || c1.SetCode != "FoO" || c1.CollectorNumber != "193" {
		t.Fatalf("unexpected card 1: %+v", c1)
	}
	if c1.Rarity != "Rare" {
		t.Fatalf("expected Rare, got %s", c1.Rarity)
	}
	if c1.Variant != "Normal" {
		t.Fatalf("expected Normal, got %s", c1.Variant)
	}
	if len(c1.Images) != 1 || c1.Images[0].URL != fmt.Sprintf("%s/images/cards/medium/spectral_tiger.jpg", server.URL) {
		t.Fatalf("unexpected image URL: %+v", c1.Images)
	}

	// Second card: Spectral Tiger (Loot)
	c2 := records[1]
	if c2.Name != "Spectral Tiger (Loot)" || c2.SetCode != "FoO" || c2.CollectorNumber != "193" {
		t.Fatalf("unexpected card 2: %+v", c2)
	}
	if c2.Rarity != "Legendary" {
		t.Fatalf("expected Legendary for loot, got %s", c2.Rarity)
	}
	if c2.Variant != "Loot" {
		t.Fatalf("expected Loot variant, got %s", c2.Variant)
	}
	if len(c2.Images) != 1 || c2.Images[0].URL != fmt.Sprintf("%s/images/cards/medium/spectral_tiger_loot.jpg", server.URL) {
		t.Fatalf("unexpected loot image URL: %+v", c2.Images)
	}
}

func TestWoWTCGProviderJSONDirect(t *testing.T) {
	jsonPayload := `[
		{
			"cardid": "1",
			"name": "Nature Resistance Totem",
			"type": "Ability Ally",
			"faction": "Neutral",
			"image": "nature_resistance_totem",
			"setname": "Worldbreaker",
			"setcode": "WBR",
			"rarity": "u",
			"no": "99"
		}
	]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jsonPayload))
	}))
	defer server.Close()

	provider := &WoWTCGProvider{
		HTTP:    testHTTP(server),
		BaseURL: server.URL + "/cards.json",
	}

	records, result := fetchRecords(t, provider)
	if result.Count != 1 || len(records) != 1 {
		t.Fatalf("expected 1 record, got count=%d, len=%d", result.Count, len(records))
	}
	if records[0].Name != "Nature Resistance Totem" || records[0].Rarity != "Uncommon" {
		t.Fatalf("unexpected record: %+v", records[0])
	}
}
