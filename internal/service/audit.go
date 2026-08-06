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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// AuditService handles application audit logging.
type AuditService struct {
	db      *sql.DB
	logCh   chan auditEntry
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	closeMu sync.RWMutex
	closed  bool
}

type auditEntry struct {
	userID       string
	action       string
	metadataJSON string
}

// NewAuditService creates a new AuditService.
func NewAuditService(db *sql.DB) *AuditService {
	ctx, cancel := context.WithCancel(context.Background())
	s := &AuditService{
		db:     db,
		logCh:  make(chan auditEntry, 256),
		ctx:    ctx,
		cancel: cancel,
	}
	s.wg.Add(1)
	go s.processLogs()
	return s
}

func (s *AuditService) processLogs() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case entry, ok := <-s.logCh:
			if !ok {
				return
			}
			s.writeLog(entry)
		}
	}
}

func (s *AuditService) writeLog(entry auditEntry) {
	_, err := s.db.ExecContext(
		s.ctx,
		"INSERT INTO audit_logs (user_id, action, metadata, created_at) VALUES ($1, $2, $3, NOW())",
		entry.userID, entry.action, entry.metadataJSON,
	)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.Error("Failed to write audit log", "user_id", entry.userID, "action", entry.action, "error", err)
	}
}

// Log records an audit entry asynchronously.
func (s *AuditService) Log(userID, action string, metadata map[string]interface{}) {
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		slog.Error("Failed to marshal audit log metadata", "user_id", userID, "action", action, "error", err)
		metadataJSON = []byte(fmt.Sprintf(`{"error":"marshal failed","action":%q}`, action))
	}

	s.closeMu.RLock()
	defer s.closeMu.RUnlock()
	if s.closed {
		slog.Warn("Audit service closed, dropping entry", "user_id", userID, "action", action)
		return
	}

	select {
	case s.logCh <- auditEntry{userID: userID, action: action, metadataJSON: string(metadataJSON)}:
	default:
		slog.Warn("Audit log channel full, dropping entry", "user_id", userID, "action", action)
	}
}

// Close stops the background log processor.
func (s *AuditService) Close() {
	_ = s.CloseContext(context.Background())
}

// CloseContext drains queued audit records until ctx expires. Once the
// deadline is reached, any in-flight database write is cancelled so shutdown
// cannot block indefinitely on an unavailable database.
func (s *AuditService) CloseContext(ctx context.Context) error {
	s.closeMu.Lock()
	if !s.closed {
		s.closed = true
		close(s.logCh)
	}
	s.closeMu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.cancel()
		return nil
	case <-ctx.Done():
		s.cancel()
		return ctx.Err()
	}
}
