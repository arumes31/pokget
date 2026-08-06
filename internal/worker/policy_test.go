package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pokget/internal/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
)

func TestRetryDelayIsBounded(t *testing.T) {
	base := 10 * time.Millisecond
	for attempt := 0; attempt < 20; attempt++ {
		delay := retryDelay(base, attempt)
		maximum := base*time.Duration(1<<min(attempt, 10)) +
			base*time.Duration(1<<min(attempt, 10))/4
		if delay < base || delay > maximum {
			t.Fatalf("retryDelay(%d) = %v, want within [%v, %v]", attempt, delay, base, maximum)
		}
	}
}

func TestMedian(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		want   float64
	}{
		{name: "empty", want: 0},
		{name: "odd", values: []float64{90, 10, 20}, want: 20},
		{name: "even", values: []float64{30, 10}, want: 20},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := median(test.values); got != test.want {
				t.Fatalf("median() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPriceAnomalyPolicy(t *testing.T) {
	worker := &DataSyncWorker{maxPriceRatio: 5}
	if worker.priceAnomalous(decimal.NewFromInt(100), 499) {
		t.Fatal("499 should be inside a 5x price envelope")
	}
	if !worker.priceAnomalous(decimal.NewFromInt(100), 501) {
		t.Fatal("501 should be outside a 5x price envelope")
	}
	if !worker.priceAnomalous(decimal.NewFromInt(100), 19) {
		t.Fatal("19 should be outside a 5x price envelope")
	}
}

func TestCircuitBreakerOpensAndRecovers(t *testing.T) {
	worker := &DataSyncWorker{
		circuitFailures: 2,
		circuitCooldown: time.Second,
		circuits:        make(map[string]providerCircuit),
	}
	now := time.Now()
	worker.circuitFailed("source", now)
	if !worker.circuitReady("source", now) {
		t.Fatal("circuit opened before reaching its threshold")
	}
	worker.circuitFailed("source", now)
	if worker.circuitReady("source", now) {
		t.Fatal("circuit remained closed at its threshold")
	}
	if !worker.circuitReady("source", now.Add(2*time.Second)) {
		t.Fatal("circuit did not recover after cooldown")
	}
}

func TestFileFailureSinkWritesReplayableJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "failures.jsonl")
	sink, err := NewFileFailureSink(path)
	if err != nil {
		t.Fatal(err)
	}
	want := FailureRecord{
		OccurredAt: time.Now().UTC().Truncate(time.Second),
		Operation:  "price",
		CardID:     "card-1",
		Game:       "pokemon",
		Attempts:   3,
		Error:      "provider timeout",
	}
	if err := sink.StoreFailure(context.Background(), want); err != nil {
		t.Fatal(err)
	}

	file, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(contents)))
	if !scanner.Scan() {
		t.Fatal("failure sink did not write a JSONL record")
	}
	var got FailureRecord
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("failure record = %+v, want %+v", got, want)
	}
}

func TestConfiguredWorkerRejectsInvalidPolicy(t *testing.T) {
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	_, err = NewConfiguredDataSyncWorker(database, nil, nil, nil, DataSyncConfig{})
	if err == nil {
		t.Fatal("expected invalid zero-value policy to fail")
	}
}

func TestCardFailureDoesNotStoreSensitiveProviderData(t *testing.T) {
	record := cardFailure(
		"price",
		models.Card{ID: "card-1", Game: "Pokémon"},
		3,
		errors.New("request failed"),
	)
	if record.Game != "pokemon" || record.CardID != "card-1" || record.Attempts != 3 {
		t.Fatalf("unexpected failure record: %+v", record)
	}
}

func TestCleanupPriceHistoryUsesConfiguredRetention(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	mock.ExpectExec("DELETE FROM price_history").WithArgs(30).
		WillReturnResult(sqlmock.NewResult(0, 4))
	worker := &DataSyncWorker{db: database, historyRetention: 30 * 24 * time.Hour}
	worker.cleanupPriceHistory(context.Background())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
