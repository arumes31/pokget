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
)

type Rank struct {
	Title    string
	MinXP    int
	IconURL  string
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
	DB *sql.DB
}

func NewGamificationService(db *sql.DB) *GamificationService {
	return &GamificationService{DB: db}
}

func (s *GamificationService) AddXP(userID string, amount int) (int, string, error) {
	var currentXP int
	var currentRank string
	err := s.DB.QueryRow("SELECT xp, rank_title FROM users WHERE id = $1", userID).Scan(&currentXP, &currentRank)
	if err != nil {
		return 0, "", err
	}

	newXP := currentXP + amount
	newRank := currentRank

	// Calculate new rank
	for _, r := range Ranks {
		if newXP >= r.MinXP {
			newRank = r.Title
		} else {
			break
		}
	}

	_, err = s.DB.Exec("UPDATE users SET xp = $1, rank_title = $2 WHERE id = $3", newXP, newRank, userID)
	if err == nil {
		// Asynchronously check for badges to not block the main flow
		go s.CheckForBadges(userID)
	}
	return newXP, newRank, err
}

func (s *GamificationService) CheckForBadges(userID string) {
	// 1. Check for "First Pull" badge
	var count int
	_ = s.DB.QueryRow("SELECT COUNT(*) FROM portfolio WHERE user_id = $1", userID).Scan(&count)
	if count >= 1 {
		s.AwardBadge(userID, "First Pull")
	}

	// 2. Check for "High Roller" badge
	var totalValue float64
	_ = s.DB.QueryRow(`
		SELECT SUM(COALESCE(p.custom_price, c.price_usd)) 
		FROM portfolio p 
		JOIN cards c ON p.card_id = c.id 
		WHERE p.user_id = $1`, userID).Scan(&totalValue)
	
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

	// Try to insert (ignore if already exists)
	_, err = s.DB.Exec("INSERT INTO user_badges (user_id, badge_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", userID, badgeID)
	if err == nil {
		// Award XP for the badge if it was newly inserted
		// Note: Exec returns rows affected, but it's simpler to just call AddXP if the insert succeeded
		// However, ON CONFLICT DO NOTHING might make it hard to know if it's new.
		// Let's use a more robust way.
		result, _ := s.DB.Exec("INSERT INTO user_badges (user_id, badge_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", userID, badgeID)
		if rows, _ := result.RowsAffected(); rows > 0 {
			_, _, _ = s.AddXP(userID, xpReward)
		}
	}
}

func (s *GamificationService) GetUserRank(xp int) Rank {
	var lastRank Rank
	for _, r := range Ranks {
		if xp >= r.MinXP {
			lastRank = r
		} else {
			break
		}
	}
	return lastRank
}

func (s *GamificationService) GetProgressToNextRank(xp int) (int, int, float64) {
	var currentRank Rank
	var nextRank Rank
	found := false
	for i, r := range Ranks {
		if xp >= r.MinXP {
			currentRank = r
			if i+1 < len(Ranks) {
				nextRank = Ranks[i+1]
				found = true
			} else {
				found = false // We are at the max rank
			}
		} else {
			break
		}
	}

	if !found {
		return xp, xp, 100.0 // Max rank behavior
	}

	relativeXP := xp - currentRank.MinXP
	requiredXP := nextRank.MinXP - currentRank.MinXP
	percent := (float64(relativeXP) / float64(requiredXP)) * 100
	return relativeXP, requiredXP, percent
}
