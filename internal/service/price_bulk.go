package service

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// BulkPriceUpdate is one card price row delivered by a bulk source. SourceID
// and SourceCardID match the catalog columns of the same name, so the worker can
// join without extra lookups. Sources that cannot address our card ids directly
// also fill Game/SetCode/Number for the fallback match.
type BulkPriceUpdate struct {
	SourceID     string
	SourceCardID string
	Game         string
	SetCode      string
	Number       string
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

// tcgPlayerPriceCategories maps our game ids to TCGplayer categories that
// TCGCSV mirrors with daily market prices.
var tcgPlayerPriceCategories = map[string]int{
	"magic":               1,
	"yugioh":              2,
	"pokemon":             3,
	"vanguard":            16,
	"force_of_will":       17,
	"weiss_schwarz":       20,
	"fftcg":               24,
	"universus":           25,
	"dragon_ball_super":   27,
	"flesh_and_blood":     62,
	"digimon":             63,
	"wixoss":              67,
	"one_piece":           68,
	"lorcana":             71,
	"battlespirits_saga":  72,
	"shadowverse_evolve":  73,
	"sorcery":             77,
	"alpha_clash":         78,
	"star_wars_unlimited": 79,
	"dbs_fusion_world":    80,
	"union_arena":         81,
	"gundam":              86,
	"hololive":            87,
	"godzilla":            88,
	"riftbound":           89,
	"cookie_run":          90,
}

// tcgcsvBulkSource downloads the daily TCGplayer price dump from TCGCSV. It
// covers every TCGplayer category, so games without their own price API still
// get a USD market price.
type tcgcsvBulkSource struct {
	BaseURL     string
	HTTP        *http.Client
	Categories  map[string]int
	Concurrency int
}

// NewTCGCSVBulkSource returns the daily TCGCSV price source.
func NewTCGCSVBulkSource() BulkPriceSource {
	return &tcgcsvBulkSource{
		BaseURL:     "https://tcgcsv.com/tcgplayer",
		HTTP:        &http.Client{Timeout: 60 * time.Second},
		Categories:  tcgPlayerPriceCategories,
		Concurrency: 4,
	}
}

func (s *tcgcsvBulkSource) Name() string { return "tcgcsv-bulk" }

func (s *tcgcsvBulkSource) FetchPrices(ctx context.Context, emit func(BulkPriceUpdate) error) error {
	base := strings.TrimRight(s.BaseURL, "/")
	if base == "" {
		base = "https://tcgcsv.com/tcgplayer"
	}
	categories := s.Categories
	if len(categories) == 0 {
		categories = tcgPlayerPriceCategories
	}
	concurrency := s.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	var emitMu sync.Mutex
	emitSafe := func(update BulkPriceUpdate) error {
		emitMu.Lock()
		defer emitMu.Unlock()
		return emit(update)
	}

	for game, category := range categories {
		if err := ctx.Err(); err != nil {
			return err
		}
		endpoint := fmt.Sprintf("%s/%d", base, category)
		var groups struct {
			Results []struct {
				GroupID      int    `json:"groupId"`
				Abbreviation string `json:"abbreviation"`
			} `json:"results"`
		}
		if err := fetchPriceJSON(ctx, s.HTTP, endpoint+"/groups", &groups); err != nil {
			return fmt.Errorf("tcgcsv groups %s: %w", game, err)
		}

		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		var failed int64
		var errMu sync.Mutex
		var lastErr error
		for _, group := range groups.Results {
			wg.Add(1)
			sem <- struct{}{}
			go func(groupID int, abbreviation string) {
				defer wg.Done()
				defer func() { <-sem }()
				if err := s.emitGroup(ctx, base, category, game, groupID, abbreviation, emitSafe); err != nil {
					errMu.Lock()
					if lastErr == nil {
						lastErr = err
					}
					errMu.Unlock()
					if n := atomic.AddInt64(&failed, 1); n <= 3 {
						slog.Warn("tcgcsv: skipping group", "game", game, "group", groupID, "error", err)
					}
				}
			}(group.GroupID, group.Abbreviation)
		}
		wg.Wait()
		if err := ctx.Err(); err != nil {
			return err
		}
		if failed == int64(len(groups.Results)) && lastErr != nil {
			// Every group of this category failed: surface the reason so the
			// operator sees it, but keep the other categories running.
			return fmt.Errorf("tcgcsv category %s: %w", game, lastErr)
		}
	}
	return nil
}

func (s *tcgcsvBulkSource) emitGroup(
	ctx context.Context,
	base string,
	category int,
	game string,
	groupID int,
	abbreviation string,
	emit func(BulkPriceUpdate) error,
) error {
	var products struct {
		Results []struct {
			ProductID    int `json:"productId"`
			ExtendedData []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"extendedData"`
		} `json:"results"`
	}
	var prices struct {
		Results []struct {
			ProductID   int     `json:"productId"`
			MarketPrice float64 `json:"marketPrice"`
			SubTypeName string  `json:"subTypeName"`
		} `json:"results"`
	}
	groupURL := fmt.Sprintf("%s/%d/%d", base, category, groupID)
	if err := s.getGroupJSON(ctx, groupURL+"/products", &products); err != nil {
		return fmt.Errorf("tcgcsv products %d: %w", groupID, err)
	}
	if err := s.getGroupJSON(ctx, groupURL+"/prices", &prices); err != nil {
		return fmt.Errorf("tcgcsv prices %d: %w", groupID, err)
	}

	priceByID := make(map[int]float64, len(prices.Results))
	subTypeByID := make(map[int]string, len(prices.Results))
	for _, price := range prices.Results {
		if price.MarketPrice <= 0 {
			continue
		}
		// Prefer the non-foil market price when a product has several printings.
		if _, ok := priceByID[price.ProductID]; ok {
			if strings.EqualFold(subTypeByID[price.ProductID], "normal") || !strings.EqualFold(price.SubTypeName, "normal") {
				continue
			}
		}
		priceByID[price.ProductID] = price.MarketPrice
		subTypeByID[price.ProductID] = price.SubTypeName
	}
	for _, product := range products.Results {
		market, ok := priceByID[product.ProductID]
		if !ok {
			continue
		}
		number := ""
		for _, ext := range product.ExtendedData {
			if ext.Name == "Number" {
				number = strings.TrimSpace(ext.Value)
				break
			}
		}
		update := BulkPriceUpdate{
			SourceID:     "tcgcsv",
			SourceCardID: strconv.Itoa(product.ProductID),
			Game:         game,
			SetCode:      abbreviation,
			Number:       number,
			USD:          market,
		}
		if err := emit(update); err != nil {
			return err
		}
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

// getGroupJSON retries a truncated or failed group download. TCGCSV sometimes
// closes a response early when several groups are fetched at once.
func (s *tcgcsvBulkSource) getGroupJSON(ctx context.Context, url string, target any) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := fetchPriceJSON(ctx, s.HTTP, url, target); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-time.After(time.Duration(attempt+1) * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return lastErr
}
