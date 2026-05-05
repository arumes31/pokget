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

func TestPriceSyncWorker(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to open mock db: %v", err)
	}
	defer db.Close()

	card := models.Card{ID: "1", Name: "Charizard", Set: "Base"}

	t.Run("SyncSuccess", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"}).
			AddRow(card.ID, card.Name, card.Set, decimal.NewFromFloat(0), decimal.NewFromFloat(0))

		mock.ExpectQuery("SELECT id, name, set_name, price_usd, price_eur FROM cards").
			WillReturnRows(rows)

		mock.ExpectExec("UPDATE cards SET price_usd = \\$1, price_eur = \\$2, last_updated = NOW\\(\\) WHERE id = \\$3").
			WithArgs(decimal.NewFromFloat(150.0), decimal.NewFromFloat(140.0), card.ID).
			WillReturnResult(sqlmock.NewResult(1, 1))

		client := &service.MockPriceClient{FixedUSD: 150.0, FixedEUR: 140.0}
		worker := NewPriceSyncWorker(db, client, 100*time.Millisecond)
		worker.syncPrices()

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("QueryError", func(t *testing.T) {
		mock.ExpectQuery("SELECT id, name, set_name, price_usd, price_eur FROM cards").
			WillReturnError(errors.New("db error"))

		client := &service.MockPriceClient{}
		worker := NewPriceSyncWorker(db, client, 100*time.Millisecond)
		worker.syncPrices()

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("FetchError", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"}).
			AddRow(card.ID, card.Name, card.Set, decimal.NewFromFloat(0), decimal.NewFromFloat(0))

		mock.ExpectQuery("SELECT id, name, set_name, price_usd, price_eur FROM cards").
			WillReturnRows(rows)

		client := &service.MockPriceClient{Err: errors.New("fetch error")}
		worker := NewPriceSyncWorker(db, client, 100*time.Millisecond)
		worker.syncPrices()

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("ScanError", func(t *testing.T) {
		// Provide fewer columns than expected to trigger a scan error
		rows := sqlmock.NewRows([]string{"id"}).AddRow(card.ID)

		mock.ExpectQuery("SELECT id, name, set_name, price_usd, price_eur FROM cards").
			WillReturnRows(rows)

		client := &service.MockPriceClient{}
		worker := NewPriceSyncWorker(db, client, 100*time.Millisecond)
		worker.syncPrices()

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("UpdateError", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"}).
			AddRow(card.ID, card.Name, card.Set, decimal.NewFromFloat(0), decimal.NewFromFloat(0))

		mock.ExpectQuery("SELECT id, name, set_name, price_usd, price_eur FROM cards").
			WillReturnRows(rows)

		mock.ExpectExec("UPDATE cards SET price_usd = \\$1, price_eur = \\$2, last_updated = NOW\\(\\) WHERE id = \\$3").
			WithArgs(decimal.NewFromFloat(150.0), decimal.NewFromFloat(140.0), card.ID).
			WillReturnError(errors.New("update error"))

		client := &service.MockPriceClient{FixedUSD: 150.0, FixedEUR: 140.0}
		worker := NewPriceSyncWorker(db, client, 100*time.Millisecond)
		worker.syncPrices()

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})
}


func TestWorkerLifecycle(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to open mock db: %v", err)
	}
	defer db.Close()

	client := &service.MockPriceClient{}
	worker := NewPriceSyncWorker(db, client, 50*time.Millisecond)

	t.Run("ContextCancel", func(_ *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		worker.Start(ctx)
		// Should return when ctx times out
	})

	t.Run("StopSignal", func(_ *testing.T) {
		ctx := context.Background()
		go func() {
			time.Sleep(20 * time.Millisecond)
			worker.Stop()
		}()

		worker.Start(ctx)
		// Should return when worker.Stop() is called
	})

	t.Run("TickerExecution", func(t *testing.T) {
		// Mock at least one sync cycle
		mock.ExpectQuery("SELECT id, name, set_name, price_usd, price_eur FROM cards").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"}))

		workerShort := NewPriceSyncWorker(db, client, 10*time.Millisecond)
		ctx, cancel := context.WithCancel(context.Background())
		
		go func() {
			time.Sleep(25 * time.Millisecond)
			cancel()
		}()

		workerShort.Start(ctx)

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})
}
