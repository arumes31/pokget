package worker

import (
	"log/slog"
	"io"
	"pokget/internal/service"
	"testing"
	"time"
    "fmt"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
)

func BenchmarkSyncPrices(b *testing.B) {
	// Mute logs during benchmark
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	for i := 0; i < b.N; i++ {
		db, mock, _ := sqlmock.New()

        // Mock 10 cards
		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "price_usd", "price_eur"})
        for j := 0; j < 10; j++ {
            rows.AddRow(fmt.Sprintf("%d", j), "Card", "Set", decimal.NewFromFloat(0), decimal.NewFromFloat(0))
        }

		mock.ExpectQuery("SELECT id, name").WillReturnRows(rows)
        for j := 0; j < 10; j++ {
		    mock.ExpectExec("UPDATE cards").WillReturnResult(sqlmock.NewResult(1, 1))
		    mock.ExpectExec("INSERT INTO price_history").WillReturnResult(sqlmock.NewResult(1, 1))
        }

        alertRows := sqlmock.NewRows([]string{"id", "user_id", "target_price", "card_id"}).
            AddRow(1, "user-1", decimal.NewFromFloat(200.0), "0")
        mock.ExpectQuery("SELECT id, user_id, target_price, card_id FROM price_alerts WHERE is_active = TRUE").WillReturnRows(alertRows)

		client := &service.MockPriceClient{FixedUSD: 150.0, FixedEUR: 140.0}
		worker := NewPriceSyncWorker(db, client, time.Hour)
		worker.syncPrices()
        db.Close()
	}
}
