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
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGetBindersByUserID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer db.Close()

	s := NewBinderService(db)
	userID := "user-123"
	now := time.Now()

	rows := sqlmock.NewRows([]string{"id", "name", "description", "created_at", "is_default", "is_private", "card_count"}).
		AddRow("binder-1", "Binder 1", "Desc 1", now, true, false, 10).
		AddRow("binder-2", "Binder 2", "Desc 2", now, false, true, 5)

	mock.ExpectQuery("SELECT id, name, description, created_at, is_default, is_private").
		WithArgs(userID).
		WillReturnRows(rows)

	binders, err := s.GetBindersByUserID(context.Background(), userID)
	if err != nil {
		t.Errorf("error was not expected while fetching binders: %s", err)
	}

	if len(binders) != 2 {
		t.Errorf("expected 2 binders, got %d", len(binders))
	}

	if binders[0].Name != "Binder 1" || binders[0].CardCount != 10 {
		t.Errorf("binder 0 mismatch: %+v", binders[0])
	}
}
