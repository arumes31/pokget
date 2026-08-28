package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPostgresAdvisoryLease(t *testing.T) {
	t.Run("AcquireAndRelease", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		mock.ExpectQuery("pg_try_advisory_lock").
			WithArgs("cycle-key").
			WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
		mock.ExpectQuery("pg_advisory_unlock").
			WithArgs("cycle-key").
			WillReturnRows(sqlmock.NewRows([]string{"pg_advisory_unlock"}).AddRow(true))

		lease := NewPostgresAdvisoryLease(db)
		release, acquired, err := lease.Acquire(context.Background(), "cycle-key")
		if err != nil {
			t.Fatal(err)
		}
		if !acquired {
			t.Fatal("Acquire() acquired = false, want true")
		}
		if err := release(); err != nil {
			t.Fatalf("release() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("LockHeldByAnotherReplica", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		mock.ExpectQuery("pg_try_advisory_lock").
			WithArgs("cycle-key").
			WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(false))

		lease := NewPostgresAdvisoryLease(db)
		release, acquired, err := lease.Acquire(context.Background(), "cycle-key")
		if err != nil {
			t.Fatal(err)
		}
		if acquired {
			t.Fatal("Acquire() acquired = true, want false when the lock is held")
		}
		if err := release(); err != nil {
			t.Fatalf("no-op release() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("LockQueryError", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		mock.ExpectQuery("pg_try_advisory_lock").
			WithArgs("cycle-key").
			WillReturnError(errors.New("db error"))

		lease := NewPostgresAdvisoryLease(db)
		release, acquired, err := lease.Acquire(context.Background(), "cycle-key")
		if err == nil {
			t.Fatal("Acquire() error = nil, want lock query error")
		}
		if acquired || release != nil {
			t.Fatalf("Acquire() acquired = %v, want false with nil release on error", acquired)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ConnectionError", func(t *testing.T) {
		db, _, _ := sqlmock.New()
		defer db.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		lease := NewPostgresAdvisoryLease(db)
		_, acquired, err := lease.Acquire(ctx, "cycle-key")
		if err == nil {
			t.Fatal("Acquire() error = nil, want connection error for a cancelled context")
		}
		if acquired {
			t.Fatal("Acquire() acquired = true, want false on connection error")
		}
	})

	t.Run("ReleaseFailsWhenLockNotHeld", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		mock.ExpectQuery("pg_try_advisory_lock").
			WithArgs("cycle-key").
			WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
		mock.ExpectQuery("pg_advisory_unlock").
			WithArgs("cycle-key").
			WillReturnRows(sqlmock.NewRows([]string{"pg_advisory_unlock"}).AddRow(false))

		lease := NewPostgresAdvisoryLease(db)
		release, acquired, err := lease.Acquire(context.Background(), "cycle-key")
		if err != nil || !acquired {
			t.Fatalf("Acquire() = %v/%v", acquired, err)
		}
		if err := release(); err == nil {
			t.Fatal("release() error = nil, want not-held error")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ReleasePropagatesUnlockError", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		mock.ExpectQuery("pg_try_advisory_lock").
			WithArgs("cycle-key").
			WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
		mock.ExpectQuery("pg_advisory_unlock").
			WithArgs("cycle-key").
			WillReturnError(errors.New("unlock failed"))

		lease := NewPostgresAdvisoryLease(db)
		release, acquired, err := lease.Acquire(context.Background(), "cycle-key")
		if err != nil || !acquired {
			t.Fatalf("Acquire() = %v/%v", acquired, err)
		}
		if err := release(); err == nil {
			t.Fatal("release() error = nil, want unlock error")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("NilLeaseBehavesAsNoop", func(t *testing.T) {
		var lease *PostgresAdvisoryLease
		release, acquired, err := lease.Acquire(context.Background(), "cycle-key")
		if err != nil || !acquired {
			t.Fatalf("Acquire() = %v/%v, want true/nil for nil lease", acquired, err)
		}
		if err := release(); err != nil {
			t.Fatalf("release() error = %v", err)
		}
	})

	t.Run("NilDatabaseBehavesAsNoop", func(t *testing.T) {
		lease := NewPostgresAdvisoryLease(nil)
		release, acquired, err := lease.Acquire(context.Background(), "cycle-key")
		if err != nil || !acquired {
			t.Fatalf("Acquire() = %v/%v, want true/nil for nil database", acquired, err)
		}
		if err := release(); err != nil {
			t.Fatalf("release() error = %v", err)
		}
	})
}

func TestWaitForRetry(t *testing.T) {
	t.Run("NonPositiveDelayReturnsContextState", func(t *testing.T) {
		if err := waitForRetry(context.Background(), 0); err != nil {
			t.Fatalf("waitForRetry(0) error = %v, want nil", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := waitForRetry(ctx, 0); !errors.Is(err, context.Canceled) {
			t.Fatalf("waitForRetry(0) error = %v, want context.Canceled", err)
		}
	})

	t.Run("TimerFires", func(t *testing.T) {
		if err := waitForRetry(context.Background(), time.Millisecond); err != nil {
			t.Fatalf("waitForRetry() error = %v, want nil", err)
		}
	})

	t.Run("CancelledWhileWaiting", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		start := time.Now()
		if err := waitForRetry(ctx, time.Hour); !errors.Is(err, context.Canceled) {
			t.Fatalf("waitForRetry() error = %v, want context.Canceled", err)
		}
		if time.Since(start) > time.Second {
			t.Fatal("waitForRetry did not return promptly after cancellation")
		}
	})
}

func TestNewRequestLimiter(t *testing.T) {
	t.Run("UnlimitedWithoutRate", func(t *testing.T) {
		limiter := newRequestLimiter(0, 0)
		firstAllowed := limiter.Allow()
		secondAllowed := limiter.Allow()
		if !firstAllowed || !secondAllowed {
			t.Fatal("limiter without a configured rate should allow every request")
		}
	})

	t.Run("BurstCoercedToOne", func(t *testing.T) {
		limiter := newRequestLimiter(1, 0)
		if !limiter.Allow() {
			t.Fatal("first request within the burst should be allowed")
		}
		if limiter.Allow() {
			t.Fatal("second immediate request should exceed the burst")
		}
	})
}

func TestDataSyncConfigValidation(t *testing.T) {
	valid := DataSyncConfig{
		Interval:        time.Hour,
		RetryAttempts:   1,
		CircuitFailures: 1,
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*DataSyncConfig)
	}{
		{"Interval", func(c *DataSyncConfig) { c.Interval = 0 }},
		{"RetryAttempts", func(c *DataSyncConfig) { c.RetryAttempts = 0 }},
		{"RetryBaseDelay", func(c *DataSyncConfig) { c.RetryBaseDelay = -time.Second }},
		{"CircuitFailures", func(c *DataSyncConfig) { c.CircuitFailures = 0 }},
		{"CircuitCooldown", func(c *DataSyncConfig) { c.CircuitCooldown = -time.Second }},
		{"RequestsPerSecond", func(c *DataSyncConfig) { c.RequestsPerSecond = -1 }},
		{"RequestBurst", func(c *DataSyncConfig) { c.RequestBurst = -1 }},
		{"MaxPriceRatio", func(c *DataSyncConfig) { c.MaxPriceRatio = 1 }},
		{"HistoryRetention", func(c *DataSyncConfig) { c.HistoryRetention = -time.Hour }},
		{"MetadataTargetGame", func(c *DataSyncConfig) {
			c.MetadataTargets = []MetadataTarget{{Game: " ", Language: "en"}}
		}},
		{"MetadataTargetLanguage", func(c *DataSyncConfig) {
			c.MetadataTargets = []MetadataTarget{{Game: "pokemon", Language: " "}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			test.mutate(&config)
			if err := config.validate(); err == nil {
				t.Fatalf("config with invalid %s accepted", test.name)
			}
		})
	}
}

func TestNewFileFailureSinkRejectsEmptyPath(t *testing.T) {
	for _, path := range []string{"", "   "} {
		if _, err := NewFileFailureSink(path); err == nil {
			t.Fatalf("NewFileFailureSink(%q) error = nil, want empty path error", path)
		}
	}
}

func TestFileFailureSinkRespectsCancelledContext(t *testing.T) {
	sink, err := NewFileFailureSink(t.TempDir() + "/failures.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = sink.StoreFailure(ctx, FailureRecord{Operation: "price", Error: "boom"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("StoreFailure() error = %v, want context.Canceled", err)
	}
}

func TestAcquireCycleWithoutLease(t *testing.T) {
	worker := &DataSyncWorker{}
	release, acquired, err := worker.acquireCycle(context.Background(), "prices")
	if err != nil || !acquired {
		t.Fatalf("acquireCycle() = %v/%v, want true/nil without a lease", acquired, err)
	}
	worker.releaseCycle(release, "prices")
	worker.releaseCycle(nil, "prices")
}

func TestStoreFailureSinkErrorIsBestEffort(t *testing.T) {
	// storeFailure only logs sink errors; nothing is returned or requeued.
	// The observable contract is that the sink is still invoked exactly once.
	failingSink := &failureSinkStub{err: errors.New("disk full")}
	worker := &DataSyncWorker{failureSink: failingSink}
	worker.storeFailure(context.Background(), FailureRecord{Operation: "price"})
	if len(failingSink.records) != 1 {
		t.Fatalf("sink calls = %d, want 1 even when the sink errors", len(failingSink.records))
	}

	worker = &DataSyncWorker{failureSink: &failureSinkStub{}}
	worker.storeFailure(context.Background(), FailureRecord{Operation: "price", Error: "boom"})
	if sink, ok := worker.failureSink.(*failureSinkStub); ok {
		if len(sink.records) != 1 || !strings.Contains(sink.records[0].Error, "boom") {
			t.Fatalf("recorded failures = %+v", sink.records)
		}
	}
}
