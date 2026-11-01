package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"pokget/internal/models"
)

// DefaultPriceResolveTimeout bounds an on-demand lookup (for example right
// after a scan) so a slow market source cannot block the UI.
const DefaultPriceResolveTimeout = 8 * time.Second

// DefaultPriceCacheTTL keeps repeated lookups (multiple scans of the same card,
// candidate printing previews) off the market sources.
const DefaultPriceCacheTTL = 15 * time.Minute

// PriceQuote is the result of an on-demand price lookup.
type PriceQuote struct {
	USD       float64
	EUR       float64
	Source    string
	Cached    bool
	FetchedAt time.Time
}

// PriceService resolves market prices outside the sync worker, for example
// directly after a scan. It shares the per-game client chains with the worker,
// caches results briefly and collapses concurrent lookups for the same card.
type PriceService struct {
	clients map[string][]PriceClient
	timeout time.Duration
	ttl     time.Duration

	mu    sync.Mutex
	cache map[string]priceCacheEntry
	calls map[string]*priceCall
}

type priceCacheEntry struct {
	quote     PriceQuote
	expiresAt time.Time
}

type priceCall struct {
	done  chan struct{}
	quote PriceQuote
	err   error
}

// NewPriceService builds a resolver over the given per-game client chains. A
// "*" chain is used for games without their own entry.
func NewPriceService(clients map[string][]PriceClient) *PriceService {
	normalized := make(map[string][]PriceClient, len(clients))
	for game, chain := range clients {
		if len(chain) == 0 {
			continue
		}
		normalized[models.NormalizeGame(game)] = append([]PriceClient(nil), chain...)
	}
	return &PriceService{
		clients: normalized,
		timeout: DefaultPriceResolveTimeout,
		ttl:     DefaultPriceCacheTTL,
		cache:   make(map[string]priceCacheEntry),
		calls:   make(map[string]*priceCall),
	}
}

// WithTimeout overrides the lookup budget (tests use a short one).
func (s *PriceService) WithTimeout(timeout time.Duration) *PriceService {
	if s != nil && timeout > 0 {
		s.timeout = timeout
	}
	return s
}

// WithCacheTTL overrides the cache lifetime (tests use a short one).
func (s *PriceService) WithCacheTTL(ttl time.Duration) *PriceService {
	if s != nil && ttl >= 0 {
		s.ttl = ttl
	}
	return s
}

// Resolve returns the current market price for a card. A cached quote is
// returned without touching any market source.
func (s *PriceService) Resolve(ctx context.Context, card models.Card) (PriceQuote, error) {
	if s == nil {
		return PriceQuote{}, errors.New("price service is not configured")
	}
	key := priceCacheKey(card)
	if quote, ok := s.cachedQuote(key); ok {
		return quote, nil
	}

	call, owner := s.beginCall(key)
	if !owner {
		select {
		case <-call.done:
			return call.quote, call.err
		case <-ctx.Done():
			return PriceQuote{}, ctx.Err()
		}
	}

	quote, err := s.fetch(ctx, card)
	s.finishCall(key, call, quote, err)
	return quote, err
}

func (s *PriceService) fetch(ctx context.Context, card models.Card) (PriceQuote, error) {
	clients := s.clients[models.NormalizeGame(card.Game)]
	if len(clients) == 0 {
		clients = s.clients["*"]
	}
	if len(clients) == 0 {
		return PriceQuote{}, fmt.Errorf("no price source configured for game %q", card.Game)
	}

	timeout := s.timeout
	if timeout <= 0 {
		timeout = DefaultPriceResolveTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	quote := PriceQuote{FetchedAt: time.Now().UTC()}
	var errs []error
	for _, client := range clients {
		if client == nil {
			continue
		}
		usd, eur, err := fetchClientPrice(ctx, client, card)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if quote.USD <= 0 && validPrice(usd) {
			quote.USD = usd
			quote.Source = priceClientName(client)
		}
		if quote.EUR <= 0 && validPrice(eur) {
			quote.EUR = eur
			quote.Source = priceClientName(client)
		}
		if quote.USD > 0 && quote.EUR > 0 {
			return quote, nil
		}
	}

	if quote.USD <= 0 && quote.EUR <= 0 {
		if len(errs) > 0 {
			return PriceQuote{}, errors.Join(errs...)
		}
		return PriceQuote{}, fmt.Errorf("no price found for %q", card.Name)
	}
	return quote, nil
}

func (s *PriceService) cachedQuote(key string) (PriceQuote, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			delete(s.cache, key)
		}
		return PriceQuote{}, false
	}
	quote := entry.quote
	quote.Cached = true
	return quote, true
}

func (s *PriceService) beginCall(key string) (*priceCall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if call, ok := s.calls[key]; ok {
		return call, false
	}
	call := &priceCall{done: make(chan struct{})}
	s.calls[key] = call
	return call, true
}

func (s *PriceService) finishCall(key string, call *priceCall, quote PriceQuote, err error) {
	s.mu.Lock()
	if err == nil && (quote.USD > 0 || quote.EUR > 0) && s.ttl > 0 {
		s.cache[key] = priceCacheEntry{quote: quote, expiresAt: time.Now().Add(s.ttl)}
	}
	delete(s.calls, key)
	s.mu.Unlock()

	call.quote = quote
	call.err = err
	close(call.done)
}

func priceCacheKey(card models.Card) string {
	if id := strings.TrimSpace(card.ID); id != "" {
		return id
	}
	return strings.ToLower(strings.TrimSpace(card.Name)) + "|" + strings.ToLower(strings.TrimSpace(card.Set))
}

func fetchClientPrice(ctx context.Context, client PriceClient, card models.Card) (float64, float64, error) {
	if contextual, ok := client.(ContextPriceClient); ok {
		return contextual.FetchPriceContext(ctx, card)
	}
	return client.FetchPrice(card)
}

func validPrice(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func priceClientName(client PriceClient) string {
	switch client.(type) {
	case *TCGdexPriceClient:
		return "tcgdex"
	case *ScryfallPriceClient:
		return "scryfall"
	case *YGOPRODeckPriceClient:
		return "ygoprodeck"
	case *ScraperPriceClient:
		return "scraper"
	case *DefaultPriceClient:
		return "scraper"
	default:
		return "market"
	}
}
