package worker

import (
	"context"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"pokget/internal/service"
)

func TestApplyBulkBatchUpdatesChangedPrices(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectExec("UPDATE cards AS c").
		WithArgs("scryfall", "card-a", 1.25, 1.10, "ygoprodeck", "1234", 0.33, 0.02).
		WillReturnResult(sqlmock.NewResult(0, 2))

	worker := &DataSyncWorker{db: db}
	affected, err := worker.applyBulkBatch(context.Background(), []service.BulkPriceUpdate{
		{SourceID: "scryfall", SourceCardID: "card-a", USD: 1.25, EUR: 1.10},
		{SourceID: "ygoprodeck", SourceCardID: "1234", USD: 0.33, EUR: 0.02},
	})
	if err != nil {
		t.Fatalf("applyBulkBatch: %v", err)
	}
	if affected != 2 {
		t.Fatalf("affected = %d, want 2", affected)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyBulkSourceFlushesPartialBatches(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectExec("UPDATE cards AS c").WillReturnResult(sqlmock.NewResult(0, 1))

	worker := &DataSyncWorker{db: db}
	err = worker.applyBulkSource(context.Background(), stubBulkSource{
		updates: []service.BulkPriceUpdate{{SourceID: "scryfall", SourceCardID: "card-a", USD: 1, EUR: 1}},
	})
	if err != nil {
		t.Fatalf("applyBulkSource: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyBulkSourcePropagatesErrors(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	worker := &DataSyncWorker{db: db}
	if err := worker.applyBulkSource(context.Background(), stubBulkSource{err: errors.New("download failed")}); err == nil {
		t.Fatal("expected the source error")
	}
}

func TestPriceMatchKeyNormalizesSetCodeAndNumber(t *testing.T) {
	cases := [][3]string{
		{"OP-01", "016/118", "op01|016"},
		{"OP01", "16", "op01|16"},
		{"SWSH1", "1", "swsh1|1"},
		{"Sword & Shield", "4", "swordshield|4"},
	}
	for _, tc := range cases {
		if got := priceMatchKey(tc[0], tc[1]); got != tc[2] {
			t.Fatalf("priceMatchKey(%q, %q) = %q, want %q", tc[0], tc[1], got, tc[2])
		}
	}
}

func TestApplyBulkSourceSkipsWithoutSources(t *testing.T) {
	worker := &DataSyncWorker{}
	worker.syncBulkPrices(context.Background())
}

type stubBulkSource struct {
	updates []service.BulkPriceUpdate
	err     error
}

func (s stubBulkSource) Name() string { return "stub" }

func (s stubBulkSource) FetchPrices(_ context.Context, emit func(service.BulkPriceUpdate) error) error {
	if s.err != nil {
		return s.err
	}
	for _, update := range s.updates {
		if err := emit(update); err != nil {
			return err
		}
	}
	return nil
}
