// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package worker

import (
	"context"
	"errors"
	"fmt"
	"pokget/internal/models"
	"pokget/internal/service"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
)

type recordingPriceClient struct {
	card  models.Card
	usd   float64
	eur   float64
	err   error
	calls int
}

func (c *recordingPriceClient) FetchPrice(card models.Card) (float64, float64, error) {
	c.calls++
	c.card = card
	return c.usd, c.eur, c.err
}

func (c *recordingPriceClient) ApplyMultiplier(price float64, _ string, _ map[string]float64) float64 {
	return price
}

func TestPriceSyncWorker_SyncPrices(t *testing.T) {
	card := models.Card{ID: "1", Name: "Charizard", Set: "Base", Game: "pokemon"}

	t.Run("SyncSuccess", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur", "game", "source_id", "source_card_id", "language", "price_checked_at"}).
			AddRow(card.ID, card.Name, card.Set, decimal.NewFromFloat(0), decimal.NewFromFloat(0), card.Game, "", "", "", time.Now())

		mock.ExpectQuery("count\\(DISTINCT tracked.user_id\\)").WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(1))
		mock.ExpectQuery("SELECT").WillReturnRows(rows)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE cards").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO price_history").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		client := &recordingPriceClient{usd: 150.0, eur: 140.0}
		worker := NewDataSyncWorker(db, client, nil, nil, time.Hour)
		worker.syncPrices(context.Background())
		if client.card.Game != card.Game {
			t.Fatalf("FetchPrice() game = %q, want %q", client.card.Game, card.Game)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("CommitErrorDoesNotEvaluateAlerts", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur", "game", "source_id", "source_card_id", "language", "price_checked_at"}).
			AddRow(card.ID, card.Name, card.Set, decimal.Zero, decimal.Zero, card.Game, "", "", "", time.Now())
		mock.ExpectQuery("count\\(DISTINCT tracked.user_id\\)").WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(1))
		mock.ExpectQuery("SELECT").WillReturnRows(rows)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE cards").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO price_history").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit().WillReturnError(errors.New("commit failed"))

		worker := NewDataSyncWorker(
			db,
			&service.MockPriceClient{FixedUSD: 150, FixedEUR: 140},
			nil,
			nil,
			time.Hour,
		)
		worker.syncPrices(context.Background())

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unexpected database operation after commit failure: %v", err)
		}
	})

	t.Run("NilPriceClient", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		worker := NewDataSyncWorker(db, nil, nil, nil, time.Hour)
		worker.syncPrices(context.Background())

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("nil price client touched the database: %v", err)
		}
	})

	t.Run("QueryError", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		mock.ExpectQuery("count\\(DISTINCT tracked.user_id\\)").WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(1))
		mock.ExpectQuery("SELECT").WillReturnError(errors.New("db error"))

		worker := NewDataSyncWorker(db, &service.MockPriceClient{}, nil, nil, time.Hour)
		worker.syncPrices(context.Background())

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("ScanError", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		// Return a row with wrong type to trigger Scan error
		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur", "game", "source_id", "source_card_id", "language", "price_checked_at"}).
			AddRow("1", "C", "S", "not-a-decimal", 0, "pokemon", "", "", "", time.Now())

		mock.ExpectQuery("count\\(DISTINCT tracked.user_id\\)").WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(1))
		mock.ExpectQuery("SELECT").WillReturnRows(rows)

		worker := NewDataSyncWorker(db, &service.MockPriceClient{}, nil, nil, time.Hour)
		worker.syncPrices(context.Background())

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("FetchError", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur", "game", "source_id", "source_card_id", "language", "price_checked_at"}).
			AddRow(card.ID, card.Name, card.Set, decimal.Zero, decimal.Zero, card.Game, "", "", "", time.Now())

		mock.ExpectQuery("count\\(DISTINCT tracked.user_id\\)").WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(1))
		mock.ExpectQuery("SELECT").WillReturnRows(rows)

		client := &service.MockPriceClient{Err: errors.New("fetch error")}
		worker := NewDataSyncWorker(db, client, nil, nil, time.Hour)
		worker.syncPrices(context.Background())

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("BlockedSourceEndsCycle", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur", "game", "source_id", "source_card_id", "language", "price_checked_at"}).
			AddRow("1", "First", "Set", decimal.Zero, decimal.Zero, "pokemon", "", "", "", time.Now()).
			AddRow("2", "Second", "Set", decimal.Zero, decimal.Zero, "pokemon", "", "", "", time.Now())
		mock.ExpectQuery("count\\(DISTINCT tracked.user_id\\)").WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(1))
		mock.ExpectQuery("SELECT").WillReturnRows(rows)

		client := &recordingPriceClient{err: service.ErrPriceSourceBlocked}
		worker := NewDataSyncWorker(db, client, nil, nil, time.Hour)
		worker.syncPrices(context.Background())

		if client.calls != 1 {
			t.Fatalf("FetchPrice() calls = %d, want 1 after source block", client.calls)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("blocked source cycle: %v", err)
		}
	})

	t.Run("UpdateError", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur", "game", "source_id", "source_card_id", "language", "price_checked_at"}).
			AddRow(card.ID, card.Name, card.Set, decimal.Zero, decimal.Zero, card.Game, "", "", "", time.Now())

		mock.ExpectQuery("count\\(DISTINCT tracked.user_id\\)").WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(1))
		mock.ExpectQuery("SELECT").WillReturnRows(rows)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE cards").WillReturnError(errors.New("upd error"))
		mock.ExpectRollback()

		client := &service.MockPriceClient{FixedUSD: 1.0, FixedEUR: 1.0}
		worker := NewDataSyncWorker(db, client, nil, nil, time.Hour)
		worker.syncPrices(context.Background())

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("HistoryError", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur", "game", "source_id", "source_card_id", "language", "price_checked_at"}).
			AddRow(card.ID, card.Name, card.Set, decimal.Zero, decimal.Zero, card.Game, "", "", "", time.Now())

		mock.ExpectQuery("count\\(DISTINCT tracked.user_id\\)").WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(1))
		mock.ExpectQuery("SELECT").WillReturnRows(rows)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE cards").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO price_history").WillReturnError(errors.New("hist error"))
		mock.ExpectRollback()

		client := &service.MockPriceClient{FixedUSD: 1.0, FixedEUR: 1.0}
		worker := NewDataSyncWorker(db, client, nil, nil, time.Hour)
		worker.syncPrices(context.Background())

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("SkipZeroPrice", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur", "game", "source_id", "source_card_id", "language", "price_checked_at"}).
			AddRow(card.ID, card.Name, card.Set, decimal.NewFromFloat(150), decimal.NewFromFloat(140), card.Game, "", "", "", time.Now())

		mock.ExpectQuery("count\\(DISTINCT tracked.user_id\\)").WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(1))
		mock.ExpectQuery("SELECT").WillReturnRows(rows)
		// A failed scrape returns (0, 0): the worker must NOT issue UPDATE/INSERT,
		// otherwise it would wipe the valid stored price. No Exec expectations set,
		// so any DB write would make ExpectationsWereMet fail.

		client := &service.MockPriceClient{FixedUSD: 0, FixedEUR: 0}
		worker := NewDataSyncWorker(db, client, nil, nil, time.Hour)
		worker.syncPrices(context.Background())

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met (zero price should be skipped): %v", err)
		}
	})

	t.Run("PartialPricePreservesFailedCurrency", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur", "game", "source_id", "source_card_id", "language", "price_checked_at"}).
			AddRow(card.ID, card.Name, card.Set, decimal.NewFromInt(150), decimal.NewFromInt(100), card.Game, "", "", "", time.Now())
		mock.ExpectQuery("count\\(DISTINCT tracked.user_id\\)").WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(1))
		mock.ExpectQuery("SELECT").WillReturnRows(rows)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE cards").
			WithArgs(decimal.NewFromInt(150), decimal.NewFromInt(110), true, card.ID, false).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO price_history").
			WithArgs(card.ID, decimal.NewFromInt(150), decimal.NewFromInt(110)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		client := &service.MockPriceClient{FixedUSD: 0, FixedEUR: 110}
		worker := NewDataSyncWorker(db, client, nil, nil, time.Hour)
		worker.syncPrices(context.Background())

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("partial price update did not preserve USD: %v", err)
		}
	})

	// A stored price no source verified for a week must not reject a real market
	// move forever: the 6x jump is accepted instead of skipped.
	t.Run("StaleReferencePriceDoesNotVetoAJump", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur", "game", "source_id", "source_card_id", "language", "price_checked_at"}).
			AddRow(card.ID, card.Name, card.Set, decimal.NewFromInt(100), decimal.NewFromInt(100), card.Game, "", "", "", time.Now().Add(-8*24*time.Hour))
		mock.ExpectQuery("count\\(DISTINCT tracked.user_id\\)").WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(1))
		mock.ExpectQuery("SELECT").WillReturnRows(rows)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE cards").
			WithArgs(decimal.NewFromInt(600), decimal.NewFromInt(600), true, card.ID, false).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO price_history").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		worker := NewDataSyncWorker(db, &service.MockPriceClient{FixedUSD: 600, FixedEUR: 600}, nil, nil, time.Hour)
		worker.maxPriceRatio = 5
		worker.syncPrices(context.Background())

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("stale reference price still vetoed the candidate: %v", err)
		}
	})

	// A rejected USD keeps the reference age: the accepted EUR is written, but
	// price_checked_at stays so the USD waiver still ages out after a week.
	t.Run("RejectedCurrencyKeepsTheCheckStamp", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur", "game", "source_id", "source_card_id", "language", "price_checked_at"}).
			AddRow(card.ID, card.Name, card.Set, decimal.NewFromInt(100), decimal.NewFromInt(100), card.Game, "", "", "", time.Now().Add(-time.Hour))
		mock.ExpectQuery("count\\(DISTINCT tracked.user_id\\)").WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(1))
		mock.ExpectQuery("SELECT").WillReturnRows(rows)
		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE cards[\s\S]*price_checked_at = CASE WHEN \$5 THEN price_checked_at ELSE NOW\(\) END`).
			WithArgs(decimal.NewFromInt(100), decimal.NewFromInt(110), true, card.ID, true).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO price_history").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		worker := NewDataSyncWorker(db, &service.MockPriceClient{FixedUSD: 600, FixedEUR: 110}, nil, nil, time.Hour)
		worker.maxPriceRatio = 5
		worker.syncPrices(context.Background())

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("the rejected USD must not refresh the check stamp: %v", err)
		}
	})

	// Nothing creates or delivers price alerts, so the sync must neither rank nor
	// evaluate them. The matcher records every statement independently: two
	// never-matching sentinels stay pending in unordered mode, so even a statement
	// issued after the real expectations are used up still reaches the matcher.
	t.Run("IgnoresPriceAlerts", func(t *testing.T) {
		const sentinel = "sentinel: never matches"
		var alertSQL []string
		noAlerts := sqlmock.QueryMatcherFunc(func(expected, actual string) error {
			if strings.Contains(actual, "price_alerts") {
				alertSQL = append(alertSQL, actual)
				return fmt.Errorf("statement touches price_alerts: %s", actual)
			}
			if expected == sentinel {
				return errors.New(sentinel)
			}
			return sqlmock.QueryMatcherRegexp.Match(expected, actual)
		})
		db, mock, _ := sqlmock.New(sqlmock.QueryMatcherOption(noAlerts))
		defer db.Close()
		mock.MatchExpectationsInOrder(false)

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur", "game", "source_id", "source_card_id", "language", "price_checked_at"}).
			AddRow(card.ID, card.Name, card.Set, decimal.Zero, decimal.Zero, card.Game, "", "", "", time.Now())

		mock.ExpectQuery("count\\(DISTINCT tracked.user_id\\)").WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(1))
		// The tracked query ranks the cards per user; the budget is 100 cards each.
		mock.ExpectQuery("WITH tracked AS[\\s\\S]*position <= \\$1").WithArgs(100, 100).WillReturnRows(rows)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE cards").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO price_history").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		// Registered last: ExpectationsWereMet reports the first unmet expectation, so
		// a real one still pending would be named before the sentinels.
		mock.ExpectQuery(sentinel)
		mock.ExpectExec(sentinel)

		client := &service.MockPriceClient{FixedUSD: 150.0, FixedEUR: 140.0}
		worker := NewDataSyncWorker(db, client, nil, nil, time.Hour)
		worker.syncPrices(context.Background())

		if len(alertSQL) > 0 {
			t.Fatalf("the sync issued price_alerts statements: %q", alertSQL)
		}
		// Only the sentinels may remain unmet.
		if err := mock.ExpectationsWereMet(); err == nil || !strings.Contains(err.Error(), sentinel) {
			t.Fatalf("unexpected unmet expectations: %v", err)
		}
	})
}

func TestWorkerLifecycle(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()
	client := &service.MockPriceClient{}

	t.Run("ContextCancel", func(_ *testing.T) {
		worker := NewDataSyncWorker(db, client, nil, nil, 50*time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		worker.Start(ctx)
	})

	t.Run("StopSignal", func(_ *testing.T) {
		worker := NewDataSyncWorker(db, client, nil, nil, 50*time.Millisecond)
		ctx := context.Background()
		go func() {
			time.Sleep(20 * time.Millisecond)
			worker.Stop()
		}()
		worker.Start(ctx)
	})

	t.Run("TickerExecution", func(t *testing.T) {
		db2, mock, _ := sqlmock.New()
		defer db2.Close()

		mock.ExpectQuery("count\\(DISTINCT tracked.user_id\\)").WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(1))
		mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur", "game", "source_id", "source_card_id", "language", "price_checked_at"}))

		worker := NewDataSyncWorker(db2, client, nil, nil, 10*time.Millisecond)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(25 * time.Millisecond)
			cancel()
		}()
		worker.Start(ctx)

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})
}
