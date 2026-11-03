package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFirstPositivePricePrefersThePlainPrice(t *testing.T) {
	plain, foil, etched := "1.50", "9.00", "4.00"
	if got := firstPositivePrice(&plain, &foil, &etched); got != 1.5 {
		t.Fatalf("got %v, want the plain price", got)
	}
	empty := ""
	if got := firstPositivePrice(&empty, nil, &etched); got != 4 {
		t.Fatalf("got %v, want the etched price", got)
	}
	if got := firstPositivePrice(&empty, nil); got != 0 {
		t.Fatalf("got %v, want 0 without usable prices", got)
	}
}

func TestFrankfurterEURRateReadsAndCachesTheRate(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.URL.Query().Get("from") != "USD" || request.URL.Query().Get("to") != "EUR" {
			t.Errorf("unexpected query %q", request.URL.RawQuery)
		}
		_, _ = writer.Write([]byte(`{"amount":1.0,"base":"USD","date":"2026-11-02","rates":{"EUR":0.89158}}`))
	}))
	defer server.Close()

	provider := &frankfurterEURRate{
		BaseURL: server.URL,
		HTTP:    server.Client(),
		TTL:     time.Hour,
	}
	rate, err := provider.USDEUR(context.Background())
	if err != nil {
		t.Fatalf("USDEUR: %v", err)
	}
	if rate != 0.89158 {
		t.Fatalf("rate = %v, want 0.89158", rate)
	}
	// The second call must come from the cache: the value only changes once per
	// working day and the API should not be hammered.
	if _, err := provider.USDEUR(context.Background()); err != nil {
		t.Fatalf("USDEUR (cached): %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1 (cached)", requests)
	}
}

func TestFrankfurterEURRateFallsBackToTheLastKnownRate(t *testing.T) {
	failing := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if failing {
			http.Error(writer, "down", http.StatusServiceUnavailable)
			return
		}
		_, _ = writer.Write([]byte(`{"rates":{"EUR":0.9}}`))
	}))
	defer server.Close()

	provider := &frankfurterEURRate{BaseURL: server.URL, HTTP: server.Client(), TTL: time.Nanosecond}
	if _, err := provider.USDEUR(context.Background()); err != nil {
		t.Fatalf("USDEUR: %v", err)
	}
	failing = true
	time.Sleep(2 * time.Millisecond)
	rate, err := provider.USDEUR(context.Background())
	if err != nil {
		t.Fatalf("USDEUR with a stale rate must not fail: %v", err)
	}
	if rate != 0.9 {
		t.Fatalf("rate = %v, want the last known 0.9", rate)
	}
}

func TestFrankfurterEURRateWithoutAnyRateFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"rates":{}}`))
	}))
	defer server.Close()

	provider := &frankfurterEURRate{BaseURL: server.URL, HTTP: server.Client(), TTL: time.Hour}
	if _, err := provider.USDEUR(context.Background()); err == nil {
		t.Fatal("expected an error when the response carries no EUR rate")
	}
}
