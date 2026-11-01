package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"pokget/internal/models"
)

type countingPriceClient struct {
	calls int32
	usd   float64
	eur   float64
	err   error
}

func (c *countingPriceClient) FetchPrice(models.Card) (float64, float64, error) {
	atomic.AddInt32(&c.calls, 1)
	return c.usd, c.eur, c.err
}

func (c *countingPriceClient) ApplyMultiplier(price float64, _ string, _ map[string]float64) float64 {
	return price
}

func TestPriceServiceFallsBackToLaterClients(t *testing.T) {
	failing := &countingPriceClient{err: errors.New("source down")}
	working := &countingPriceClient{usd: 4.5, eur: 3.75}
	service := NewPriceService(map[string][]PriceClient{"*": {failing, working}})

	quote, err := service.Resolve(context.Background(), models.Card{ID: "card-1", Name: "Card", Game: "pokemon"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if quote.USD != 4.5 || quote.EUR != 3.75 {
		t.Fatalf("quote = %+v, want usd 4.5 and eur 3.75", quote)
	}
	if atomic.LoadInt32(&failing.calls) != 1 || atomic.LoadInt32(&working.calls) != 1 {
		t.Fatalf("calls = %d/%d, want 1/1", failing.calls, working.calls)
	}
}

func TestPriceServiceCachesQuotes(t *testing.T) {
	client := &countingPriceClient{usd: 2, eur: 1.5}
	service := NewPriceService(map[string][]PriceClient{"*": {client}}).WithCacheTTL(time.Minute)
	card := models.Card{ID: "card-1", Name: "Card"}

	first, err := service.Resolve(context.Background(), card)
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if first.Cached {
		t.Fatal("first quote must not be cached")
	}
	second, err := service.Resolve(context.Background(), card)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if !second.Cached {
		t.Fatal("second quote must come from the cache")
	}
	if calls := atomic.LoadInt32(&client.calls); calls != 1 {
		t.Fatalf("source calls = %d, want 1", calls)
	}
}

func TestPriceServiceReportsMissingPrices(t *testing.T) {
	service := NewPriceService(map[string][]PriceClient{"*": {&countingPriceClient{}}})
	if _, err := service.Resolve(context.Background(), models.Card{ID: "card-1", Name: "Card"}); err == nil {
		t.Fatal("expected an error when no source returns a price")
	}
}

func TestPriceServiceHonorsContextCancellation(t *testing.T) {
	service := NewPriceService(map[string][]PriceClient{"*": {&countingPriceClient{err: context.Canceled}}}).WithTimeout(50 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.Resolve(ctx, models.Card{ID: "card-1", Name: "Card"}); err == nil {
		t.Fatal("expected an error for a cancelled context")
	}
}

func TestPriceServiceNormalizesGameChains(t *testing.T) {
	pokemon := &countingPriceClient{eur: 9}
	other := &countingPriceClient{usd: 1}
	service := NewPriceService(map[string][]PriceClient{"Pokemon": {pokemon}, "*": {other}})

	quote, err := service.Resolve(context.Background(), models.Card{ID: "card-1", Game: "pokemon"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if quote.EUR != 9 {
		t.Fatalf("quote = %+v, want the pokemon chain", quote)
	}
	if calls := atomic.LoadInt32(&other.calls); calls != 0 {
		t.Fatalf("wildcard chain was used %d times, want 0", calls)
	}
}
