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
	"pokget/internal/models"
	"pokget/internal/service"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
)

func TestPriceSyncWorker_SyncPrices(t *testing.T) {
	card := models.Card{ID: "1", Name: "Charizard", Set: "Base"}

	t.Run("SyncSuccess", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"}).
			AddRow(card.ID, card.Name, card.Set, decimal.NewFromFloat(0), decimal.NewFromFloat(0))

		mock.ExpectQuery("SELECT").WillReturnRows(rows)
		mock.ExpectExec("UPDATE cards").WillReturnResult(sqlmock.NewResult(1, 1))
		alertRows := sqlmock.NewRows([]string{"id", "user_id", "target_price"})
		mock.ExpectQuery("SELECT id, user_id, target_price FROM price_alerts").WillReturnRows(alertRows)
		mock.ExpectExec("INSERT INTO price_history").WillReturnResult(sqlmock.NewResult(1, 1))

		client := &service.MockPriceClient{FixedUSD: 150.0, FixedEUR: 140.0}
		worker := NewPriceSyncWorker(db, client, time.Hour)
		worker.syncPrices()

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("QueryError", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		mock.ExpectQuery("SELECT").WillReturnError(errors.New("db error"))

		worker := NewPriceSyncWorker(db, &service.MockPriceClient{}, time.Hour)
		worker.syncPrices()

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("ScanError", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		// Return a row with wrong type to trigger Scan error
		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"}).
			AddRow("1", "C", "S", "not-a-decimal", 0)

		mock.ExpectQuery("SELECT").WillReturnRows(rows)

		worker := NewPriceSyncWorker(db, &service.MockPriceClient{}, time.Hour)
		worker.syncPrices()

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("FetchError", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"}).
			AddRow(card.ID, card.Name, card.Set, decimal.Zero, decimal.Zero)

		mock.ExpectQuery("SELECT").WillReturnRows(rows)

		client := &service.MockPriceClient{Err: errors.New("fetch error")}
		worker := NewPriceSyncWorker(db, client, time.Hour)
		worker.syncPrices()

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("UpdateError", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"}).
			AddRow(card.ID, card.Name, card.Set, decimal.Zero, decimal.Zero)

		mock.ExpectQuery("SELECT").WillReturnRows(rows)
		mock.ExpectExec("UPDATE cards").WillReturnError(errors.New("upd error"))
		alertRows := sqlmock.NewRows([]string{"id", "user_id", "target_price"})
		mock.ExpectQuery("SELECT id, user_id, target_price FROM price_alerts").WillReturnRows(alertRows)
		mock.ExpectExec("INSERT INTO price_history").WillReturnResult(sqlmock.NewResult(1, 1))

		client := &service.MockPriceClient{FixedUSD: 1.0, FixedEUR: 1.0}
		worker := NewPriceSyncWorker(db, client, time.Hour)
		worker.syncPrices()

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("HistoryError", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"}).
			AddRow(card.ID, card.Name, card.Set, decimal.Zero, decimal.Zero)

		mock.ExpectQuery("SELECT").WillReturnRows(rows)
		mock.ExpectExec("UPDATE cards").WillReturnResult(sqlmock.NewResult(1, 1))
		// We mock an empty alerts result here before the bulk insert
		alertRows := sqlmock.NewRows([]string{"id", "user_id", "target_price"})
		mock.ExpectQuery("SELECT id, user_id, target_price FROM price_alerts").WillReturnRows(alertRows)
		mock.ExpectExec("INSERT INTO price_history").WillReturnError(errors.New("hist error"))

		client := &service.MockPriceClient{FixedUSD: 1.0, FixedEUR: 1.0}
		worker := NewPriceSyncWorker(db, client, time.Hour)
		worker.syncPrices()

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("SkipZeroPrice", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"}).
			AddRow(card.ID, card.Name, card.Set, decimal.NewFromFloat(150), decimal.NewFromFloat(140))

		mock.ExpectQuery("SELECT").WillReturnRows(rows)
		// A failed scrape returns (0, 0): the worker must NOT issue UPDATE/INSERT,
		// otherwise it would wipe the valid stored price. No Exec expectations set,
		// so any DB write would make ExpectationsWereMet fail.

		client := &service.MockPriceClient{FixedUSD: 0, FixedEUR: 0}
		worker := NewPriceSyncWorker(db, client, time.Hour)
		worker.syncPrices()

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met (zero price should be skipped): %v", err)
		}
	})

	t.Run("PriceAlerts_Triggered", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"}).
			AddRow(card.ID, card.Name, card.Set, decimal.Zero, decimal.Zero)

		mock.ExpectQuery("SELECT id, name").WillReturnRows(rows)
		mock.ExpectExec("UPDATE cards").WillReturnResult(sqlmock.NewResult(1, 1))

		// Alert trigger
		alertRows := sqlmock.NewRows([]string{"id", "user_id", "target_price"}).
			AddRow(1, "user-1", decimal.NewFromFloat(200.0))
		mock.ExpectQuery("SELECT id, user_id, target_price FROM price_alerts").WithArgs(card.ID).
			WillReturnRows(alertRows)

		mock.ExpectExec("INSERT INTO price_history").WillReturnResult(sqlmock.NewResult(1, 1))

		client := &service.MockPriceClient{FixedUSD: 150.0, FixedEUR: 140.0}
		worker := NewPriceSyncWorker(db, client, time.Hour)
		worker.syncPrices()

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})
}

func TestWorkerLifecycle(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()
	client := &service.MockPriceClient{}

	t.Run("ContextCancel", func(_ *testing.T) {
		worker := NewPriceSyncWorker(db, client, 50*time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		worker.Start(ctx)
	})

	t.Run("StopSignal", func(_ *testing.T) {
		worker := NewPriceSyncWorker(db, client, 50*time.Millisecond)
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
		
		mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"}))

		worker := NewPriceSyncWorker(db2, client, 10*time.Millisecond)
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
