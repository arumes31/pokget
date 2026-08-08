package main

import (
	"reflect"
	"strings"
	"testing"

	"pokget/internal/config"
	"pokget/internal/worker"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestParseMetadataTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  []worker.MetadataTarget
	}{
		{
			name:  "single target",
			value: "pokemon:en",
			want:  []worker.MetadataTarget{{Game: "pokemon", Language: "en"}},
		},
		{
			name:  "multiple targets with whitespace and case normalization",
			value: " pokemon:en , One Piece:JP ",
			want: []worker.MetadataTarget{
				{Game: "pokemon", Language: "en"},
				{Game: "one_piece", Language: "jp"},
			},
		},
		{name: "missing separator skipped", value: "pokemon", want: []worker.MetadataTarget{}},
		{name: "empty game skipped", value: ":en", want: []worker.MetadataTarget{}},
		{name: "empty language skipped", value: "pokemon:", want: []worker.MetadataTarget{}},
		{name: "empty value", value: "", want: []worker.MetadataTarget{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := parseMetadataTargets(test.value)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("parseMetadataTargets(%q) = %#v, want %#v", test.value, got, test.want)
			}
		})
	}
}

func TestValidateRuntimeConfig(t *testing.T) {
	t.Parallel()

	validConfig := func() *config.Config {
		cfg := &config.Config{}
		cfg.Scan.TimeoutSeconds = 75
		cfg.Worker.PriceSyncMinutes = 60
		cfg.Worker.MetadataTargets = "pokemon:en"
		return cfg
	}

	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr string
	}{
		{name: "valid", mutate: func(*config.Config) {}},
		{
			name:    "non-positive scan timeout",
			mutate:  func(cfg *config.Config) { cfg.Scan.TimeoutSeconds = 0 },
			wantErr: "SCAN_TIMEOUT_SECONDS",
		},
		{
			name:    "non-positive price sync interval",
			mutate:  func(cfg *config.Config) { cfg.Worker.PriceSyncMinutes = 0 },
			wantErr: "PRICE_SYNC_INTERVAL_MINUTES",
		},
		{
			name: "legacy metadata sync without targets",
			mutate: func(cfg *config.Config) {
				cfg.Catalog.LegacyMetadataSync = true
				cfg.Worker.MetadataTargets = "invalid"
			},
			wantErr: "METADATA_TARGETS",
		},
		{
			name: "legacy metadata sync with targets",
			mutate: func(cfg *config.Config) {
				cfg.Catalog.LegacyMetadataSync = true
				cfg.Worker.MetadataTargets = "pokemon:en"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			test.mutate(cfg)
			err := validateRuntimeConfig(cfg)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("validateRuntimeConfig() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validateRuntimeConfig() = %v, want error mentioning %q", err, test.wantErr)
			}
		})
	}
}

// newTestConfig returns a config with valid worker values for tests.
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{}
	cfg.App.Port = "18066"
	cfg.App.WriteTimeout = 120
	cfg.Auth.SessionKey = "0123456789abcdef0123456789abcdef"
	cfg.Scan.TimeoutSeconds = 75
	cfg.Scan.OCRPoolSize = 3
	cfg.Scan.PhashHighConf = 5
	cfg.Scan.PhashPotential = 10
	cfg.Catalog.Enabled = false
	cfg.Catalog.ImagesEnabled = false
	cfg.Worker.PriceSyncMinutes = 60
	cfg.Worker.MetadataTargets = "pokemon:en"
	cfg.Worker.RequestsPerSecond = 0.5
	cfg.Worker.RequestBurst = 1
	cfg.Worker.RetryAttempts = 3
	cfg.Worker.RetryBaseDelayMS = 1000
	cfg.Worker.CircuitFailures = 3
	cfg.Worker.CircuitCooldownSec = 300
	cfg.Worker.MaxPriceRatio = 5
	cfg.Worker.HistoryRetentionDays = 730
	cfg.Worker.FailurePath = t.TempDir() + "/worker-failures.jsonl"
	return cfg
}

func TestNewDataSyncWorker(t *testing.T) {
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := newTestConfig(t)
	dataWorker, err := newDataSyncWorker(database, cfg, nil)
	if err != nil {
		t.Fatalf("newDataSyncWorker() error = %v", err)
	}
	if dataWorker == nil {
		t.Fatal("newDataSyncWorker() returned nil worker")
	}
	t.Cleanup(func() { _ = dataWorker.Close() })
}

func TestNewDataSyncWorkerLegacyMetadataSync(t *testing.T) {
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := newTestConfig(t)
	cfg.Catalog.LegacyMetadataSync = true
	dataWorker, err := newDataSyncWorker(database, cfg, nil)
	if err != nil {
		t.Fatalf("newDataSyncWorker() error = %v", err)
	}
	if dataWorker == nil {
		t.Fatal("newDataSyncWorker() returned nil worker")
	}
	t.Cleanup(func() { _ = dataWorker.Close() })
}

func TestNewDataSyncWorkerErrors(t *testing.T) {
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	t.Run("empty failure path", func(t *testing.T) {
		cfg := newTestConfig(t)
		cfg.Worker.FailurePath = ""
		if _, err := newDataSyncWorker(database, cfg, nil); err == nil {
			t.Fatal("newDataSyncWorker() error = nil, want error")
		}
	})

	t.Run("invalid worker values", func(t *testing.T) {
		cfg := newTestConfig(t)
		cfg.Worker.RetryAttempts = 0
		if _, err := newDataSyncWorker(database, cfg, nil); err == nil {
			t.Fatal("newDataSyncWorker() error = nil, want error")
		}
	})
}
