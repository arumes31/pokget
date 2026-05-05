// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package service

import (
	"database/sql"
	"fmt"
	"pokget/internal/models"
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
