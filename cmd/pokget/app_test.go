package main

import (
	"context"
	"errors"
	"html/template"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pokget/internal/handlers"
	"pokget/internal/service"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDeriveKey(t *testing.T) {
	t.Parallel()

	key := deriveKey("master-key", "pokget:csrf:auth")
	if len(key) != 32 {
		t.Fatalf("deriveKey length = %d, want 32", len(key))
	}
	again := deriveKey("master-key", "pokget:csrf:auth")
	if string(key) != string(again) {
		t.Fatal("deriveKey is not deterministic")
	}
	other := deriveKey("master-key", "pokget:crypto:aes256")
	if string(key) == string(other) {
		t.Fatal("deriveKey returns the same key for different purposes")
	}
}

func TestTemplateFuncMap(t *testing.T) {
	t.Parallel()

	funcs := templateFuncMap()
	div, ok := funcs["div"].(func(float64, float64) float64)
	if !ok {
		t.Fatal("template func div has unexpected type")
	}
	if got := div(10, 4); got != 2.5 {
		t.Fatalf("div(10, 4) = %v, want 2.5", got)
	}
	if got := div(1, 0); got != 0 {
		t.Fatalf("div(1, 0) = %v, want 0", got)
	}
	mul, ok := funcs["mul"].(func(float64, float64) float64)
	if !ok {
		t.Fatal("template func mul has unexpected type")
	}
	if got := mul(3, 4); got != 12 {
		t.Fatalf("mul(3, 4) = %v, want 12", got)
	}
}

func TestNewCryptoService(t *testing.T) {
	t.Parallel()

	cryptoSvc, err := newCryptoService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("newCryptoService() error = %v", err)
	}
	if cryptoSvc == nil {
		t.Fatal("newCryptoService() returned nil service")
	}
}

func TestDetectBuildVersion(t *testing.T) {
	t.Run("missing asset falls back to 1", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if got := detectBuildVersion(); got != "1" {
			t.Fatalf("detectBuildVersion() = %q, want %q", got, "1")
		}
	})

	t.Run("uses asset modification time", func(t *testing.T) {
		dir := t.TempDir()
		cssDir := filepath.Join(dir, "static", "css")
		if err := os.MkdirAll(cssDir, 0o755); err != nil {
			t.Fatal(err)
		}
		cssPath := filepath.Join(cssDir, "tailwind.css")
		if err := os.WriteFile(cssPath, []byte("/* css */"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)

		info, err := os.Stat(cssPath)
		if err != nil {
			t.Fatal(err)
		}
		want := strconv.FormatInt(info.ModTime().Unix(), 10)
		if got := detectBuildVersion(); got != want {
			t.Fatalf("detectBuildVersion() = %q, want %q", got, want)
		}
	})
}

var cardCacheColumns = []string{
	"id", "name", "set_name", "price_usd", "price_eur", "image_url", "variant",
	"change_24h", "phash", "game", "language", "rarity", "set_code",
	"collector_number", "catalog_active",
}

