package worker

import (
	"context"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"pokget/internal/models"
	"pokget/internal/service"
)

type stubPriceClient struct {
	usd, eur float64
	err      error
	calls    int
}

func (s *stubPriceClient) FetchPrice(models.Card) (float64, float64, error) {
	s.calls++
	return s.usd, s.eur, s.err
}

func (s *stubPriceClient) ApplyMultiplier(price float64, _ string, _ map[string]float64) float64 {
	return price
}

func TestBackfillPricesStoresMissingPokemonPrices(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "name", "set_name", "game", "language", "source_id", "source_card_id", "set_code", "collector_number"}).
		AddRow("card-1", "Bulbasaur", "151", "pokemon", "de", "tcgdex", "sv03.5-001", "sv03.5", "001")
	mock.ExpectQuery("SELECT id, name, set_name").WithArgs(50).WillReturnRows(rows)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE cards[\\s\\S]*COALESCE\\(price_eur, 0\\) <= 0").
		WithArgs(0.12, 0.17, "card-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	// The backfill deliberately writes no price_history row.
	mock.ExpectCommit()

	client := &stubPriceClient{usd: 0.12, eur: 0.17}
	worker := &DataSyncWorker{
		db:            db,
		priceClients:  map[string][]service.PriceClient{"pokemon": {client}},
		backfillBatch: 50,
	}
	worker.backfillPrices(context.Background())

	if client.calls != 1 {
		t.Fatalf("client calls = %d, want 1", client.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// A card the provider does not price must be recorded so the next cycle moves on
// instead of retrying the same card forever.
func TestBackfillPricesRecordsMisses(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "name", "set_name", "game", "language", "source_id", "source_card_id", "set_code", "collector_number"}).
		AddRow("card-2", "Unpriced", "Set", "pokemon", "ja", "tcgdex", "SV3-999", "SV3", "999")
	mock.ExpectQuery("SELECT id, name, set_name").WithArgs(50).WillReturnRows(rows)
	// The miss is recorded as a check, not as a price change.
	mock.ExpectExec("UPDATE cards SET price_checked_at = NOW\\(\\)").WithArgs("card-2").
		WillReturnResult(sqlmock.NewResult(0, 1))

	worker := &DataSyncWorker{
		db:            db,
		priceClients:  map[string][]service.PriceClient{"pokemon": {&stubPriceClient{err: errors.New("no price")}}},
		backfillBatch: 50,
	}
	worker.backfillPrices(context.Background())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestBackfillPricesIsDisabledWithoutBatch(t *testing.T) {
	worker := &DataSyncWorker{}
	worker.backfillPrices(context.Background())
}
