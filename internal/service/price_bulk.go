package service

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// BulkPriceUpdate is one card price row delivered by a bulk source. SourceID
// and SourceCardID match the catalog columns of the same name, so the worker can
// join without extra lookups.
type BulkPriceUpdate struct {
	SourceID     string
	SourceCardID string
	USD          float64
	EUR          float64
}

// BulkPriceSource streams current market prices for its whole catalog. An
// implementation must call emit for every card with at least one price and must
// return early when emit fails or the context ends.
type BulkPriceSource interface {
	Name() string
	FetchPrices(ctx context.Context, emit func(BulkPriceUpdate) error) error
}

// scryfallBulkSource downloads Scryfall's daily default-cards export (a gzipped
// JSONL file) and reads the Cardmarket (EUR) and TCGplayer (USD) prices of every
// Magic card. Bulk data is free under Scryfall's fan-content terms and refreshes
// once per day.
type scryfallBulkSource struct {
	BaseURL string
	HTTP    *http.Client
}

// NewScryfallBulkSource returns the daily Scryfall bulk price source.
func NewScryfallBulkSource() BulkPriceSource {
	return &scryfallBulkSource{
		BaseURL: "https://api.scryfall.com",
		// The daily export is ~80 MB gzipped; the shared 20 s API timeout would
		// abort the download halfway through.
		HTTP: &http.Client{Timeout: 30 * time.Minute},
	}
}

func (s *scryfallBulkSource) Name() string { return "scryfall-bulk" }

func (s *scryfallBulkSource) FetchPrices(ctx context.Context, emit func(BulkPriceUpdate) error) error {
	base := strings.TrimRight(s.BaseURL, "/")
	if base == "" {
		base = "https://api.scryfall.com"
	}

	var manifest struct {
		Data []struct {
			Type             string `json:"type"`
			JSONLDownloadURI string `json:"jsonl_download_uri"`
		} `json:"data"`
	}
	if err := fetchPriceJSON(ctx, s.HTTP, base+"/bulk-data", &manifest); err != nil {
		return fmt.Errorf("scryfall bulk manifest: %w", err)
	}
	downloadURL := ""
	for _, entry := range manifest.Data {
		if entry.Type == "default_cards" && entry.JSONLDownloadURI != "" {
			downloadURL = entry.JSONLDownloadURI
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("scryfall bulk manifest: no default_cards download")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", priceAPIUserAgent)

	response, err := priceClientOrDefault(s.HTTP).Do(request)
	if err != nil {
		return fmt.Errorf("scryfall bulk download: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("scryfall bulk download: HTTP %d", response.StatusCode)
	}

	uncompressed, err := gzip.NewReader(response.Body)
	if err != nil {
		return fmt.Errorf("scryfall bulk gzip: %w", err)
	}
	defer func() { _ = uncompressed.Close() }()

	scanner := bufio.NewScanner(uncompressed)
	scanner.Buffer(make([]byte, 0, 256<<10), 4<<20)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var card struct {
			ID     string `json:"id"`
			Prices struct {
				USD *string `json:"usd"`
				EUR *string `json:"eur"`
			} `json:"prices"`
		}
		if err := json.Unmarshal([]byte(line), &card); err != nil {
			continue // tolerate a single malformed line
		}
		if card.ID == "" {
			continue
		}
		usd := parseOptionalPrice(card.Prices.USD)
		eur := parseOptionalPrice(card.Prices.EUR)
		if usd <= 0 && eur <= 0 {
			continue
		}
		if err := emit(BulkPriceUpdate{SourceID: "scryfall", SourceCardID: card.ID, USD: usd, EUR: eur}); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scryfall bulk read: %w", err)
	}
	return nil
}

// ygoprodeckBulkSource reads the whole Yu-Gi-Oh card database in one request.
// The API is free, allows one bulk request and returns Cardmarket/TCGplayer
// prices per card.
type ygoprodeckBulkSource struct {
	BaseURL string
	HTTP    *http.Client
}

// NewYGOPRODeckBulkSource returns the Yu-Gi-Oh bulk price source.
func NewYGOPRODeckBulkSource() BulkPriceSource {
	return &ygoprodeckBulkSource{
		BaseURL: "https://db.ygoprodeck.com/api/v7",
		// The full card dump is a few megabytes; keep a generous but finite budget.
		HTTP: &http.Client{Timeout: 10 * time.Minute},
	}
}

func (s *ygoprodeckBulkSource) Name() string { return "ygoprodeck-bulk" }

func (s *ygoprodeckBulkSource) FetchPrices(ctx context.Context, emit func(BulkPriceUpdate) error) error {
	base := strings.TrimRight(s.BaseURL, "/")
	if base == "" {
		base = "https://db.ygoprodeck.com/api/v7"
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/cardinfo.php", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", priceAPIUserAgent)

	response, err := priceClientOrDefault(s.HTTP).Do(request)
	if err != nil {
		return fmt.Errorf("ygoprodeck bulk: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("ygoprodeck bulk: HTTP %d", response.StatusCode)
	}

	var payload struct {
		Data []struct {
			ID         int64 `json:"id"`
			CardPrices []struct {
				CardmarketPrice string `json:"cardmarket_price"`
				TcgplayerPrice  string `json:"tcgplayer_price"`
			} `json:"card_prices"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 512<<20)).Decode(&payload); err != nil {
		return fmt.Errorf("ygoprodeck bulk decode: %w", err)
	}

	for _, card := range payload.Data {
		if card.ID <= 0 || len(card.CardPrices) == 0 {
			continue
		}
		first := card.CardPrices[0]
		usd := parseOptionalPrice(&first.TcgplayerPrice)
		eur := parseOptionalPrice(&first.CardmarketPrice)
		if usd <= 0 && eur <= 0 {
			continue
		}
		update := BulkPriceUpdate{
			SourceID:     "ygoprodeck",
			SourceCardID: strconv.FormatInt(card.ID, 10),
			USD:          usd,
			EUR:          eur,
		}
		if err := emit(update); err != nil {
			return err
		}
	}
	return nil
}
