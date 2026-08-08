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

type closeablePriceClient struct {
	closeErr error
	closes   int
}

func (c *closeablePriceClient) FetchPrice(models.Card) (float64, float64, error) {
	return 0, 0, nil
}

func (c *closeablePriceClient) ApplyMultiplier(price float64, _ string, _ map[string]float64) float64 {
	return price
}

func (c *closeablePriceClient) Close() error {
	c.closes++
	return c.closeErr
}

type flakyPriceClient struct {
	errs  []error
	usd   float64
	eur   float64
	calls int
}

func (f *flakyPriceClient) FetchPrice(models.Card) (float64, float64, error) {
	f.calls++
	if f.calls <= len(f.errs) {
		return 0, 0, f.errs[f.calls-1]
	}
	return f.usd, f.eur, nil
}

func (f *flakyPriceClient) ApplyMultiplier(price float64, _ string, _ map[string]float64) float64 {
	return price
}

type contextPriceClientStub struct {
	usedContext bool
}

func (c *contextPriceClientStub) FetchPrice(models.Card) (float64, float64, error) {
	return 1, 1, nil
}

func (c *contextPriceClientStub) ApplyMultiplier(price float64, _ string, _ map[string]float64) float64 {
	return price
}

func (c *contextPriceClientStub) FetchPriceContext(ctx context.Context, _ models.Card) (float64, float64, error) {
	c.usedContext = ctx != nil
	return 2, 3, nil
}

type errorLimiter struct{ err error }

func (l errorLimiter) Wait(context.Context) error { return l.err }

func TestDataSyncWorkerWait(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()

	t.Run("ReturnsNilAfterStop", func(t *testing.T) {
		worker := NewDataSyncWorker(db, &service.MockPriceClient{}, nil, nil, time.Hour)
		go worker.Start(context.Background())
		worker.Stop()
		if err := worker.Wait(context.Background()); err != nil {
			t.Fatalf("Wait() error = %v, want nil", err)
		}
	})

	t.Run("ReturnsContextErrorWhenNotDone", func(t *testing.T) {
		worker := NewDataSyncWorker(db, &service.MockPriceClient{}, nil, nil, time.Hour)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := worker.Wait(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Wait() error = %v, want context.Canceled", err)
		}
	})
}

