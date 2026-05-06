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

package errors

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestAppError(t *testing.T) {
	t.Run("Error_WithInner", func(t *testing.T) {
		inner := errors.New("underlying")
		err := &AppError{Message: "msg", Inner: inner}
		if !strings.Contains(err.Error(), "msg: underlying") {
			t.Errorf("Unexpected error string: %s", err.Error())
		}
	})

	t.Run("Error_WithoutInner", func(t *testing.T) {
		err := &AppError{Message: "msg"}
		if err.Error() != "msg" {
			t.Errorf("Expected msg, got %s", err.Error())
		}
	})

	t.Run("Wrap", func(t *testing.T) {
		inner := errors.New("db fail")
		err := Wrap(http.StatusBadRequest, "bad request", inner)
		if err.Code != http.StatusBadRequest {
			t.Errorf("Expected 400, got %d", err.Code)
		}
		if !strings.Contains(err.StackTrace, "TestHandlers") && !strings.Contains(err.StackTrace, "TestAppError") {
			t.Error("Expected stack trace to contain test function name")
		}
	})

	t.Run("MapToStatusCode", func(t *testing.T) {
		appErr := &AppError{Code: http.StatusTeapot}
		if MapToStatusCode(appErr) != http.StatusTeapot {
			t.Errorf("Expected 418, got %d", MapToStatusCode(appErr))
		}

		stdErr := errors.New("std")
		if MapToStatusCode(stdErr) != http.StatusInternalServerError {
			t.Errorf("Expected 500, got %d", MapToStatusCode(stdErr))
		}
	})
}
