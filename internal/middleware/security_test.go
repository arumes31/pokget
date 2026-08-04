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
	"strings"
	"sync"
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

	// Validate Content-Security-Policy
	csp := rr.Header().Get("Content-Security-Policy")
	for _, directive := range []string{
		"default-src 'self'",
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
		"font-src 'self' https://fonts.gstatic.com",
	} {
		if !strings.Contains(csp, directive) {
			t.Errorf("Content-Security-Policy does not contain %q: %q", directive, csp)
		}
	}

	// Validate Referrer-Policy
	if got := rr.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("Expected Referrer-Policy: strict-origin-when-cross-origin, got %q", got)
	}

	// The scanner is a same-origin feature, so camera access must be available to
	// this application while remaining disabled for embedded cross-origin pages.
	if got := rr.Header().Get("Permissions-Policy"); !strings.Contains(got, "camera=(self)") {
		t.Errorf("Expected Permissions-Policy to contain \"camera=(self)\", got %q", got)
	}
}

func TestConcurrentLimitMiddlewareRejectsExcessWork(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	firstStatus := make(chan int, 1)
	handler := ConcurrentLimitMiddleware(1)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/scan", nil))
		firstStatus <- response.Code
	}()
	<-entered

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/scan", nil))
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", response.Code, http.StatusTooManyRequests)
	}
	if response.Header().Get("Retry-After") == "" {
		t.Fatal("busy response is missing Retry-After")
	}

	close(release)
	wg.Wait()
	if status := <-firstStatus; status != http.StatusNoContent {
		t.Fatalf("first status = %d, want %d", status, http.StatusNoContent)
	}
}
