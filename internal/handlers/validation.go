package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/lib/pq"
)

// PostgreSQL DECIMAL(12,2) can store at most ten integer digits.
const maxStoredPrice = 9_999_999_999.99

// Rune limits for collector-typed free text.
const (
	maxNotesRunes             = 2000
	maxGradeRunes             = 32
	maxBinderNameRunes        = 100
	maxBinderDescriptionRunes = 1000
	maxErrorTypeRunes         = 64
	maxErrorDescriptionRunes  = 2000
)

type textField struct {
	name, value string
	limit       int
}

// textTooLong returns a client message for the first field over its rune
// limit, or "" when all fit.
func textTooLong(fields ...textField) string {
	for _, f := range fields {
		if utf8.RuneCountInString(f.value) > f.limit {
			return fmt.Sprintf("%s must be at most %d characters", f.name, f.limit)
		}
	}
	return ""
}

// isMissingRow treats a malformed UUID id (SQLSTATE 22P02) like an absent
// row: it can never match, so the caller answers 404 instead of 500.
func isMissingRow(err error) bool {
	var pgErr *pq.Error
	return errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pgErr) && pgErr.Code == "22P02")
}

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
