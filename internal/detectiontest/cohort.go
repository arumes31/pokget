package detectiontest

import (
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
)

const CardsPerCohortGame = 100

//go:embed testdata/card-cohort-600.csv
var cardCohortCSV string

type CohortCard struct {
	Game string
	ID   string
	Name string
}

// LoadCardCohort returns the fixed 600-card acceptance cohort used by the full
// image audit. Images remain outside Git; IDs make each DB/image-volume run use
// the same 100 unique cards per supported TCG.
func LoadCardCohort() ([]CohortCard, error) {
	reader := csv.NewReader(strings.NewReader(cardCohortCSV))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading card cohort header: %w", err)
	}
	if len(header) != 3 || header[0] != "game" || header[1] != "id" || header[2] != "name" {
		return nil, fmt.Errorf("unexpected card cohort header: %v", header)
	}

	cards := make([]CohortCard, 0, SupportedGameCount*CardsPerCohortGame)
	for row := 2; ; row++ {
		record, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("reading card cohort row %d: %w", row, readErr)
		}
		cards = append(cards, CohortCard{Game: record[0], ID: record[1], Name: record[2]})
	}
	if err := ValidateCardCohort(cards); err != nil {
		return nil, err
	}
	return cards, nil
}

func ValidateCardCohort(cards []CohortCard) error {
	expectedTotal := SupportedGameCount * CardsPerCohortGame
	if len(cards) != expectedTotal {
		return fmt.Errorf("card cohort has %d cards, want %d", len(cards), expectedTotal)
	}
	counts := make(map[string]int, SupportedGameCount)
	seenIDs := make(map[string]struct{}, len(cards))
	for row, card := range cards {
		if strings.TrimSpace(card.Game) == "" || strings.TrimSpace(card.ID) == "" || strings.TrimSpace(card.Name) == "" {
			return fmt.Errorf("card cohort row %d has an empty field", row+2)
		}
		if _, duplicate := seenIDs[card.ID]; duplicate {
			return fmt.Errorf("card cohort contains duplicate ID %q", card.ID)
		}
		seenIDs[card.ID] = struct{}{}
		counts[card.Game]++
	}
	for _, game := range []string{"pokemon", "magic", "one_piece", "lorcana", "weiss_schwarz", "yugioh"} {
		if counts[game] != CardsPerCohortGame {
			return fmt.Errorf("card cohort game %q has %d cards, want %d", game, counts[game], CardsPerCohortGame)
		}
	}
	if len(counts) != SupportedGameCount {
		return fmt.Errorf("card cohort contains %d games, want %d", len(counts), SupportedGameCount)
	}
	return nil
}
