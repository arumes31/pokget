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

	// Mock data
	card := models.Card{ID: "1", Name: "Charizard", Set: "Base"}
	
	// Expectations for sync cycle
	rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"}).
		AddRow(card.ID, card.Name, card.Set, decimal.NewFromFloat(0), decimal.NewFromFloat(0))
	
	mock.ExpectQuery("SELECT id, name, set_name, price_usd, price_eur FROM cards").
		WillReturnRows(rows)
	
	mock.ExpectExec("UPDATE cards SET price_usd = \\$1, price_eur = \\$2, last_updated = NOW\\(\\) WHERE id = \\$3").
		WithArgs(decimal.NewFromFloat(150.0), decimal.NewFromFloat(140.0), card.ID).
		WillReturnResult(sqlmock.NewResult(1, 1))

	client := &service.MockPriceClient{FixedUSD: 150.0, FixedEUR: 140.0}
	worker := NewPriceSyncWorker(db, client, 100*time.Millisecond)

	// Run sync once manually for testing
	worker.syncPrices()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Expectations not met: %v", err)
	}
}