func TestDataSyncWorkerClose(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()

	t.Run("ClosesEachClientOnceAndJoinsErrors", func(t *testing.T) {
		primary := &closeablePriceClient{}
		duplicate := &closeablePriceClient{}
		failing := &closeablePriceClient{closeErr: errors.New("close boom")}

		worker, err := NewConfiguredDataSyncWorker(db, primary, nil, nil, DataSyncConfig{
			Interval:        time.Hour,
			RetryAttempts:   1,
			CircuitFailures: 1,
			PriceClients: map[string][]service.PriceClient{
				"pokemon":  {duplicate, duplicate},
				"onepiece": {failing},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := worker.Close(); err == nil {
			t.Fatal("Close() error = nil, want joined client error")
		}
		if primary.closes != 1 || duplicate.closes != 1 || failing.closes != 1 {
			t.Fatalf("close counts = %d/%d/%d, want 1/1/1", primary.closes, duplicate.closes, failing.closes)
		}
	})

	t.Run("NoCloseableClients", func(t *testing.T) {
		worker := NewDataSyncWorker(db, &service.MockPriceClient{}, nil, nil, time.Hour)
		if err := worker.Close(); err != nil {
			t.Fatalf("Close() error = %v, want nil", err)
		}
	})

	t.Run("NilClients", func(t *testing.T) {
		worker := &DataSyncWorker{}
		if err := worker.Close(); err != nil {
			t.Fatalf("Close() error = %v, want nil", err)
		}
	})
}

func TestNewDataSyncWorkerPanicsOnInvalidConfig(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()

	defer func() {
		if recover() == nil {
			t.Fatal("NewDataSyncWorker with a non-positive interval should panic")
		}
	}()
	NewDataSyncWorker(db, &service.MockPriceClient{}, nil, nil, 0)
}

func TestFetchPricePrefersContextClient(t *testing.T) {
	client := &contextPriceClientStub{}
	usd, eur, err := fetchPrice(context.Background(), client, models.Card{ID: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if !client.usedContext {
		t.Fatal("fetchPrice should prefer FetchPriceContext when implemented")
	}
	if usd != 2 || eur != 3 {
		t.Fatalf("fetchPrice() = %v/%v, want 2/3", usd, eur)
	}
}

func TestFetchSourceWithRetry(t *testing.T) {
	card := models.Card{ID: "1", Game: "pokemon"}

	newWorker := func(client service.PriceClient, limiter interface{ Wait(context.Context) error }) *DataSyncWorker {
		return &DataSyncWorker{
			priceClient:     client,
			limiter:         limiter,
			retryAttempts:   2,
			retryBaseDelay:  time.Millisecond,
			circuitFailures: 2,
			circuitCooldown: time.Minute,
			circuits:        make(map[string]providerCircuit),
		}
	}

	t.Run("LimiterErrorAbortsFetch", func(t *testing.T) {
		limiterErr := errors.New("limiter shutdown")
		client := &flakyPriceClient{usd: 5, eur: 4}
		worker := newWorker(client, errorLimiter{err: limiterErr})

		_, _, err := worker.fetchSourceWithRetry(context.Background(), "key", client, card)
		if !errors.Is(err, limiterErr) {
			t.Fatalf("error = %v, want limiter error", err)
		}
		if client.calls != 0 {
			t.Fatalf("FetchPrice called %d times despite limiter error", client.calls)
		}
	})

	t.Run("RetriesUntilSuccess", func(t *testing.T) {
		client := &flakyPriceClient{
			errs: []error{errors.New("transient")},
			usd:  5, eur: 4,
		}
		worker := newWorker(client, newRequestLimiter(0, 0))

		usd, eur, err := worker.fetchSourceWithRetry(context.Background(), "key", client, card)
		if err != nil {
			t.Fatal(err)
		}
		if client.calls != 2 {
			t.Fatalf("FetchPrice calls = %d, want 2", client.calls)
		}
		if usd != 5 || eur != 4 {
			t.Fatalf("prices = %v/%v, want 5/4", usd, eur)
		}
	})

	t.Run("CancellationIsNotRetried", func(t *testing.T) {
		client := &flakyPriceClient{errs: []error{context.Canceled}}
		worker := newWorker(client, newRequestLimiter(0, 0))

		_, _, err := worker.fetchSourceWithRetry(context.Background(), "key", client, card)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if client.calls != 1 {
			t.Fatalf("FetchPrice calls = %d, want 1 for a cancelled fetch", client.calls)
		}
	})

	t.Run("ExhaustedRetriesFeedCircuit", func(t *testing.T) {
		client := &flakyPriceClient{errs: []error{errors.New("down"), errors.New("down")}}
		worker := newWorker(client, newRequestLimiter(0, 0))

		_, _, err := worker.fetchSourceWithRetry(context.Background(), "key", client, card)
		if err == nil {
			t.Fatal("expected error after exhausting retries")
		}
		if client.calls != 2 {
			t.Fatalf("FetchPrice calls = %d, want 2", client.calls)
		}
		worker.circuitMu.Lock()
		failures := worker.circuits["key"].failures
		worker.circuitMu.Unlock()
		if failures != 1 {
			t.Fatalf("circuit failures = %d, want 1", failures)
		}
	})
}

func TestFetchCardPriceWithoutSources(t *testing.T) {
	worker := &DataSyncWorker{priceClients: map[string][]service.PriceClient{}}
	_, _, err := worker.fetchCardPrice(context.Background(), models.Card{ID: "1", Game: "pokemon"})
	if err == nil {
		t.Fatal("expected an error when no price source is configured")
	}
}

func TestCheckPriceAlerts(t *testing.T) {
	card := models.Card{ID: "card-1", Name: "Charizard"}

	t.Run("QueryError", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		mock.ExpectQuery("SELECT id, user_id, target_price FROM price_alerts").
			WithArgs(card.ID).
			WillReturnError(errors.New("db error"))

		worker := &DataSyncWorker{db: db}
		worker.checkPriceAlerts(context.Background(), card, 150)

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("TargetNotHitLeavesAlertActive", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		alertRows := sqlmock.NewRows([]string{"id", "user_id", "target_price"}).
			AddRow(1, "user-1", decimal.NewFromFloat(100))
		mock.ExpectQuery("SELECT id, user_id, target_price FROM price_alerts").
			WithArgs(card.ID).
			WillReturnRows(alertRows)
		// No UPDATE expectation: a price above target must not claim the alert.

		worker := &DataSyncWorker{db: db}
		worker.checkPriceAlerts(context.Background(), card, 150)

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("UnclaimedAlertIsIgnored", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		alertRows := sqlmock.NewRows([]string{"id", "user_id", "target_price"}).
			AddRow(1, "user-1", decimal.NewFromFloat(200))
		mock.ExpectQuery("SELECT id, user_id, target_price FROM price_alerts").
			WithArgs(card.ID).
			WillReturnRows(alertRows)
		mock.ExpectExec("UPDATE price_alerts").
			WithArgs(1).
			WillReturnResult(sqlmock.NewResult(0, 0))

		worker := &DataSyncWorker{db: db}
		worker.checkPriceAlerts(context.Background(), card, 150)

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ScanErrorSkipsRow", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		alertRows := sqlmock.NewRows([]string{"id", "user_id", "target_price"}).
			AddRow("not-an-int", "user-1", decimal.NewFromFloat(200))
		mock.ExpectQuery("SELECT id, user_id, target_price FROM price_alerts").
			WithArgs(card.ID).
			WillReturnRows(alertRows)

		worker := &DataSyncWorker{db: db}
		worker.checkPriceAlerts(context.Background(), card, 150)

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}
