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
	// NegativeTTL is how long a failed fetch is remembered before another call
	// is allowed to try the upstream again. It bounds how often an outage is
	// re-probed so every caller does not pay the HTTP timeout. Defaults to one
	// minute.
	NegativeTTL time.Duration

	mu       sync.Mutex
	rate     float64
	fetched  time.Time
	failedAt time.Time // separate from `fetched` so a failure never looks fresh
	lastErr  error
}

// NewFrankfurterEURRate returns a USD -> EUR provider backed by Frankfurter.
func NewFrankfurterEURRate() EURRateProvider {
	return &frankfurterEURRate{
		BaseURL:     "https://api.frankfurter.app",
		HTTP:        &http.Client{Timeout: 20 * time.Second},
		TTL:         6 * time.Hour,
		NegativeTTL: time.Minute,
	}
}

func (f *frankfurterEURRate) USDEUR(ctx context.Context) (float64, error) {
	ttl := f.TTL
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	negTTL := f.NegativeTTL
	if negTTL <= 0 {
		negTTL = time.Minute
	}

	f.mu.Lock()
	// A fresh success is served straight from the cache (6h TTL by default).
	if f.rate > 0 && time.Since(f.fetched) < ttl {
		rate := f.rate
		f.mu.Unlock()
		return rate, nil
	}
	// Recent failure: back off instead of re-probing on every call. Serve the
	// last good rate when we have one, otherwise the cached error — never a
	// silent (0, nil) that would wipe the EUR column.
	if !f.failedAt.IsZero() && time.Since(f.failedAt) < negTTL {
		rate, err := f.rate, f.lastErr
		f.mu.Unlock()
		if rate > 0 {
			return rate, nil
		}
		if err == nil {
			err = fmt.Errorf("fx usd->eur: temporarily unavailable")
		}
		return 0, err
	}
	base := strings.TrimRight(f.BaseURL, "/")
	if base == "" {
		base = "https://api.frankfurter.app"
	}
	f.mu.Unlock()

	// The HTTP call runs WITHOUT the lock held so a slow or hung upstream cannot
	// serialize every caller behind f.mu for the full request timeout.
	var payload struct {
		Rates map[string]float64 `json:"rates"`
		Date  string             `json:"date"`
	}
	fetchErr := fetchPriceJSON(ctx, f.HTTP, base+"/latest?from=USD&to=EUR", &payload)

	f.mu.Lock()
	defer f.mu.Unlock()
	if fetchErr != nil {
		f.failedAt = time.Now()
		f.lastErr = fmt.Errorf("fx usd->eur: %w", fetchErr)
		if f.rate > 0 {
			return f.rate, nil // keep the last known rate across an outage
		}
		return 0, f.lastErr
	}
	rate := payload.Rates["EUR"]
	if rate <= 0 {
		f.failedAt = time.Now()
		f.lastErr = fmt.Errorf("fx usd->eur: no EUR rate in response")
		if f.rate > 0 {
			return f.rate, nil
		}
		return 0, f.lastErr
	}
	f.rate = rate
	f.fetched = time.Now()
	f.failedAt = time.Time{}
	f.lastErr = nil
	return rate, nil
}
