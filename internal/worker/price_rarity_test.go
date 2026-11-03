package worker

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// Catalog sources that publish no rarity (TCGdex) get it from the marketplace
// payload. Cards that already carry a rarity must not be touched.
func TestApplyFuzzyRarityFillsMissingRarities(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "set_code", "set_name", "collector_number"}).
		AddRow("card-jp", "SV3", "Ruler of the Black Flame", "001").
		AddRow("card-en", "sv3", "Obsidian Flames", "002")
	mock.ExpectQuery("SELECT id, COALESCE\\(set_code[\\s\\S]*rarity IS NULL").
		WithArgs("pokemon", "ja", sqlmock.AnyArg()).
		WillReturnRows(rows)
	// The batch is written in id order so consecutive batches lock rows alike.
	mock.ExpectExec("UPDATE cards AS c[\\s\\S]*SET rarity = v.rarity").
		WithArgs("card-en", "Rare Holo", "card-jp", "Common").
		WillReturnResult(sqlmock.NewResult(0, 2))

	worker := &DataSyncWorker{db: db}
	rarities := map[string]string{}
	for key, value := range fuzzyRarityIndex("SV3", "001/108", "Common") {
		rarities[key] = value
	}
	for key, value := range fuzzyRarityIndex("SV3", "002/108", "Rare Holo") {
		rarities[key] = value
	}

	changed, err := worker.applyFuzzyRarity(context.Background(), "pokemon", "ja", nil, rarities)
	if err != nil {
		t.Fatalf("applyFuzzyRarity: %v", err)
	}
	if changed != 2 {
		t.Fatalf("changed = %d, want 2", changed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyFuzzyRaritySkipsWithoutData(t *testing.T) {
	worker := &DataSyncWorker{}
	changed, err := worker.applyFuzzyRarity(context.Background(), "pokemon", "ja", nil, nil)
	if err != nil || changed != 0 {
		t.Fatalf("changed = %d, err = %v, want a no-op", changed, err)
	}
}

// fuzzyRarityIndex mirrors the rarity index the bulk worker builds per scope.
func fuzzyRarityIndex(set, number, rarity string) map[string]string {
	index := make(map[string]string)
	for _, key := range priceMatchKeys(set, number) {
		index[key] = rarity
	}
	return index
}
