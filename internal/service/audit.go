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
	"encoding/json"
	"log/slog"
)

type AuditService struct {
	db *sql.DB
}

func NewAuditService(db *sql.DB) *AuditService {
	return &AuditService{db: db}
}

func (s *AuditService) Log(userID, action string, metadata map[string]interface{}) {
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		slog.Error("Audit: Failed to marshal metadata", "error", err)
		metaJSON = []byte("{}")
	}

	_, err = s.db.Exec("INSERT INTO audit_logs (user_id, action, metadata) VALUES ($1, $2, $3)",
		userID, action, metaJSON)
	if err != nil {
		slog.Error("Audit: Failed to insert log", "error", err)
	}
}
