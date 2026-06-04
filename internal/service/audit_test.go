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
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to open mock db: %v", err)
	}
	defer db.Close()

	s := NewAuditService(db)

	t.Run("Log_Success", func(t *testing.T) {
		mock.ExpectExec("INSERT INTO audit_logs").WithArgs("user-1", "LOGIN", sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))

		s.Log("user-1", "LOGIN", map[string]interface{}{"ip": "1.2.3.4"})

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("Log_Success_NoMetadata", func(t *testing.T) {
		mock.ExpectExec("INSERT INTO audit_logs").WithArgs("user-1", "LOGIN", []byte("{}")).
			WillReturnResult(sqlmock.NewResult(1, 1))

		s.Log("user-1", "LOGIN", nil)

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("Log_Error_NoMetadata", func(_ *testing.T) {
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnError(sql.ErrConnDone)
		// Should not panic, just log the error
		s.Log("user-1", "LOGIN", nil)
	})

	t.Run("Log_Error", func(_ *testing.T) {
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnError(sql.ErrConnDone)
		// Should not panic, just log the error
		s.Log("user-1", "LOGIN", map[string]interface{}{"ip": "1.2.3.4"})
	})

	t.Run("Log_JSON_Marshal_Error", func(t *testing.T) {
		mock.ExpectExec("INSERT INTO audit_logs").WithArgs("user-1", "LOGIN", []byte("{}")).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Channels cannot be marshaled to JSON
		s.Log("user-1", "LOGIN", map[string]interface{}{"error": make(chan int)})

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})
}
