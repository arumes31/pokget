package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pokget/internal/models"
)

// priceAPIUserAgent identifies the application to the public price APIs that
// require a descriptive agent (Scryfall, TCGdex, YGOPRODeck).
const priceAPIUserAgent = "pokget-price-sync/1.0 (+https://github.com/arumes31/pokget)"

var priceAPIHTTPClient = &http.Client{Timeout: 20 * time.Second}

func priceClientOrDefault(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return priceAPIHTTPClient
}

// ParsePrice converts a market-place price string into a float64. It accepts
// both US ("$1,299.99") and German ("1.299,99 €") notation, because the public
// APIs return raw display strings.
func ParsePrice(text string) (float64, error) {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9', r == '.', r == ',', r == '-':
			return r
		default:
			return -1
		}
	}, text)
	if cleaned == "" || cleaned == "-" {
		return 0, fmt.Errorf("no numeric price found in %q", text)
	}

	lastDot := strings.LastIndex(cleaned, ".")
	lastComma := strings.LastIndex(cleaned, ",")
	switch {
	case lastDot >= 0 && lastComma >= 0:
		if lastComma > lastDot {
			// German notation: dots group thousands, the comma is decimal.
			cleaned = strings.ReplaceAll(cleaned, ".", "")
			cleaned = strings.ReplaceAll(cleaned, ",", ".")
		} else {
			// US notation: commas group thousands, the dot is decimal.
			cleaned = strings.ReplaceAll(cleaned, ",", "")
		}
	case lastComma >= 0:
		if decimalsAfter(cleaned, lastComma) == 2 {
			cleaned = strings.ReplaceAll(cleaned, ",", ".")
		} else {
			cleaned = strings.ReplaceAll(cleaned, ",", "")
		}
	case lastDot >= 0:
		if decimalsAfter(cleaned, lastDot) != 2 {
			cleaned = strings.ReplaceAll(cleaned, ".", "")
		}
	}

	return strconv.ParseFloat(cleaned, 64)
}

func decimalsAfter(value string, separator int) int {
	return len(value) - separator - 1
}

