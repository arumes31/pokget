package worker

import (
	"gettos/internal/models"
	"gettos/internal/service"
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
