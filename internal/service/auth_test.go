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
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

type mockMailer struct {
	sendFunc func(to, token string) error
}

func (m *mockMailer) SendConfirmationEmail(to, token string) error {
	return m.sendFunc(to, token)
}

func TestAuthService_RegisterUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to open mock db: %v", err)
	}
	defer db.Close()

	audit := NewAuditService(db)
	mailer := &mockMailer{
		sendFunc: func(to, token string) error {
			return nil
		},
	}
	authSvc := NewAuthService(db, mailer, audit)

	t.Run("Success_NewUser", func(t *testing.T) {
		email := "new@example.com"
		password := "password123"

		mock.ExpectExec("INSERT INTO users").
			WithArgs(email, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectExec("INSERT INTO audit_logs").
			WithArgs("", "USER_REGISTER", sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := authSvc.RegisterUser(context.Background(), email, password)
		if err != nil {
			t.Errorf("RegisterUser failed: %v", err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("Success_UnverifiedUser", func(t *testing.T) {
		email := "unverified@example.com"
		password := "password123"

		mock.ExpectExec("INSERT INTO users").
			WithArgs(email, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1)) // UPSERT happened

		mock.ExpectExec("INSERT INTO audit_logs").
			WithArgs("", "USER_REGISTER", sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := authSvc.RegisterUser(context.Background(), email, password)
		if err != nil {
			t.Errorf("RegisterUser failed: %v", err)
		}
	})

	t.Run("Failure_AlreadyVerified", func(t *testing.T) {
		email := "verified@example.com"
		password := "password123"

		mock.ExpectExec("INSERT INTO users").
			WithArgs(email, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 0)) // No rows affected due to WHERE is_verified = FALSE

		err := authSvc.RegisterUser(context.Background(), email, password)
		if err == nil {
			t.Error("Expected error for already verified user")
		}
	})

	t.Run("Failure_DBError", func(t *testing.T) {
		email := "error@example.com"
		password := "password123"

		mock.ExpectExec("INSERT INTO users").
			WillReturnError(sql.ErrConnDone)

		err := authSvc.RegisterUser(context.Background(), email, password)
		if err == nil {
			t.Error("Expected error on DB failure")
		}
	})
}