func fetchPriceJSON(ctx context.Context, client *http.Client, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", priceAPIUserAgent)

	response, err := priceClientOrDefault(client).Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("price api %s: HTTP %d", endpoint, response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(target); err != nil {
		return fmt.Errorf("price api %s: %w", endpoint, err)
	}
	return nil
}

// TCGdexPriceClient reads Cardmarket (EUR) and TCGplayer (USD) prices from the
// TCGdex API. Cards catalogued by the TCGdex provider carry the upstream card id
// (for example "swsh1-1") in cards.source_card_id.
type TCGdexPriceClient struct {
	BaseURL  string
	Language string
	HTTP     *http.Client
}

func (c *TCGdexPriceClient) FetchPrice(card models.Card) (float64, float64, error) {
	return c.FetchPriceContext(context.Background(), card)
}

func (c *TCGdexPriceClient) ApplyMultiplier(price float64, condition string, multipliers map[string]float64) float64 {
	return applyConditionMultiplier(price, condition, multipliers)
}

func (c *TCGdexPriceClient) FetchPriceContext(ctx context.Context, card models.Card) (float64, float64, error) {
	if c == nil {
		return 0, 0, fmt.Errorf("nil TCGdex price client")
	}
	base, fallbackLanguage := c.endpoint()
	// Japanese (and other non-English) printings only exist in their own TCGdex
	// catalog. Querying the English catalog returned "no card id" and the UI
	// showed "Price unavailable" for a card that Cardmarket prices just fine.
	language := tcgdexLanguageFor(card.Language, fallbackLanguage)
	id, err := c.resolveCardID(ctx, base, language, card)
	if err != nil && language != fallbackLanguage {
		// A card catalogued in another language may still be listed in the
		// fallback catalog; try it before reporting a missing price.
		if fallbackID, fallbackErr := c.resolveCardID(ctx, base, fallbackLanguage, card); fallbackErr == nil {
			id, err = fallbackID, nil
			language = fallbackLanguage
		}
	}
	if err != nil {
		return 0, 0, err
	}

	var payload tcgdexPricePayload
	endpoint := fmt.Sprintf("%s/%s/cards/%s", base, url.PathEscape(language), url.PathEscape(id))
	if err := fetchPriceJSON(ctx, c.HTTP, endpoint, &payload); err != nil {
		return 0, 0, err
	}

	eur := payload.Pricing.Cardmarket.EUR()
	usd := payload.Pricing.TCGplayer.USD()
	if eur <= 0 && usd <= 0 {
		return 0, 0, fmt.Errorf("tcgdex: no price for %s", id)
	}
	return usd, eur, nil
}

// tcgdexLanguages maps the catalog language codes onto the TCGdex language
// path segments. Unknown codes fall back to the client language.
var tcgdexLanguages = map[string]string{
	"en": "en", "eng": "en", "english": "en",
	"de": "de", "deu": "de", "ger": "de", "german": "de",
	"fr": "fr", "fra": "fr", "fre": "fr", "french": "fr",
	"es": "es", "spa": "es", "spanish": "es",
	"it": "it", "ita": "it", "italian": "it",
	"pt": "pt", "por": "pt", "portuguese": "pt",
	"nl": "nl", "pl": "pl", "ru": "ru",
	"ja": "ja", "jp": "ja", "jpn": "ja", "japanese": "ja",
	"ko": "ko", "kor": "ko", "korean": "ko",
	"zh": "zh-cn", "cn": "zh-cn", "zh-cn": "zh-cn", "chi_sim": "zh-cn",
	"tw": "zh-tw", "zh-tw": "zh-tw", "chi_tra": "zh-tw",
	"id": "id", "th": "th",
}

// tcgdexLanguageFor returns the TCGdex language segment for a card, falling back
// to the client default when the card language is unknown.
func tcgdexLanguageFor(cardLanguage, fallback string) string {
	code := strings.ToLower(strings.TrimSpace(cardLanguage))
	if mapped, ok := tcgdexLanguages[code]; ok {
		return mapped
	}
	// Accept region-tagged codes such as "ja-JP" or "en-US".
	if base, _, found := strings.Cut(code, "-"); found {
		if mapped, ok := tcgdexLanguages[base]; ok {
			return mapped
		}
	}
	if strings.TrimSpace(fallback) == "" {
		return "en"
	}
	return fallback
}

func (c *TCGdexPriceClient) endpoint() (string, string) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.tcgdex.net/v2"
	}
	language := strings.TrimSpace(c.Language)
	if language == "" {
		language = "en"
	}
	return base, language
}

// resolveCardID returns the TCGdex card id. Rows written by other catalog
// sources (or legacy rows) have no usable source_card_id, so the id is resolved
// through TCGdex's public name search, which keeps a scraper fallback
// unnecessary.
func (c *TCGdexPriceClient) resolveCardID(ctx context.Context, base, language string, card models.Card) (string, error) {
	if id := strings.TrimSpace(card.SourceCardID); id != "" {
		if source := strings.TrimSpace(card.SourceID); source == "" || strings.EqualFold(source, "tcgdex") {
			return id, nil
		}
	}

	name := strings.TrimSpace(card.Name)
	if name == "" {
		return "", fmt.Errorf("tcgdex: card %q has no name", card.Name)
	}
	var results []struct {
		ID      string `json:"id"`
		LocalID string `json:"localId"`
		Name    string `json:"name"`
	}
	endpoint := fmt.Sprintf("%s/%s/cards?name=%s", base, url.PathEscape(language), url.QueryEscape(name))
	if err := fetchPriceJSON(ctx, c.HTTP, endpoint, &results); err != nil {
		return "", err
	}

	number := strings.TrimSpace(card.CollectorNumber)
	// Prefer an exact name match (with the collector number when present); the
	// number alone is ambiguous because many sets reuse it.
	for _, result := range results {
		if !strings.EqualFold(strings.TrimSpace(result.Name), name) {
			continue
		}
		if number == "" || result.LocalID == number {
			return result.ID, nil
		}
	}
	for _, result := range results {
		if strings.EqualFold(strings.TrimSpace(result.Name), name) {
			return result.ID, nil
		}
	}
	if number != "" {
		for _, result := range results {
			if result.LocalID == number {
				return result.ID, nil
			}
		}
	}
	return "", fmt.Errorf("tcgdex: no card id for %q", name)
}

