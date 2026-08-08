//go:build integration

package source

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"pokget/internal/catalog"
)

const liveProbeRecordLimit = 5

var errLiveProbeComplete = errors.New("catalog source live probe complete")

func TestDefaultProvidersLive(t *testing.T) {
	providers := DefaultProviders(HTTPOptions{
		Client:       &http.Client{Timeout: 2 * time.Minute},
		UserAgent:    "pokget-catalog-live-test/1.0",
		MaxBodyBytes: 512 << 20,
		RequestDelay: 20 * time.Millisecond,
	}, "en", 1)
	requestedSource := strings.TrimSpace(os.Getenv("POKGET_CATALOG_SOURCE"))
	if requestedSource != "" {
		providers = selectLiveProviders(t, providers, requestedSource)
	}

	for _, provider := range providers {
		t.Run(provider.ID(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()

			records := make([]catalog.CardRecord, 0, liveProbeRecordLimit)
			_, err := provider.Fetch(ctx, catalog.FetchRequest{Mode: catalog.SyncModeFull}, func(record catalog.CardRecord) error {
				if err := record.Validate(); err != nil {
					return fmt.Errorf("validating live record: %w", err)
				}
				records = append(records, record)
				if len(records) == liveProbeRecordLimit {
					return errLiveProbeComplete
				}
				return nil
			})
			if err != nil && !errors.Is(err, errLiveProbeComplete) {
				t.Fatalf("Fetch() error = %v", err)
			}
			if len(records) != liveProbeRecordLimit {
				t.Fatalf("live records = %d, want %d", len(records), liveProbeRecordLimit)
			}
			t.Logf("validated %d live records; first=%q last=%q", len(records), records[0].SourceCardID, records[len(records)-1].SourceCardID)
		})
	}
}

func selectLiveProviders(t *testing.T, providers []catalog.Provider, requestedSource string) []catalog.Provider {
	t.Helper()
	for _, provider := range providers {
		if provider.ID() == requestedSource {
			return []catalog.Provider{provider}
		}
	}
	t.Fatalf("POKGET_CATALOG_SOURCE %q is not a registered provider", requestedSource)
	return nil
}
