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
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAuditService(t *testing.T) {
	tests := []struct {
		name        string
		metadata    map[string]interface{}
		insertError error
	}{
		{name: "Log_Success", metadata: map[string]interface{}{"ip": "1.2.3.4"}},
		{name: "Log_Success_NoMetadata"},
		{name: "Log_Error_NoMetadata", insertError: sql.ErrConnDone},
		{name: "Log_Error", metadata: map[string]interface{}{"ip": "1.2.3.4"}, insertError: sql.ErrConnDone},
		{name: "Log_JSON_Marshal_Error", metadata: map[string]interface{}{"error": make(chan int)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("open mock database: %v", err)
			}
			defer db.Close()

			expectation := mock.ExpectExec("INSERT INTO audit_logs").
				WithArgs("user-1", "LOGIN", sqlmock.AnyArg())
			if tt.insertError != nil {
				expectation.WillReturnError(tt.insertError)
			} else {
				expectation.WillReturnResult(sqlmock.NewResult(1, 1))
			}

			service := NewAuditService(db)
			service.Log("user-1", "LOGIN", tt.metadata)
			service.Close()

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("audit insert expectation: %v", err)
			}
		})
	}
}

func TestAuditServiceCloseFlushesAndIsIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock database: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("INSERT INTO audit_logs").
		WithArgs("user-1", "SHUTDOWN_TEST", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	service := NewAuditService(db)
	service.Log("user-1", "SHUTDOWN_TEST", nil)
	service.Close()
	service.Close()
	service.Log("user-1", "AFTER_CLOSE", nil)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Close() did not flush the queued audit record: %v", err)
	}
}
