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
	"log/slog"
	"sort"
)

type Rank struct {
	Title   string
	MinXP   int
	IconURL string
}

var Ranks = []Rank{
	{"Novice Collector", 0, "/static/img/ranks/novice.png"},
	{"Card Scout", 500, "/static/img/ranks/scout.png"},
	{"Hobbyist", 1500, "/static/img/ranks/hobbyist.png"},
	{"Portfolio Manager", 3500, "/static/img/ranks/manager.png"},
	{"Elite Collector", 7500, "/static/img/ranks/elite.png"},
	{"Vault Guardian", 15000, "/static/img/ranks/guardian.png"},
	{"Set Master", 30000, "/static/img/ranks/master.png"},
	{"TCG Legend", 60000, "/static/img/ranks/legend.png"},
	{"Prestige Collector", 120000, "/static/img/ranks/prestige.png"},
	{"Apex Collector", 250000, "/static/img/ranks/apex.png"},
}

type GamificationService struct {
	DB       *sql.DB
	badgeSem chan struct{}
}

func NewGamificationService(db *sql.DB) *GamificationService {
	return &GamificationService{
		DB:       db,
		badgeSem: make(chan struct{}, 5), // max 5 concurrent badge checks
	}
}

func (s *GamificationService) AddXP(userID string, amount int) (int, string, error) {
	// BUG-C02 FIX: Use atomic increment instead of read-then-write to prevent race conditions
	// under concurrent requests. Use UPDATE ... RETURNING to get the new XP value atomically.
	var newXP int
	var newRank string
	err := s.DB.QueryRow(
		"UPDATE users SET xp = xp + $1, rank_title = $2 WHERE id = $3 RETURNING xp, rank_title",
		amount, s.GetUserRank(0).Title, userID, // placeholder rank, will be recalculated below
	).Scan(&newXP, &newRank)
	if err != nil {
		return 0, "", err
	}

	// Recalculate rank based on the new XP value
	actualRank := s.GetUserRank(newXP)
	if actualRank.Title != newRank {
		// Rank changed, update it
		if _, err := s.DB.Exec("UPDATE users SET rank_title = $1 WHERE id = $2", actualRank.Title, userID); err != nil {
			slog.Error("failed to update rank_title", "error", err, "user_id", userID)
		}
		newRank = actualRank.Title
	}

	// Asynchronously check for badges with semaphore to limit concurrency
	select {
	case s.badgeSem <- struct{}{}:
		go func() {
			defer func() { <-s.badgeSem }()
			s.CheckForBadges(userID)
		}()
	default:
		slog.Warn("Badge check skipped: too many concurrent checks", "user_id", userID)
	}

	return newXP, newRank, nil
}

func (s *GamificationService) CheckForBadges(userID string) {
	// 1. Check for "First Pull" badge
	var count int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM portfolio WHERE user_id = $1", userID).Scan(&count); err != nil {
		slog.Warn("Failed to check portfolio count for badges", "user_id", userID, "error", err)
		return
	}
	if count >= 1 {
		s.AwardBadge(userID, "First Pull")
	}

	// 2. Check for "High Roller" badge
	var totalValue float64
	if err := s.DB.QueryRow(`
		SELECT SUM(COALESCE(p.custom_price, c.price_usd))
		FROM portfolio p
		JOIN cards c ON p.card_id = c.id
		WHERE p.user_id = $1`, userID).Scan(&totalValue); err != nil {
		slog.Warn("Failed to check portfolio value for badges", "user_id", userID, "error", err)
		return
	}

	if totalValue >= 10000 {
		s.AwardBadge(userID, "High Roller")
	}
}

func (s *GamificationService) AwardBadge(userID, badgeName string) {
	var badgeID string
	var xpReward int
	err := s.DB.QueryRow("SELECT id, xp_reward FROM badges WHERE name = $1", badgeName).Scan(&badgeID, &xpReward)
	if err != nil {
		return
	}

	// BUG-C01 FIX: Use a single INSERT with ON CONFLICT DO NOTHING and check RowsAffected
	// to determine if the badge was newly awarded. Previously, two INSERT statements were
	// executed — the second always conflicted with the first, so badge XP was never awarded.
	result, err := s.DB.Exec("INSERT INTO user_badges (user_id, badge_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", userID, badgeID)
	if err != nil {
		slog.Error("Failed to award badge", "badge", badgeName, "user_id", userID, "error", err)
		return
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		// Badge was newly awarded — grant the XP reward
		_, _, _ = s.AddXP(userID, xpReward)
	}
}

func (s *GamificationService) GetUserRank(xp int) Rank {
	// Use binary search to find the highest rank with MinXP <= xp
	// sort.Search returns the first index i where f(i) is true.
	// We want the last index where Ranks[i].MinXP <= xp.
	// Searching for the first index where Ranks[i].MinXP > xp, then subtracting 1.
	i := sort.Search(len(Ranks), func(i int) bool {
		return Ranks[i].MinXP > xp
	})

	if i > 0 {
		return Ranks[i-1]
	}
	return Ranks[0]
}

func (s *GamificationService) GetProgressToNextRank(xp int) (int, int, float64) {
	// Find the first index where Ranks[i].MinXP > xp
	i := sort.Search(len(Ranks), func(i int) bool {
		return Ranks[i].MinXP > xp
	})

	// If i == len(Ranks), we are at the highest rank (xp >= Ranks[len-1].MinXP)
	if i >= len(Ranks) {
		return xp, xp, 100.0 // Max rank behavior
	}

	// currentRank is Ranks[i-1] (highest rank with MinXP <= xp)
	// nextRank is Ranks[i] (first rank with MinXP > xp)
	// If i == 0, it means xp < Ranks[0].MinXP, which shouldnt happen as Ranks[0].MinXP is 0
	// but we handle it just in case.
	currIdx := i - 1
	if i == 0 {
		currIdx = 0
	}

	currentRank := Ranks[currIdx]
	nextRank := Ranks[i]

	relativeXP := xp - currentRank.MinXP
	requiredXP := nextRank.MinXP - currentRank.MinXP
	percent := (float64(relativeXP) / float64(requiredXP)) * 100
	return relativeXP, requiredXP, percent
}
