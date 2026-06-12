package service

import (
	"fmt"
	"pokget/internal/models"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

type DummyPriceClient struct{}

func (d *DummyPriceClient) FetchPrice(_ models.Card) (float64, float64, error) {
	time.Sleep(1 * time.Second) // simulate network delay of 1 second
	return 1.0, 1.0, nil
}

func (d *DummyPriceClient) ApplyMultiplier(price float64, _ string, multipliers map[string]float64) float64 {
	return price
}

func BenchmarkWorkerRefreshAllPrices(b *testing.B) {
	db, mock, err := sqlmock.New()
	if err != nil {
		b.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer db.Close()

	priceClient := &DummyPriceClient{}
	workerService := NewWorkerService(db, priceClient)

	numCards := 3

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "game"})
		for j := 0; j < numCards; j++ {
			rows.AddRow(fmt.Sprintf("%d", j), "Test Card", "Test Set", "Test Game")
		}

		mock.ExpectQuery("SELECT id, name, set_name, game FROM cards").WillReturnRows(rows)

		for j := 0; j < numCards; j++ {
			mock.ExpectExec("UPDATE cards").
				WithArgs(1.0, 1.0, fmt.Sprintf("%d", j)).
				WillReturnResult(sqlmock.NewResult(1, 1))
		}

		b.StartTimer()

		workerService.refreshAllPrices()
	}
}
