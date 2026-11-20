package worker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

	rows := sqlmock.NewRows([]string{"id", "name", "set_name", "game", "language", "source_id", "source_card_id", "set_code", "collector_number", "has_usd"}).
		AddRow("card-1", "Bulbasaur", "151", "pokemon", "de", "tcgdex", "sv03.5-001", "sv03.5", "001", false)
	// A converted EUR is no Cardmarket quote, so the card stays in the queue and the
	// native quote may replace it.
	mock.ExpectQuery(`SELECT id, name, set_name[\s\S]*OR price_eur_estimated\)`).WithArgs(50, "", false).WillReturnRows(rows)
	mock.ExpectBegin()
	// Either missing currency is a reason to write, so a EUR-only card still gets its
	// USD quote without losing the EUR one. last_updated only moves with an amount
	// (a native quote equal to the estimate confirms it but changes nothing).
	mock.ExpectExec(`UPDATE cards[\s\S]*last_updated = CASE WHEN \(\$1::numeric > 0 AND price_usd IS DISTINCT FROM \$1::numeric\)\s+`+
		`OR \(\$2::numeric > 0 AND price_eur IS DISTINCT FROM \$2::numeric\)\s+THEN NOW\(\) ELSE last_updated END,\s+price_checked_at = NOW\(\)`+
		`[\s\S]*COALESCE\(price_eur, 0\) <= 0 OR price_eur_estimated\) AND \$2`).
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

// A card the provider answers for without a price must be recorded so the next cycle
// moves on instead of retrying the same card forever.
func TestBackfillPricesRecordsMisses(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "name", "set_name", "game", "language", "source_id", "source_card_id", "set_code", "collector_number", "has_usd"}).
		AddRow("card-2", "Unpriced", "Set", "pokemon", "ja", "tcgdex", "SV3-999", "SV3", "999", false)
	mock.ExpectQuery("SELECT id, name, set_name").WithArgs(50, "", false).WillReturnRows(rows)
	// The empty answer is recorded as a check, not as a price change.
	mock.ExpectExec("UPDATE cards SET price_checked_at = NOW\\(\\)").WithArgs("card-2").
		WillReturnResult(sqlmock.NewResult(0, 1))

	worker := &DataSyncWorker{
		db:            db,
		priceClients:  map[string][]service.PriceClient{"pokemon": {&stubPriceClient{}}},
		backfillBatch: 50,
	}
	worker.backfillPrices(context.Background())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// A provider or transport error is not a verification: the card keeps its stamp so
// the next cycle retries it instead of moving it to the back of the queue. A real
// "no price" answer of the TCGdex client (404, empty pricing) is one and rotates it.
func TestBackfillPricesKeepsStampOnProviderError(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		wantStamp bool
	}{
		{name: "server error", status: http.StatusInternalServerError, body: `broken`},
		{name: "unknown card", status: http.StatusNotFound, body: `{"error":"not found"}`, wantStamp: true},
		{name: "no pricing", status: http.StatusOK, body: `{"pricing":{}}`, wantStamp: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, _ = writer.Write([]byte(tc.body))
			}))
			defer server.Close()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()

			rows := sqlmock.NewRows([]string{"id", "name", "set_name", "game", "language", "source_id", "source_card_id", "set_code", "collector_number", "has_usd"}).
				AddRow("card-3", "Retry", "Set", "pokemon", "en", "tcgdex", "SV3-1", "SV3", "1", false)
			mock.ExpectQuery("SELECT id, name, set_name").WithArgs(50, "", false).WillReturnRows(rows)
			if tc.wantStamp {
				mock.ExpectExec("UPDATE cards SET price_checked_at = NOW\\(\\)").WithArgs("card-3").
					WillReturnResult(sqlmock.NewResult(0, 1))
			}

			client := &service.TCGdexPriceClient{BaseURL: server.URL, HTTP: server.Client()}
			worker := &DataSyncWorker{
				db:            db,
				priceClients:  map[string][]service.PriceClient{"pokemon": {client}},
				backfillBatch: 50,
			}
			worker.backfillPrices(context.Background())

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
			// An unexpected stamp only logs, so check the classification directly.
			_, _, fetchErr := fetchCardPriceFrom(context.Background(), []service.PriceClient{client},
				models.Card{ID: "card-3", Name: "Retry", SourceID: "tcgdex", SourceCardID: "SV3-1"})
			if got := errors.Is(fetchErr, errNoBackfillPrice); got != tc.wantStamp {
				t.Fatalf("no-price classification = %v (err %v), want %v", got, fetchErr, tc.wantStamp)
			}
		})
	}
}

