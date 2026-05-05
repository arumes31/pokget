package service

import (
	"database/sql"
	"fmt"
	"gettos/internal/models"
	"image"

	"github.com/corona10/goimagehash"
)

type FingerprintService struct {
	db *sql.DB
}

func NewFingerprintService(db *sql.DB) *FingerprintService {
	return &FingerprintService{db: db}
}

// CalculateHash computes the perceptual hash (pHash) of an image
func (s *FingerprintService) CalculateHash(img image.Image) (int64, error) {
	hash, err := goimagehash.PerceptionHash(img)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate pHash: %w", err)
	}
	return int64(hash.GetHash()), nil // #nosec G115 - conversion for BIGINT storage
}

// MatchFingerprint searches for a card with a similar pHash in the database
func (s *FingerprintService) MatchFingerprint(hashVal int64) (*models.Card, int, error) {
	// We look for cards where the Hamming distance is small
	// Since we store as BIGINT, we can use BIT_COUNT and XOR if the DB supports it,
	// or just a range search for now if we want exact/very close matches.
	// For simplicity, let's look for exact matches or very close ones in Go.

	rows, err := s.db.Query(`
		SELECT id, name, set_name, image_url, phash 
		FROM cards 
		WHERE phash IS NOT NULL`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var bestMatch *models.Card
	minDistance := 64 // Max bits in pHash

	targetHash := goimagehash.NewImageHash(uint64(hashVal), goimagehash.PHash) // #nosec G115

	for rows.Next() {
		var c models.Card
		var storedPhash int64
		if err := rows.Scan(&c.ID, &c.Name, &c.Set, &c.ImageURL, &storedPhash); err != nil {
			continue
		}

		storedHash := goimagehash.NewImageHash(uint64(storedPhash), goimagehash.PHash) // #nosec G115
		distance, err := targetHash.Distance(storedHash)
		if err != nil {
			continue
		}

		if distance < minDistance {
			minDistance = distance
			bestMatch = &c
		}
	}

	// Threshold for a "good" match (usually < 10 for pHash)
	if minDistance > 10 {
		return nil, minDistance, nil
	}

	return bestMatch, minDistance, nil
}