type tcgdexPricePayload struct {
	Pricing struct {
		Cardmarket tcgdexCardmarketPricing `json:"cardmarket"`
		TCGplayer  *tcgdexTCGplayerPricing `json:"tcgplayer"`
	} `json:"pricing"`
}

// tcgdexCardmarketPricing carries Cardmarket's average, trend and holo prices.
// Japanese sets frequently publish only the holo columns, so a lookup that
// reads just "avg" reported the card as unpriced.
type tcgdexCardmarketPricing struct {
	Unit      string   `json:"unit"`
	Avg       float64  `json:"avg"`
	Low       float64  `json:"low"`
	Trend     float64  `json:"trend"`
	AvgHolo   *float64 `json:"avg-holo"`
	LowHolo   *float64 `json:"low-holo"`
	TrendHolo *float64 `json:"trend-holo"`
}

// EUR returns the best available Cardmarket price in euro, or zero.
func (c tcgdexCardmarketPricing) EUR() float64 {
	candidates := []float64{c.Avg, c.Low, c.Trend}
	for _, holo := range []*float64{c.AvgHolo, c.TrendHolo, c.LowHolo} {
		if holo != nil {
			candidates = append(candidates, *holo)
		}
	}
	for _, candidate := range candidates {
		if candidate > 0 {
			return candidate
		}
	}
	return 0
}

// tcgdexTCGplayerPricing accepts both payload shapes TCGdex has used: a flat
// object with avg/market fields and an object with one price per variant
// (normal, holofoil, ...). Only one of them is populated at a time.
type tcgdexTCGplayerPricing struct {
	Unit            string                  `json:"unit"`
	Market          float64                 `json:"market"`
	MarketPrice     float64                 `json:"marketPrice"`
	Avg             float64                 `json:"avg"`
	Normal          *tcgdexTCGplayerVariant `json:"normal"`
	Holofoil        *tcgdexTCGplayerVariant `json:"holofoil"`
	ReverseHolofoil *tcgdexTCGplayerVariant `json:"reverseHolofoil"`
	FirstEdition    *tcgdexTCGplayerVariant `json:"firstEdition"`
}

type tcgdexTCGplayerVariant struct {
	MarketPrice float64 `json:"marketPrice"`
	LowPrice    float64 `json:"lowPrice"`
}

// USD returns the TCGplayer price in US dollars, or zero when TCGdex has none.
func (t *tcgdexTCGplayerPricing) USD() float64 {
	if t == nil {
		return 0
	}
	for _, candidate := range []float64{t.Market, t.MarketPrice, t.Avg} {
		if candidate > 0 {
			return candidate
		}
	}
	for _, variant := range []*tcgdexTCGplayerVariant{t.Normal, t.Holofoil, t.ReverseHolofoil, t.FirstEdition} {
		if variant != nil && variant.MarketPrice > 0 {
			return variant.MarketPrice
		}
	}
	return 0
}

// ScryfallPriceClient reads Scryfall's Cardmarket (EUR) and TCGplayer (USD)
// price fields for Magic cards.
type ScryfallPriceClient struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *ScryfallPriceClient) FetchPrice(card models.Card) (float64, float64, error) {
	return c.FetchPriceContext(context.Background(), card)
}

func (c *ScryfallPriceClient) ApplyMultiplier(price float64, condition string, multipliers map[string]float64) float64 {
	return applyConditionMultiplier(price, condition, multipliers)
}

