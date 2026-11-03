package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
)

type fixedEURRate struct {
	rate float64
	err  error
}

func (f fixedEURRate) USDEUR(context.Context) (float64, error) { return f.rate, f.err }

func TestApplyEURFallbackConvertsOnlyUnpricedEuros(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectExec("UPDATE cards[\\s\\S]*price_eur_estimated = TRUE").
		WithArgs(0.9, fxBatchSize).
		WillReturnResult(sqlmock.NewResult(0, 3))
	// An empty batch ends the loop; a short one does not (rows priced by the sync
	// are filtered by the row recheck).
	mock.ExpectExec("UPDATE cards[\\s\\S]*price_eur_estimated = TRUE").
		WithArgs(0.9, fxBatchSize).
		WillReturnResult(sqlmock.NewResult(0, 0))

	worker := &DataSyncWorker{db: db, eurRate: fixedEURRate{rate: 0.9}}
	worker.applyEURFallback(context.Background())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// The conversion runs in id-ordered slices and retries a lost lock race instead
// of abandoning the whole catalog.
func TestApplyEURFallbackRetriesADeadlock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	deadlock := &pq.Error{Code: "40P01", Message: "deadlock detected"}
	mock.ExpectExec("UPDATE cards[\\s\\S]*LIMIT \\$2").WithArgs(0.9, fxBatchSize).WillReturnError(deadlock)
	// The retry converts fewer rows than the batch size, so the loop stops.
	mock.ExpectExec("UPDATE cards[\\s\\S]*LIMIT \\$2").WithArgs(0.9, fxBatchSize).WillReturnResult(sqlmock.NewResult(0, 7))
	mock.ExpectExec("UPDATE cards[\\s\\S]*LIMIT \\$2").WithArgs(0.9, fxBatchSize).WillReturnResult(sqlmock.NewResult(0, 0))

	worker := &DataSyncWorker{db: db, eurRate: fixedEURRate{rate: 0.9}}
	worker.applyEURFallback(context.Background())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRetryablePostgresError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&pq.Error{Code: "40P01"}, true},
		{&pq.Error{Code: "40001"}, true},
		{&pq.Error{Code: "23505"}, false},
		{errors.New("plain"), false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := retryablePostgresError(tc.err); got != tc.want {
			t.Fatalf("retryablePostgresError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestApplyEURFallbackIsSkippedWithoutAProvider(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectExec("UPDATE cards").WillReturnResult(sqlmock.NewResult(0, 1))

	worker := &DataSyncWorker{db: db}
	worker.applyEURFallback(context.Background())

	// No provider means no statement: the mock must stay untouched.
	if err := mock.ExpectationsWereMet(); err == nil {
		t.Fatal("expected the pending UPDATE expectation to remain unmatched")
	}
}

func TestApplyEURFallbackSurvivesAFailingProvider(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectExec("UPDATE cards").WillReturnResult(sqlmock.NewResult(0, 1))

	worker := &DataSyncWorker{db: db, eurRate: fixedEURRate{err: errors.New("api down")}}
	worker.applyEURFallback(context.Background())

	if err := mock.ExpectationsWereMet(); err == nil {
		t.Fatal("a failing rate provider must not touch the database")
	}
}
