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
	"fmt"
	"net/http"
	"runtime"
)

// AppError represents a domain-specific error with extra context.
type AppError struct {
	Code       int    // HTTP status code
	Message    string // User-facing message
	Inner      error  // Underlying error
	StackTrace string // Where it happened
}

func (e *AppError) Error() string {
	if e.Inner != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Inner)
	}
	return e.Message
}

// Wrap creates a new AppError with a stack trace.
func Wrap(code int, message string, err error) *AppError {
	stack := make([]byte, 1024)
	length := runtime.Stack(stack, false)
	return &AppError{
		Code:       code,
		Message:    message,
		Inner:      err,
		StackTrace: string(stack[:length]),
	}
}

// MapToStatusCode returns the HTTP status code for an error.
func MapToStatusCode(err error) int {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code
	}
	return http.StatusInternalServerError
}
