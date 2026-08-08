package worker

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pokget/internal/models"
	"pokget/internal/service"

	"github.com/DATA-DOG/go-sqlmock"
)

// newTestImageServer serves a small deterministic PNG so the real
// MetadataService can download and fingerprint it without network access.
func newTestImageServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status < 200 || status >= 300 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(server.Close)
	return server
}

func newTestMetadataService() *service.MetadataService {
	return service.NewMetadataService(service.NewFingerprintService(nil))
}

type metadataClientStub struct {
	cards   []models.Card
	err     error
	calls   int
	game    string
	lang    string
	fetched chan struct{}
}

func (m *metadataClientStub) FetchCards(_ context.Context, game string, lang string) ([]models.Card, error) {
	m.calls++
	m.game = game
	m.lang = lang
	if m.fetched != nil {
		m.fetched <- struct{}{}
	}
	return m.cards, m.err
}

type failureSinkStub struct {
	records []FailureRecord
	err     error
}

func (s *failureSinkStub) StoreFailure(_ context.Context, record FailureRecord) error {
	s.records = append(s.records, record)
	return s.err
}

func TestSyncMissingFingerprints(t *testing.T) {
	server := newTestImageServer(t, http.StatusOK)

	t.Run("RepairsCardAndAdvancesCursor", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "image_url", "game", "language"}).
			AddRow("r-1", "Repairmon", server.URL+"/r-1.png", "pokemon", "en")
		mock.ExpectQuery("SELECT id, name, image_url, game, language").
			WithArgs("").
			WillReturnRows(rows)
		mock.ExpectExec("UPDATE cards SET phash").
			WithArgs(sqlmock.AnyArg(), "r-1").
			WillReturnResult(sqlmock.NewResult(0, 1))

		synced := false
		worker := &DataSyncWorker{
			db:              db,
			metadataService: newTestMetadataService(),
			OnSyncComplete:  func() { synced = true },
		}
		worker.syncMissingFingerprints(context.Background())

		if !synced {
			t.Fatal("OnSyncComplete was not called after a repair cycle")
		}
		if worker.repairAfterID != "r-1" {
			t.Fatalf("repairAfterID = %q, want %q", worker.repairAfterID, "r-1")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("QueryErrorKeepsCursor", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		mock.ExpectQuery("SELECT id, name, image_url, game, language").
			WithArgs("seed").
			WillReturnError(errors.New("db down"))

		worker := &DataSyncWorker{
			db:              db,
			metadataService: newTestMetadataService(),
			repairAfterID:   "seed",
		}
		worker.syncMissingFingerprints(context.Background())

		if worker.repairAfterID != "seed" {
			t.Fatalf("repairAfterID = %q, want unchanged %q", worker.repairAfterID, "seed")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("EmptyBatchResetsCursor", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		mock.ExpectQuery("SELECT id, name, image_url, game, language").
			WithArgs("seed").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "image_url", "game", "language"}))

		worker := &DataSyncWorker{
			db:              db,
			metadataService: newTestMetadataService(),
			repairAfterID:   "seed",
		}
		worker.syncMissingFingerprints(context.Background())

		if worker.repairAfterID != "" {
			t.Fatalf("repairAfterID = %q, want reset to empty", worker.repairAfterID)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ProcessFailureSkipsUpdate", func(t *testing.T) {
		broken := newTestImageServer(t, http.StatusInternalServerError)
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "image_url", "game", "language"}).
			AddRow("bad-1", "Broken", broken.URL+"/bad.png", "pokemon", "en")
		mock.ExpectQuery("SELECT id, name, image_url, game, language").
			WithArgs("").
			WillReturnRows(rows)
		// No UPDATE expectation: a failed download must not write a fingerprint.

		synced := false
		worker := &DataSyncWorker{
			db:              db,
			metadataService: newTestMetadataService(),
			OnSyncComplete:  func() { synced = true },
		}
		worker.syncMissingFingerprints(context.Background())

		if !synced {
			t.Fatal("cycle should still report completion after a card failure")
		}
		if worker.repairAfterID != "bad-1" {
			t.Fatalf("repairAfterID = %q, want %q", worker.repairAfterID, "bad-1")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("UpdateErrorStillCompletesCycle", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "image_url", "game", "language"}).
			AddRow("r-2", "Repairmon", server.URL+"/r-2.png", "pokemon", "en")
		mock.ExpectQuery("SELECT id, name, image_url, game, language").
			WithArgs("").
			WillReturnRows(rows)
		mock.ExpectExec("UPDATE cards SET phash").
			WithArgs(sqlmock.AnyArg(), "r-2").
			WillReturnError(errors.New("update failed"))

		synced := false
		worker := &DataSyncWorker{
			db:              db,
			metadataService: newTestMetadataService(),
			OnSyncComplete:  func() { synced = true },
		}
		worker.syncMissingFingerprints(context.Background())

		if !synced {
			t.Fatal("cycle should complete even when the fingerprint update fails")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSyncMetadata(t *testing.T) {
	server := newTestImageServer(t, http.StatusOK)

	t.Run("UpsertsCardAndFingerprint", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		imageURL := server.URL + "/m-1.png"
		mock.ExpectExec("INSERT INTO cards").
			WithArgs("m-1", "Metapod", "Base", imageURL, "pokemon").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("UPDATE cards SET phash").
			WithArgs(sqlmock.AnyArg(), imageURL, "m-1").
			WillReturnResult(sqlmock.NewResult(0, 1))

		client := &metadataClientStub{cards: []models.Card{
			{ID: "m-1", Name: "Metapod", Set: "Base", ImageURL: imageURL, Game: "pokemon"},
		}}
		synced := false
		worker := &DataSyncWorker{
			db:              db,
			metadataClient:  client,
			metadataService: newTestMetadataService(),
			metadataTargets: []MetadataTarget{{Game: "pokemon", Language: "en"}},
			OnSyncComplete:  func() { synced = true },
		}
		worker.syncMetadata(context.Background())

		if !synced {
			t.Fatal("OnSyncComplete was not called after a metadata cycle")
		}
		if client.calls != 1 || client.game != "pokemon" || client.lang != "en" {
			t.Fatalf("FetchCards calls/game/lang = %d/%q/%q", client.calls, client.game, client.lang)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("FetchErrorRecordsFailureAndContinues", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		client := &metadataClientStub{err: errors.New("upstream down")}
		sink := &failureSinkStub{}
		synced := false
		worker := &DataSyncWorker{
			db:              db,
			metadataClient:  client,
			metadataService: newTestMetadataService(),
			metadataTargets: []MetadataTarget{{Game: "Pokémon", Language: "en"}},
			failureSink:     sink,
			OnSyncComplete:  func() { synced = true },
		}
		worker.syncMetadata(context.Background())

		if !synced {
			t.Fatal("metadata cycle should complete after a fetch failure")
		}
		if len(sink.records) != 1 {
			t.Fatalf("stored failures = %d, want 1", len(sink.records))
		}
		record := sink.records[0]
		if record.Operation != "metadata" || record.Game != "pokemon" || record.Error == "" {
			t.Fatalf("unexpected failure record: %+v", record)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("NilMetadataServiceSkipsCycle", func(t *testing.T) {
		db, _, _ := sqlmock.New()
		defer db.Close()

		client := &metadataClientStub{}
		worker := &DataSyncWorker{
			db:             db,
			metadataClient: client,
		}
		worker.syncMetadata(context.Background())

		if client.calls != 0 {
			t.Fatalf("FetchCards called %d times with a nil metadata service", client.calls)
		}
	})

	t.Run("DefaultTargetsWhenUnconfigured", func(t *testing.T) {
		db, _, _ := sqlmock.New()
		defer db.Close()

		client := &metadataClientStub{err: errors.New("no feed")}
		worker := &DataSyncWorker{
			db:              db,
			metadataClient:  client,
			metadataService: newTestMetadataService(),
		}
		worker.syncMetadata(context.Background())

		if client.calls != 1 || client.game != "pokemon" || client.lang != "en" {
			t.Fatalf("default target calls/game/lang = %d/%q/%q", client.calls, client.game, client.lang)
		}
	})
}

func TestSyncMetadataCards(t *testing.T) {
	t.Run("InsertErrorStillFingerprints", func(t *testing.T) {
		server := newTestImageServer(t, http.StatusOK)
		db, mock, _ := sqlmock.New()
		defer db.Close()

		imageURL := server.URL + "/m-2.png"
		mock.ExpectExec("INSERT INTO cards").
			WithArgs("m-2", "Metapod", "Base", imageURL, "pokemon").
			WillReturnError(errors.New("duplicate key"))
		mock.ExpectExec("UPDATE cards SET phash").
			WithArgs(sqlmock.AnyArg(), imageURL, "m-2").
			WillReturnResult(sqlmock.NewResult(0, 1))

		worker := &DataSyncWorker{db: db, metadataService: newTestMetadataService()}
		worker.syncMetadataCards(context.Background(), []models.Card{
			{ID: "m-2", Name: "Metapod", Set: "Base", ImageURL: imageURL, Game: "pokemon"},
		})

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ProcessFailureSkipsFingerprintUpdate", func(t *testing.T) {
		broken := newTestImageServer(t, http.StatusInternalServerError)
		db, mock, _ := sqlmock.New()
		defer db.Close()

		imageURL := broken.URL + "/bad.png"
		mock.ExpectExec("INSERT INTO cards").
			WithArgs("m-3", "Metapod", "Base", imageURL, "pokemon").
			WillReturnResult(sqlmock.NewResult(0, 1))
		// No UPDATE expectation: a failed download must not write a fingerprint.

		worker := &DataSyncWorker{db: db, metadataService: newTestMetadataService()}
		worker.syncMetadataCards(context.Background(), []models.Card{
			{ID: "m-3", Name: "Metapod", Set: "Base", ImageURL: imageURL, Game: "pokemon"},
		})

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("CancelledContextSkipsRemainingCards", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		// No expectations: a cancelled context must stop before the first write.

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		worker := &DataSyncWorker{db: db, metadataService: newTestMetadataService()}
		worker.syncMetadataCards(ctx, []models.Card{
			{ID: "m-4", Name: "Metapod", Set: "Base", ImageURL: "http://unused", Game: "pokemon"},
		})

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestWaitForContext(t *testing.T) {
	if !waitForContext(context.Background(), time.Millisecond) {
		t.Fatal("waitForContext should return true once the timer fires")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitForContext(ctx, time.Hour) {
		t.Fatal("waitForContext should return false on cancellation")
	}
}

func TestStartRunsInitialMetadataAndRepairCycles(t *testing.T) {
	broken := newTestImageServer(t, http.StatusInternalServerError)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	// The repair card fails to download, so the cycle finishes quickly and
	// still reports completion through OnSyncComplete.
	rows := sqlmock.NewRows([]string{"id", "name", "image_url", "game", "language"}).
		AddRow("bad-1", "Broken", broken.URL+"/bad.png", "pokemon", "en")
	mock.ExpectQuery("SELECT id, name, image_url, game, language").
		WithArgs("").
		WillReturnRows(rows)

	client := &metadataClientStub{err: errors.New("no feed")}
	// Both the metadata and the fingerprint-repair cycle report completion;
	// wait for both before cancelling so the repair query always runs.
	completed := make(chan struct{}, 2)
	worker := &DataSyncWorker{
		db:              db,
		priceClient:     &service.MockPriceClient{},
		metadataClient:  client,
		metadataService: newTestMetadataService(),
		interval:        time.Hour,
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
		OnSyncComplete:  func() { completed <- struct{}{} },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Start(ctx)

	for range 2 {
		select {
		case <-completed:
		case <-time.After(2 * time.Second):
			t.Fatal("initial metadata/repair cycles did not complete")
		}
	}
	cancel()
	if err := worker.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("FetchCards calls = %d, want 1", client.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
