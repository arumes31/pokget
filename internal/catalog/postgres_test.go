package catalog

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

const testRunID = "11111111-1111-1111-1111-111111111111"

func TestNewPostgresRepository(t *testing.T) {
	t.Parallel()

	if _, err := NewPostgresRepository(nil); err == nil {
		t.Fatal("NewPostgresRepository(nil) error = nil, want error")
	}
}

func TestPostgresRepositoryBeginRun(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE catalog_source_state")).
		WithArgs("tcgdex").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO catalog_sync_runs")).
		WithArgs("tcgdex", SyncModeFull).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testRunID))
	mock.ExpectCommit()

	runID, err := repository.BeginRun(context.Background(), "tcgdex", SyncModeFull)
	if err != nil {
		t.Fatalf("BeginRun() error = %v", err)
	}
	if runID != testRunID {
		t.Fatalf("BeginRun() = %q, want %q", runID, testRunID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryBeginRun_SourceNotFound(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE catalog_source_state")).
		WithArgs("missing").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	_, err = repository.BeginRun(context.Background(), "missing", SyncModeFull)
	if !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("BeginRun() error = %v, want ErrSourceNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryUpsertBatch(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatal(err)
	}

	cardID, err := CardID("ygoprodeck", "46986414", "en")
	if err != nil {
		t.Fatal(err)
	}
	printingID, err := PrintingID("ygoprodeck", "LOB-005", "en", "Normal")
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT run.source_id, source.game")).
		WithArgs(testRunID).
		WillReturnRows(sqlmock.NewRows([]string{"source_id", "game"}).AddRow("ygoprodeck", "yugioh"))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO cards")).
		WillReturnRows(sqlmock.NewRows([]string{"inserted"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO card_images")).
		WithArgs(cardID, "46986414", "front", "https://images.example/46986414.jpg").
		WillReturnRows(sqlmock.NewRows([]string{"id", "inserted"}).AddRow(42, true))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM card_fingerprints")).
		WithArgs(int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO catalog_printings")).
		WillReturnRows(sqlmock.NewRows([]string{"inserted"}).AddRow(true))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM catalog_printing_images")).
		WithArgs(printingID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO catalog_printing_images")).
		WithArgs(printingID, int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	counts, err := repository.UpsertBatch(
		context.Background(),
		Batch{
			RunID:    testRunID,
			SourceID: "ygoprodeck",
			Game:     GameYuGiOh,
			Records: []CardRecord{
				{
					SourceCardID: "46986414",
					Name:         "Dark Magician",
					SetName:      "Multiple sets",
					Language:     "en",
					Images: []ImageRecord{
						{
							SourceImageID: "46986414",
							URL:           "https://images.example/46986414.jpg",
						},
					},
					Printings: []PrintingRecord{
						{
							SourcePrintingID: "LOB-005",
							SetCode:          "LOB",
							SetName:          "Legend of Blue Eyes White Dragon",
							CollectorNumber:  "005",
							Language:         "en",
							SourceImageIDs:   []string{"46986414"},
						},
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("UpsertBatch() error = %v", err)
	}
	if counts.CardsInserted != 1 || counts.ImagesInserted != 1 || counts.PrintingsInserted != 1 {
		t.Fatalf("UpsertBatch() counts = %+v", counts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryCompleteRun_DeactivatesOnlyCompleteFullSnapshot(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT source_id, mode")).
		WithArgs(testRunID).
		WillReturnRows(sqlmock.NewRows([]string{"source_id", "mode"}).AddRow("tcgdex", "full"))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE cards")).
		WithArgs("tcgdex", testRunID).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE catalog_printings")).
		WithArgs("tcgdex", testRunID).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE catalog_sync_runs")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE catalog_source_state")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	counts, err := repository.CompleteRun(
		context.Background(),
		testRunID,
		Completion{Fetch: FetchResult{CompleteSnapshot: true, Count: 10}},
	)
	if err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}
	if counts.CardsDeactivated != 2 || counts.PrintingsDeactivated != 3 {
		t.Fatalf("CompleteRun() counts = %+v", counts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryUpsertBatch_EmptyBatchDoesNotOpenTransaction(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.UpsertBatch(
		context.Background(),
		Batch{RunID: testRunID, SourceID: "tcgdex", Game: GamePokemon, Records: []CardRecord{}},
	)
	if err != nil {
		t.Fatalf("UpsertBatch() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
