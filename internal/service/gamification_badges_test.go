package service

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// High Roller must count market value, not the collector-typed custom price.
func TestHighRollerIgnoresCustomPrice(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SUM\(COALESCE\(c\.price_usd, 0\)\)`).WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(0))

	NewGamificationService(db).CheckForBadges("user-1")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("High Roller still reads custom_price: %v", err)
	}
}
