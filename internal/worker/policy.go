package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"pokget/internal/models"
	"pokget/internal/service"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

var ErrWorkerCircuitOpen = errors.New("worker: provider circuit open")

// MetadataTarget identifies one game/language catalog feed.
type MetadataTarget struct {
	Game     string
	Language string
}

// FailureRecord is the durable, replayable description of a worker failure.
type FailureRecord struct {
	OccurredAt time.Time `json:"occurred_at"`
	Operation  string    `json:"operation"`
	CardID     string    `json:"card_id,omitempty"`
	Game       string    `json:"game,omitempty"`
	Attempts   int       `json:"attempts"`
	Error      string    `json:"error"`
}

// FailureSink stores failures that exhausted the configured retry policy.
type FailureSink interface {
	StoreFailure(context.Context, FailureRecord) error
}

// FileFailureSink appends bounded worker metadata to a JSONL file. It never
// stores OCR text, image data, credentials, or provider response bodies.
type FileFailureSink struct {
	path string
	mu   sync.Mutex
}

func NewFileFailureSink(path string) (*FileFailureSink, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return nil, errors.New("worker: failure sink path is empty")
	}
	return &FileFailureSink{path: path}, nil
}

func (s *FileFailureSink) StoreFailure(ctx context.Context, record FailureRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("creating worker failure directory: %w", err)
	}
	file, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening worker failure sink: %w", err)
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(record); err != nil {
		return fmt.Errorf("encoding worker failure: %w", err)
	}
	return nil
}

// CycleLease prevents the same synchronization cycle from running on more than
// one replica at a time.
type CycleLease interface {
	Acquire(context.Context, string) (release func() error, acquired bool, err error)
}

// PostgresAdvisoryLease holds an advisory lock on one dedicated connection for
// the lifetime of a synchronization cycle.
type PostgresAdvisoryLease struct {
	db *sql.DB
}

func NewPostgresAdvisoryLease(db *sql.DB) *PostgresAdvisoryLease {
	return &PostgresAdvisoryLease{db: db}
}

func (l *PostgresAdvisoryLease) Acquire(
	ctx context.Context,
	key string,
) (func() error, bool, error) {
	if l == nil || l.db == nil {
		return func() error { return nil }, true, nil
	}

	conn, err := l.db.Conn(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquiring worker lease connection: %w", err)
	}

	var acquired bool
	if err := conn.QueryRowContext(
		ctx,
		"SELECT pg_try_advisory_lock(hashtext($1))",
		key,
	).Scan(&acquired); err != nil {
		_ = conn.Close()
		return nil, false, fmt.Errorf("acquiring worker lease: %w", err)
	}
	if !acquired {
		_ = conn.Close()
		return func() error { return nil }, false, nil
	}

	release := func() error {
		var released bool
		unlockErr := conn.QueryRowContext(
			context.Background(),
			"SELECT pg_advisory_unlock(hashtext($1))",
			key,
		).Scan(&released)
		closeErr := conn.Close()
		if unlockErr != nil {
			return fmt.Errorf("releasing worker lease: %w", unlockErr)
		}
		if !released {
			return errors.New("worker: advisory lease was not held")
		}
		if closeErr != nil {
			return fmt.Errorf("closing worker lease connection: %w", closeErr)
		}
		return nil
	}
	return release, true, nil
}

type providerCircuit struct {
	failures  int
	openUntil time.Time
}

// DataSyncConfig configures bounded retries and multi-source synchronization.
type DataSyncConfig struct {
	Interval          time.Duration
	MetadataTargets   []MetadataTarget
	PriceClients      map[string][]service.PriceClient
	RequestsPerSecond float64
	RequestBurst      int
	RetryAttempts     int
	RetryBaseDelay    time.Duration
	CircuitFailures   int
	CircuitCooldown   time.Duration
	MaxPriceRatio     float64
	HistoryRetention  time.Duration
	FailureSink       FailureSink
	Lease             CycleLease
}

func (c DataSyncConfig) validate() error {
	if c.Interval <= 0 {
		return errors.New("worker: interval must be positive")
	}
	if c.RetryAttempts < 1 {
		return errors.New("worker: retry attempts must be positive")
	}
	if c.RetryBaseDelay < 0 {
		return errors.New("worker: retry delay cannot be negative")
	}
	if c.CircuitFailures < 1 {
		return errors.New("worker: circuit failure threshold must be positive")
	}
	if c.CircuitCooldown < 0 {
		return errors.New("worker: circuit cooldown cannot be negative")
	}
	if c.RequestsPerSecond < 0 || c.RequestBurst < 0 {
		return errors.New("worker: request limits cannot be negative")
	}
	if c.MaxPriceRatio != 0 && c.MaxPriceRatio <= 1 {
		return errors.New("worker: maximum price ratio must exceed one")
	}
	if c.HistoryRetention < 0 {
		return errors.New("worker: history retention cannot be negative")
	}
	for _, target := range c.MetadataTargets {
		if strings.TrimSpace(target.Game) == "" || strings.TrimSpace(target.Language) == "" {
			return errors.New("worker: metadata targets require game and language")
		}
	}
	return nil
}

func defaultDataSyncConfig(interval time.Duration) DataSyncConfig {
	return DataSyncConfig{
		Interval:        interval,
		RetryAttempts:   1,
		CircuitFailures: 3,
		CircuitCooldown: 5 * time.Minute,
		RequestBurst:    1,
	}
}

func newRequestLimiter(requestsPerSecond float64, burst int) *rate.Limiter {
	if requestsPerSecond <= 0 {
		return rate.NewLimiter(rate.Inf, 1)
	}
	if burst < 1 {
		burst = 1
	}
	return rate.NewLimiter(rate.Limit(requestsPerSecond), burst)
}

func retryDelay(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		return 0
	}
	shift := min(attempt, 10)
	delay := base * time.Duration(1<<shift)
	jitterLimit := max(delay/4, time.Millisecond)
	return delay + time.Duration(rand.Int64N(int64(jitterLimit)))
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func cardFailure(operation string, card models.Card, attempts int, err error) FailureRecord {
	return FailureRecord{
		OccurredAt: time.Now().UTC(),
		Operation:  operation,
		CardID:     card.ID,
		Game:       models.NormalizeGame(card.Game),
		Attempts:   attempts,
		Error:      err.Error(),
	}
}