func TestLoadCardsCache(t *testing.T) {
	t.Run("nil database returns no cards", func(t *testing.T) {
		t.Parallel()
		if cards := loadCardsCache(nil); len(cards) != 0 {
			t.Fatalf("loadCardsCache(nil) returned %d cards, want 0", len(cards))
		}
	})

	t.Run("loads rows into cache", func(t *testing.T) {
		t.Parallel()
		database, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close() })

		rows := sqlmock.NewRows(cardCacheColumns).
			AddRow("card-1", "Charizard", "Base Set", 120.5, 110.0, "https://img.test/1.png", "Holo", 1.5, int64(99), "pokemon", "en", "Rare", "BS", "4", true).
			AddRow("card-2", "Nami", "Romance Dawn", 15.0, 14.0, "", "Parallel", 0, nil, "one_piece", "jp", "", "OP01", "016", false)
		mock.ExpectQuery("SELECT id, name, set_name").WillReturnRows(rows)

		cards := loadCardsCache(database)
		if len(cards) != 2 {
			t.Fatalf("loadCardsCache() returned %d cards, want 2", len(cards))
		}
		if cards[0].ID != "card-1" || cards[0].Name != "Charizard" || cards[0].Set != "Base Set" {
			t.Fatalf("first card = %+v, want card-1 Charizard Base Set", cards[0])
		}
		if cards[0].Phash == nil || *cards[0].Phash != 99 {
			t.Fatalf("first card phash = %v, want 99", cards[0].Phash)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("query error returns no cards", func(t *testing.T) {
		t.Parallel()
		database, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close() })

		mock.ExpectQuery("SELECT id, name, set_name").WillReturnError(errors.New("boom"))
		if cards := loadCardsCache(database); len(cards) != 0 {
			t.Fatalf("loadCardsCache() returned %d cards, want 0", len(cards))
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("row iteration error is logged and tolerated", func(t *testing.T) {
		t.Parallel()
		database, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close() })

		rows := sqlmock.NewRows(cardCacheColumns).
			AddRow("card-1", "Charizard", "Base Set", 120.5, 110.0, "", "Holo", 0, nil, "pokemon", "en", "", "BS", "4", true).
			RowError(1, errors.New("stream reset"))
		mock.ExpectQuery("SELECT id, name, set_name").WillReturnRows(rows)

		cards := loadCardsCache(database)
		if len(cards) != 1 {
			t.Fatalf("loadCardsCache() returned %d cards, want 1", len(cards))
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestNewHandler(t *testing.T) {
	t.Parallel()

	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := newTestConfig(t)
	cfg.App.SecureCookies = false
	services := &appServices{}
	cryptoSvc, err := newCryptoService(cfg.Auth.SessionKey)
	if err != nil {
		t.Fatal(err)
	}
	llmSvc := service.NewLLMService()
	templates := template.Must(template.New("").Parse(`{{define "index"}}{{end}}`))
	allCards := loadCardsCache(nil)

	h := newHandler(cfg, database, templates, allCards, services, nil, cryptoSvc, llmSvc, "42")
	if h == nil {
		t.Fatal("newHandler() returned nil")
	}
	if h.Templates != templates {
		t.Error("handler Templates not wired")
	}
	if h.DB != database {
		t.Error("handler DB not wired")
	}
	if h.Crypto != cryptoSvc {
		t.Error("handler Crypto not wired")
	}
	if h.LLM != llmSvc {
		t.Error("handler LLM not wired")
	}
	if h.BuildVersion != "42" {
		t.Errorf("handler BuildVersion = %q, want %q", h.BuildVersion, "42")
	}
	if h.ScanTimeout != 75*time.Second {
		t.Errorf("handler ScanTimeout = %v, want 75s", h.ScanTimeout)
	}
	if h.SecureCookies {
		t.Error("handler SecureCookies = true, want false")
	}
	if h.Game == nil {
		t.Error("handler Game service is nil")
	}
	if h.PriceClient == nil {
		t.Error("handler PriceClient is nil")
	}
}

func TestInitServicesNilDatabase(t *testing.T) {
	t.Parallel()

	services, err := initServices(newTestConfig(t), nil, &atomic.Bool{})
	if err != nil {
		t.Fatalf("initServices() error = %v", err)
	}
	if services == nil {
		t.Fatal("initServices() returned nil")
	}
	if services.fingerprintSvc != nil || services.auditSvc != nil || services.dataWorker != nil {
		t.Fatalf("initServices() = %+v, want zero-value services", services)
	}
}

// expectServiceInitQueries registers the sqlmock expectations for the queries
// run while building services: fingerprint loading and the seeding guard.
func expectServiceInitQueries(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT id, name, set_name").
		WillReturnRows(sqlmock.NewRows(cardCacheColumns))
	mock.ExpectQuery("SELECT EXISTS").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
}

func TestInitServicesCatalogDisabled(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	expectServiceInitQueries(mock)

	cfg := newTestConfig(t)
	services, err := initServices(cfg, database, &atomic.Bool{})
	if err != nil {
		t.Fatalf("initServices() error = %v", err)
	}
	if services.fingerprintSvc == nil {
		t.Error("fingerprint service is nil")
	}
	if services.auditSvc == nil {
		t.Error("audit service is nil")
	}
	if services.dataWorker == nil {
		t.Error("data sync worker is nil")
	}
	if services.catalogRepository != nil || services.catalogWorker != nil || services.catalogImageWorker != nil {
		t.Error("catalog services initialized while catalog is disabled")
	}
	if got := services.fingerprintSvc.PhashHighConf; got != cfg.Scan.PhashHighConf {
		t.Errorf("PhashHighConf = %d, want %d", got, cfg.Scan.PhashHighConf)
	}
	if got := services.fingerprintSvc.PhashPotential; got != cfg.Scan.PhashPotential {
		t.Errorf("PhashPotential = %d, want %d", got, cfg.Scan.PhashPotential)
	}
	if service.OCRPoolSize != cfg.Scan.OCRPoolSize {
		t.Errorf("OCRPoolSize = %d, want %d", service.OCRPoolSize, cfg.Scan.OCRPoolSize)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInitServicesCatalogEnabled(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	expectServiceInitQueries(mock)

	cfg := newTestConfig(t)
	cfg.Catalog.Enabled = true
	cfg.Catalog.ImagesEnabled = true
	cfg.Catalog.Language = "en"
	cfg.Catalog.SyncIntervalMins = 360
	cfg.Catalog.BatchSize = 500
	cfg.Catalog.RequestDelayMS = 100
	cfg.Catalog.ImageStore = t.TempDir()
	cfg.Catalog.ImageBatchSize = 8
	cfg.Catalog.ImagePollIntervalMS = 5000

	var fingerprintIndexDirty atomic.Bool
	services, err := initServices(cfg, database, &fingerprintIndexDirty)
	if err != nil {
		t.Fatalf("initServices() error = %v", err)
	}
	if services.catalogRepository == nil {
		t.Error("catalog repository is nil")
	}
	if services.catalogWorker == nil {
		t.Error("catalog worker is nil")
	}
	if services.catalogImageWorker == nil {
		t.Error("catalog image worker is nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInitServicesErrors(t *testing.T) {
	t.Run("data sync worker failure", func(t *testing.T) {
		database, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close() })
		expectServiceInitQueries(mock)

		cfg := newTestConfig(t)
		cfg.Worker.FailurePath = ""
		services, err := initServices(cfg, database, &atomic.Bool{})
		if err == nil || !strings.Contains(err.Error(), "data sync worker initialization failed") {
			t.Fatalf("initServices() error = %v, want data sync worker failure", err)
		}
		if services != nil {
			t.Fatalf("initServices() = %+v, want nil services on error", services)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("image processor failure", func(t *testing.T) {
		database, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close() })
		expectServiceInitQueries(mock)

		cfg := newTestConfig(t)
		cfg.Catalog.ImagesEnabled = true
		cfg.Catalog.ImageStore = ""
		_, err = initServices(cfg, database, &atomic.Bool{})
		if err == nil || !strings.Contains(err.Error(), "catalog image processor initialization failed") {
			t.Fatalf("initServices() error = %v, want image processor failure", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestStartBackgroundWorkers(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:9")

	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	expectServiceInitQueries(mock)

	cfg := newTestConfig(t)
	var fingerprintIndexDirty atomic.Bool
	services, err := initServices(cfg, database, &fingerprintIndexDirty)
	if err != nil {
		t.Fatalf("initServices() error = %v", err)
	}

	h := &handlers.Handler{}
	llmSvc := service.NewLLMService()

	// Pre-cancel the worker context so every worker returns immediately
	// without touching the network or the database.
	workerCtx, workerCancel := context.WithCancel(context.Background())
	workerCancel()

	var backgroundWorkers sync.WaitGroup
	startBackgroundWorkers(workerCtx, llmSvc, services, h, &backgroundWorkers, &fingerprintIndexDirty)

	done := make(chan struct{})
	go func() {
		backgroundWorkers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("background workers did not stop after context cancellation")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStartBackgroundWorkersCatalogEnabled(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:9")

	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	expectServiceInitQueries(mock)

	cfg := newTestConfig(t)
	cfg.Catalog.Enabled = true
	cfg.Catalog.ImagesEnabled = true
	cfg.Catalog.Language = "en"
	cfg.Catalog.SyncIntervalMins = 360
	cfg.Catalog.BatchSize = 500
	cfg.Catalog.RequestDelayMS = 100
	cfg.Catalog.ImageStore = t.TempDir()
	cfg.Catalog.ImageBatchSize = 8
	cfg.Catalog.ImagePollIntervalMS = 5000

	var fingerprintIndexDirty atomic.Bool
	services, err := initServices(cfg, database, &fingerprintIndexDirty)
	if err != nil {
		t.Fatalf("initServices() error = %v", err)
	}

	h := &handlers.Handler{DB: database, Fingerprint: services.fingerprintSvc}
	llmSvc := service.NewLLMService()

	// Pre-cancel the worker context so every worker returns immediately
	// without touching the network or the database.
	workerCtx, workerCancel := context.WithCancel(context.Background())
	workerCancel()

	var backgroundWorkers sync.WaitGroup
	startBackgroundWorkers(workerCtx, llmSvc, services, h, &backgroundWorkers, &fingerprintIndexDirty)

	done := make(chan struct{})
	go func() {
		backgroundWorkers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("background workers did not stop after context cancellation")
	}

	// Invoke the wired change callbacks to exercise the cache reload path.
	// Each reload runs the cards query and then the fingerprint reload query.
	expectReload := func() {
		mock.ExpectQuery("SELECT id, name, set_name").
			WillReturnRows(sqlmock.NewRows(cardCacheColumns))
		mock.ExpectQuery("SELECT id, name, set_name").
			WillReturnRows(sqlmock.NewRows(cardCacheColumns))
	}
	expectReload()
	services.dataWorker.OnSyncComplete()
	expectReload()
	services.catalogWorker.OnChanged()

	mock.ExpectQuery("SELECT id, name, set_name").WillReturnError(errors.New("boom"))
	services.dataWorker.OnSyncComplete() // reload failure is logged, not fatal

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
