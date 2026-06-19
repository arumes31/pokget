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
	"fmt"
	"log/slog"
	"sync"
)

// AuditService handles application audit logging.
type AuditService struct {
	db    *sql.DB
	logCh chan auditEntry
	wg    sync.WaitGroup
}

type auditEntry struct {
	userID   string
	action   string
	metadata map[string]interface{}
}

// NewAuditService creates a new AuditService.
func NewAuditService(db *sql.DB) *AuditService {
	s := &AuditService{
		db:    db,
		logCh: make(chan auditEntry, 256),
	}
	s.wg.Add(1)
	go s.processLogs()
	return s
}

func (s *AuditService) processLogs() {
	defer s.wg.Done()
	for entry := range s.logCh {
		metadataJSON, err := json.Marshal(entry.metadata)
		if err != nil {
			slog.Error("Failed to marshal audit log metadata", "user_id", entry.userID, "action", entry.action, "error", err)
			metadataJSON = []byte(fmt.Sprintf(`{"error":"marshal failed","action":%q}`, entry.action))
		}
		_, err = s.db.Exec(
			"INSERT INTO audit_logs (user_id, action, metadata, created_at) VALUES ($1, $2, $3, NOW())",
			entry.userID, entry.action, string(metadataJSON),
		)
		if err != nil {
			slog.Error("Failed to write audit log", "user_id", entry.userID, "action", entry.action, "error", err)
		}
	}
}

// Log records an audit entry asynchronously.
func (s *AuditService) Log(userID, action string, metadata map[string]interface{}) {
	select {
	case s.logCh <- auditEntry{userID: userID, action: action, metadata: metadata}:
	default:
		slog.Warn("Audit log channel full, dropping entry", "user_id", userID, "action", action)
	}
}

// Close stops the background log processor.
func (s *AuditService) Close() {
	close(s.logCh)
	s.wg.Wait()
}
