package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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
		WithArgs(0.9).
		WillReturnResult(sqlmock.NewResult(0, 3))

	worker := &DataSyncWorker{db: db, eurRate: fixedEURRate{rate: 0.9}}
	worker.applyEURFallback(context.Background())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
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
