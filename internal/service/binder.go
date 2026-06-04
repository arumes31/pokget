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
	"context"
	"database/sql"
	"pokget/internal/models"
)

type BinderService struct {
	DB *sql.DB
}

func NewBinderService(db *sql.DB) *BinderService {
	return &BinderService{DB: db}
}

// GetBindersByUserID fetches all binders for a user, including card counts.
func (s *BinderService) GetBindersByUserID(ctx context.Context, userID string) ([]models.Binder, error) {
	// ⚡ Bolt: Using a correlated subquery to count cards instead of a LEFT JOIN with GROUP BY.
	// This optimization avoids joining the entire portfolio table with binders before aggregation,
	// which is significantly faster when users have many cards across multiple binders.
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, name, description, created_at, is_default, is_private,
		       (SELECT COUNT(*) FROM portfolio WHERE binder_id = binders.id) as card_count
		FROM binders
		WHERE user_id = $1
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var binders []models.Binder
	for rows.Next() {
		var b models.Binder
		if err := rows.Scan(&b.ID, &b.Name, &b.Description, &b.CreatedAt, &b.IsDefault, &b.IsPrivate, &b.CardCount); err != nil {
			return nil, err
		}
		binders = append(binders, b)
	}
	return binders, nil
}

// GetBinderByID fetches a single binder by ID and user ID.
func (s *BinderService) GetBinderByID(ctx context.Context, binderID, userID string) (*models.Binder, error) {
	var b models.Binder
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, name, description, created_at, is_default, is_private
		FROM binders WHERE id = $1 AND user_id = $2`, binderID, userID).
		Scan(&b.ID, &b.Name, &b.Description, &b.CreatedAt, &b.IsDefault, &b.IsPrivate)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// CreateBinder creates a new binder for a user.
func (s *BinderService) CreateBinder(ctx context.Context, userID, name, description string) error {
	_, err := s.DB.ExecContext(ctx, "INSERT INTO binders (user_id, name, description) VALUES ($1, $2, $3)", userID, name, description)
	return err
}
