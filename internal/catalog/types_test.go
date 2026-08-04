package catalog

import (
	"encoding/json"
	"testing"
)

func TestCardRecordValidate(t *testing.T) {
	t.Parallel()

	valid := CardRecord{
		SourceCardID: "123",
		Name:         "Card",
		SetName:      "Set",
		Language:     "en",
		Images: []ImageRecord{
			{SourceImageID: "front", URL: "https://example.com/front.jpg"},
		},
		Printings: []PrintingRecord{
			{
				SourcePrintingID: "SET-001",
				SetName:          "Set",
				SourceImageIDs:   []string{"front"},
			},
		},
		Metadata: json.RawMessage(`{"key":"value"}`),
	}

	tests := []struct {
		name    string
		mutate  func(*CardRecord)
		wantErr bool
	}{
		{name: "valid", mutate: func(*CardRecord) {}},
		{name: "missing source card id", mutate: func(r *CardRecord) { r.SourceCardID = "" }, wantErr: true},
		{name: "invalid metadata", mutate: func(r *CardRecord) { r.Metadata = json.RawMessage(`{`) }, wantErr: true},
		{name: "duplicate image", mutate: func(r *CardRecord) { r.Images = append(r.Images, r.Images[0]) }, wantErr: true},
		{name: "unknown printing image", mutate: func(r *CardRecord) { r.Printings[0].SourceImageIDs = []string{"missing"} }, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := valid
			record.Images = append([]ImageRecord{}, valid.Images...)
			record.Printings = append([]PrintingRecord{}, valid.Printings...)
			record.Printings[0].SourceImageIDs = append([]string{}, valid.Printings[0].SourceImageIDs...)
			test.mutate(&record)

			err := record.Validate()
			if test.wantErr && err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestGameValid(t *testing.T) {
	t.Parallel()

	for _, game := range []Game{
		GamePokemon,
		GameMagic,
		GameOnePiece,
		GameLorcana,
		GameWeissSchwarz,
		GameYuGiOh,
	} {
		if !game.Valid() {
			t.Fatalf("Game(%q).Valid() = false", game)
		}
	}
	if Game("unknown").Valid() {
		t.Fatal("unknown game is valid")
	}
}