func (c *ScryfallPriceClient) FetchPriceContext(ctx context.Context, card models.Card) (float64, float64, error) {
	if c == nil {
		return 0, 0, fmt.Errorf("nil Scryfall price client")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.scryfall.com"
	}
	endpoint := fmt.Sprintf("%s/cards/named?fuzzy=%s", base, url.QueryEscape(strings.TrimSpace(card.Name)))

	var payload struct {
		Prices struct {
			USD *string `json:"usd"`
			EUR *string `json:"eur"`
		} `json:"prices"`
	}
	if err := fetchPriceJSON(ctx, c.HTTP, endpoint, &payload); err != nil {
		return 0, 0, err
	}
	usd := parseOptionalPrice(payload.Prices.USD)
	eur := parseOptionalPrice(payload.Prices.EUR)
	if usd <= 0 && eur <= 0 {
		return 0, 0, fmt.Errorf("scryfall: no price for %q", card.Name)
	}
	return usd, eur, nil
}

func parseOptionalPrice(raw *string) float64 {
	if raw == nil {
		return 0
	}
	value, err := ParsePrice(*raw)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

// firstPositivePrice returns the first usable price of a series. Bulk sources list
// the plain, foil and etched price of a printing side by side; a printing that only
// exists as foil has no plain value.
func firstPositivePrice(candidates ...*string) float64 {
	price, _ := firstVariantPrice(candidates, nil)
	return price
}

// pricedField pairs a raw marketplace price with the printing it describes.
type pricedField struct {
	raw     *string
	variant string
}

// firstVariantPrice returns the first usable price and the printing it belongs to:
// "normal" for the plain fields, otherwise the variant of the fallback field
// ("foil", "etched"). A printing that only exists as foil has no plain value.
func firstVariantPrice(plain []*string, variants []pricedField) (float64, string) {
	for _, raw := range plain {
		if value := parseOptionalPrice(raw); value > 0 {
			return value, "normal"
		}
	}
	for _, field := range variants {
		if value := parseOptionalPrice(field.raw); value > 0 {
			return value, field.variant
		}
	}
	return 0, ""
}

// YGOPRODeckPriceClient reads the Cardmarket (EUR) and TCGplayer (USD) price
// fields bundled in the YGOPRODeck card payload.
type YGOPRODeckPriceClient struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *YGOPRODeckPriceClient) FetchPrice(card models.Card) (float64, float64, error) {
	return c.FetchPriceContext(context.Background(), card)
}

func (c *YGOPRODeckPriceClient) ApplyMultiplier(price float64, condition string, multipliers map[string]float64) float64 {
	return applyConditionMultiplier(price, condition, multipliers)
}

func (c *YGOPRODeckPriceClient) FetchPriceContext(ctx context.Context, card models.Card) (float64, float64, error) {
	if c == nil {
		return 0, 0, fmt.Errorf("nil YGOPRODeck price client")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://db.ygoprodeck.com/api/v7"
	}
	endpoint := fmt.Sprintf("%s/cardinfo.php?name=%s", base, url.QueryEscape(strings.TrimSpace(card.Name)))

	var payload struct {
		Data []struct {
			CardPrices []struct {
				CardmarketPrice string `json:"cardmarket_price"`
				TcgplayerPrice  string `json:"tcgplayer_price"`
			} `json:"card_prices"`
		} `json:"data"`
	}
	if err := fetchPriceJSON(ctx, c.HTTP, endpoint, &payload); err != nil {
		return 0, 0, err
	}
	if len(payload.Data) == 0 || len(payload.Data[0].CardPrices) == 0 {
		return 0, 0, fmt.Errorf("ygoprodeck: no price for %q", card.Name)
	}
	first := payload.Data[0].CardPrices[0]
	usd := parseOptionalPrice(&first.TcgplayerPrice)
	eur := parseOptionalPrice(&first.CardmarketPrice)
	if usd <= 0 && eur <= 0 {
		return 0, 0, fmt.Errorf("ygoprodeck: no price for %q", card.Name)
	}
	return usd, eur, nil
}
