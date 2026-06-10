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

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	// Dummy handler that will be wrapped by the middleware
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	middleware := SecurityHeadersMiddleware(nextHandler)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	middleware.ServeHTTP(rr, req)

	// Validate X-Content-Type-Options
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("Expected X-Content-Type-Options: nosniff, got %q", got)
	}

	// Validate X-Frame-Options
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("Expected X-Frame-Options: DENY, got %q", got)
	}

	// Validate X-XSS-Protection
	if got := rr.Header().Get("X-XSS-Protection"); got != "1; mode=block" {
		t.Errorf("Expected X-XSS-Protection: 1; mode=block, got %q", got)
	}
}
