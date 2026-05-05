package service

import (
	"gettos/internal/models"
	"regexp"
	"testing"
)

func TestOCRMatchingLogic(t *testing.T) {
	mockCards := []models.Card{
		{Name: "Charizard"},
		{Name: "Pikachu"},
		{Name: "Mew"},
	}

	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{"Exact match", "Pikachu", "Pikachu"},
		{"Case insensitive", "charizard", "Charizard"},
		{"Partial sentence", "Found a rare Charizard card today", "Charizard"},
		{"Word boundary - Mewtwo not Mew", "Mewtwo is here", "Unknown Card"},
		{"No match", "Bulbasaur", "Unknown Card"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detected := "Unknown Card"
			for _, card := range mockCards {
				if containsIgnoreCase(tt.text, card.Name) {
					detected = card.Name
					break
				}
			}
			if detected != tt.expected {
				t.Errorf("For text '%s', expected %s, got %s", tt.text, tt.expected, detected)
			}
		})
	}
}

func containsIgnoreCase(s, substr string) bool {
	if substr == "" {
		return true
	}
	pattern := `(?i)\b` + regexp.QuoteMeta(substr) + `\b`
	matched, _ := regexp.MatchString(pattern, s)
	return matched
}
