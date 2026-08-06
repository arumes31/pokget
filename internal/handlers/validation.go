package handlers

import (
	"fmt"
	"math"
	"strconv"
)

// PostgreSQL DECIMAL(12,2) can store at most ten integer digits.
const maxStoredPrice = 9_999_999_999.99

func parseFiniteFloat(raw string, minValue, maxValue float64) (float64, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse number: %w", err)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value < minValue || value > maxValue {
		return 0, fmt.Errorf("number must be finite and between %g and %g", minValue, maxValue)
	}
	return value, nil
}

func parseOptionalPrice(raw string) (*float64, error) {
	if raw == "" {
		return nil, nil
	}
	value, err := parseFiniteFloat(raw, 0, maxStoredPrice)
	if err != nil {
		return nil, err
	}
	return &value, nil
}
