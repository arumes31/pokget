package worker

import (
	"fmt"
	"pokget/internal/service"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
)

func BenchmarkSyncPrices(b *testing.B) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	client := &service.MockPriceClient{FixedUSD: 150.0, FixedEUR: 140.0}
	worker := NewPriceSyncWorker(db, client, time.Hour)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"})
		for j := 0; j < 100; j++ {
			rows.AddRow(fmt.Sprintf("%d", j), "Card", "Set", decimal.Zero, decimal.Zero)
		}

		mock.ExpectQuery("SELECT id, name, set_name, price_usd, price_eur FROM cards").WillReturnRows(rows)

		for j := 0; j < 100; j++ {
			mock.ExpectExec("UPDATE cards").WillReturnResult(sqlmock.NewResult(1, 1))
			// INSERT history is now batched
			alertRows := sqlmock.NewRows([]string{"id", "user_id", "target_price"})
			mock.ExpectQuery("SELECT id, user_id, target_price FROM price_alerts").WillReturnRows(alertRows)
		}

		mock.ExpectExec("INSERT INTO price_history").WillReturnResult(sqlmock.NewResult(100, 100))

		b.StartTimer()
		worker.syncPrices()
	}
}
