package detectiontest

import "testing"

func TestLoadCardCohort(t *testing.T) {
	cards, err := LoadCardCohort()
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != SupportedGameCount*CardsPerCohortGame {
		t.Fatalf("cohort size = %d", len(cards))
	}
}

func TestValidateCardCohortRejectsDuplicateID(t *testing.T) {
	cards, err := LoadCardCohort()
	if err != nil {
		t.Fatal(err)
	}
	cards[1].ID = cards[0].ID
	if err := ValidateCardCohort(cards); err == nil {
		t.Fatal("duplicate cohort ID was accepted")
	}
}
