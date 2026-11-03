package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// EURRateProvider reports how many EUR one US dollar is worth. Most TCG
// marketplaces only publish USD prices and Cardmarket blocks automated access,
// so the EUR column is filled from the USD price with the daily ECB reference
// rate. Converted values are flagged in the database (cards.price_eur_estimated)
// so they are never presented as a Cardmarket quote.
type EURRateProvider interface {
	// USDEUR returns the current USD -> EUR rate.
	USDEUR(ctx context.Context) (float64, error)
}

// frankfurterEURRate reads the ECB reference rate from the free Frankfurter API
// (no key, one update per working day) and keeps the last good rate in memory so
// a temporary outage cannot zero out the EUR column.
type frankfurterEURRate struct {
	BaseURL string
	HTTP    *http.Client
	// TTL is how long a fetched rate is reused. ECB publishes once per working
	// day, so a few hours are safe and keep the provider quiet.
	TTL time.Duration

	mu      sync.Mutex
	rate    float64
	fetched time.Time
}

// NewFrankfurterEURRate returns a USD -> EUR provider backed by Frankfurter.
func NewFrankfurterEURRate() EURRateProvider {
	return &frankfurterEURRate{
		BaseURL: "https://api.frankfurter.app",
		HTTP:    &http.Client{Timeout: 20 * time.Second},
		TTL:     6 * time.Hour,
	}
}

func (f *frankfurterEURRate) USDEUR(ctx context.Context) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ttl := f.TTL
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	if f.rate > 0 && time.Since(f.fetched) < ttl {
		return f.rate, nil
	}

	base := strings.TrimRight(f.BaseURL, "/")
	if base == "" {
		base = "https://api.frankfurter.app"
	}
	var payload struct {
		Rates map[string]float64 `json:"rates"`
		Date  string             `json:"date"`
	}
	if err := fetchPriceJSON(ctx, f.HTTP, base+"/latest?from=USD&to=EUR", &payload); err != nil {
		if f.rate > 0 {
			return f.rate, nil
		}
		return 0, fmt.Errorf("fx usd->eur: %w", err)
	}
	rate := payload.Rates["EUR"]
	if rate <= 0 {
		if f.rate > 0 {
			return f.rate, nil
		}
		return 0, fmt.Errorf("fx usd->eur: no EUR rate in response")
	}
	f.rate = rate
	f.fetched = time.Now()
	return rate, nil
}
