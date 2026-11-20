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
	"fmt"
	"image"

	"pokget/internal/models"

	"github.com/corona10/goimagehash"
)

// DefaultPhashThresholdHighConf is the strict threshold for high-confidence
// fingerprint matches (SCAN-02).
const DefaultPhashThresholdHighConf = 5

// DefaultPhashThresholdPotential is the relaxed threshold for potential matches
// that need secondary verification (SCAN-02).
const DefaultPhashThresholdPotential = 10

// HashDistance reports how many bits two stored perceptual hashes differ by.
// It is exposed for diagnostics that compare catalog hashes with fresh images.
func HashDistance(a, b int64) int {
	return hammingDistance(uint64(a), uint64(b)) // #nosec G115 - comparing hash bits, not a numeric value
}

// hammingDistance computes the Hamming distance between two uint64 hashes.
func hammingDistance(a, b uint64) int {
	xor := a ^ b
	count := 0
	for xor != 0 {
		count++
		xor &= xor - 1 // Clear lowest set bit
	}
	return count
}

// FingerprintService computes perceptual hashes and carries the pHash
// thresholds the detector's tie-break applies to card.Phash.
type FingerprintService struct {
	PhashHighConf  int // Strict threshold for high-confidence matches (SCAN-02)
	PhashPotential int // Relaxed threshold for potential matches (SCAN-02)
}

// NewFingerprintService creates a FingerprintService with the default thresholds.
func NewFingerprintService() *FingerprintService {
	return &FingerprintService{
		PhashHighConf:  DefaultPhashThresholdHighConf,
		PhashPotential: DefaultPhashThresholdPotential,
	}
}

// CalculateHash computes the perceptual hash (pHash) of an image.
func (s *FingerprintService) CalculateHash(img image.Image) (int64, error) {
	if img == nil {
		return 0, fmt.Errorf("fingerprint: cannot calculate hash of nil image")
	}
	hash, err := goimagehash.PerceptionHash(img)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate pHash: %w", err)
	}
	return int64(hash.GetHash()), nil // #nosec G115 - conversion for BIGINT storage
}

func tcgForCard(card models.Card) models.TCG {
	tcg, err := models.ParseTCG(card.Game)
	if err != nil {
		return models.TCGUnknown
	}
	return tcg
}