// A card that already shows a USD price and only lacks a Cardmarket EUR must not
// get a "verified" stamp from a miss; that queue rotates through the id cursor.
func TestBackfillPricesRotatesUSDPricedCardsByCursor(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "name", "set_name", "game", "language", "source_id", "source_card_id", "set_code", "collector_number", "has_usd"}).
		AddRow("card-a", "A", "Set", "pokemon", "ja", "tcgdex", "SV3-1", "SV3", "1", true).
		AddRow("card-b", "B", "Set", "pokemon", "ja", "tcgdex", "SV3-2", "SV3", "2", true)
	mock.ExpectQuery(`SELECT id, name, set_name[\s\S]*id > \$2`).WithArgs(2, "card-0", false).WillReturnRows(rows)
	for _, id := range []string{"card-a", "card-b"} {
		mock.ExpectExec(`UPDATE cards SET price_checked_at = NOW\(\) WHERE id = \$1 AND COALESCE\(price_usd, 0\) <= 0`).
			WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 0))
	}

	worker := &DataSyncWorker{
		db:             db,
		priceClients:   map[string][]service.PriceClient{"pokemon": {&stubPriceClient{err: service.ErrNoPrice}}},
		backfillBatch:  2,
		backfillCursor: "card-0",
	}
	worker.backfillPrices(context.Background())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
	if worker.backfillCursor != "card-b" {
		t.Fatalf("cursor = %q, want the last USD-priced card", worker.backfillCursor)
	}
}

// perCardClient answers per card id, so one pass can mix outcomes.
type perCardClient map[string]error

func (c perCardClient) FetchPrice(card models.Card) (float64, float64, error) {
	return 0, 0, c[card.ID]
}

func (perCardClient) ApplyMultiplier(price float64, _ string, _ map[string]float64) float64 {
	return price
}

// failAfter lets n waits through and then fails like a cancelled context.
type failAfter struct{ n int }

func (f *failAfter) Wait(context.Context) error {
	if f.n <= 0 {
		return context.Canceled
	}
	f.n--
	return nil
}

// The cursor must not pass a USD-priced card that hit a provider error or that the
// pass never reached; the next pass retries from there.
func TestBackfillCursorStopsAtUnansweredCards(t *testing.T) {
	cases := []struct {
		name       string
		client     perCardClient
		limiter    *failAfter
		wantCursor string
	}{
		{name: "provider error", client: perCardClient{"card-a": errors.New("HTTP 500"), "card-b": service.ErrNoPrice}, wantCursor: "card-0"},
		{name: "not reached", client: perCardClient{"card-a": service.ErrNoPrice, "card-b": service.ErrNoPrice}, limiter: &failAfter{n: 1}, wantCursor: "card-a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()

			rows := sqlmock.NewRows([]string{"id", "name", "set_name", "game", "language", "source_id", "source_card_id", "set_code", "collector_number", "has_usd"}).
				AddRow("card-a", "A", "Set", "pokemon", "ja", "tcgdex", "SV3-1", "SV3", "1", true).
				AddRow("card-b", "B", "Set", "pokemon", "ja", "tcgdex", "SV3-2", "SV3", "2", true)
			// The miss stamps are guarded to no-ops for USD-priced cards and only log on
			// failure, so the test asserts the cursor alone.
			mock.ExpectQuery("SELECT id, name, set_name").WithArgs(2, "card-0", false).WillReturnRows(rows)

			worker := &DataSyncWorker{
				db:             db,
				priceClients:   map[string][]service.PriceClient{"pokemon": {tc.client}},
				backfillBatch:  2,
				backfillCursor: "card-0",
			}
			if tc.limiter != nil {
				worker.backfillLimiter = tc.limiter
			}
			worker.backfillPrices(context.Background())

			if worker.backfillCursor != tc.wantCursor {
				t.Fatalf("cursor = %q, want %q", worker.backfillCursor, tc.wantCursor)
			}
		})
	}
}

// With a batch of one, the queue that takes the first turn alternates per pass so
// neither the unpriced nor the USD-priced queue starves.
func TestBackfillAlternatesTheFirstQueue(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	empty := []string{"id", "name", "set_name", "game", "language", "source_id", "source_card_id", "set_code", "collector_number", "has_usd"}
	mock.ExpectQuery(`ORDER BY turn, has_usd <> \$3`).WithArgs(1, "", false).WillReturnRows(sqlmock.NewRows(empty))
	mock.ExpectQuery(`ORDER BY turn, has_usd <> \$3`).WithArgs(1, "", true).WillReturnRows(sqlmock.NewRows(empty))

	worker := &DataSyncWorker{
		db:            db,
		priceClients:  map[string][]service.PriceClient{"pokemon": {&stubPriceClient{}}},
		backfillBatch: 1,
	}
	worker.backfillPrices(context.Background())
	worker.backfillPrices(context.Background())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// A provider that only repeats the currency the card already has made no progress;
// the card must rotate to the back of the queue instead of being retried every cycle.
func TestBackfillPricesRotatesWhenNothingIsFilled(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "name", "set_name", "game", "language", "source_id", "source_card_id", "set_code", "collector_number", "has_usd"}).
		AddRow("card-4", "EurOnly", "Set", "pokemon", "en", "tcgdex", "SV3-2", "SV3", "2", false)
	mock.ExpectQuery("SELECT id, name, set_name").WithArgs(50, "", false).WillReturnRows(rows)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE cards[\s\S]*COALESCE\(price_eur, 0\) <= 0 OR price_eur_estimated\) AND \$2`).
		WithArgs(0.0, 0.9, "card-4").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	mock.ExpectExec("UPDATE cards SET price_checked_at = NOW\\(\\)").WithArgs("card-4").
		WillReturnResult(sqlmock.NewResult(0, 1))

	worker := &DataSyncWorker{
		db:            db,
		priceClients:  map[string][]service.PriceClient{"pokemon": {&stubPriceClient{usd: 0, eur: 0.9}}},
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
