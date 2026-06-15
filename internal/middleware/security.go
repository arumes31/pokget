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
)

// SecurityHeadersMiddleware adds essential HTTP security headers to all responses
// to protect against common web vulnerabilities (XSS, clickjacking, MIME sniffing).
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent browsers from MIME-sniffing a response away from the declared content-type
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Prevent clickjacking by ensuring content cannot be embedded in a frame, iframe, object, or embed
		w.Header().Set("X-Frame-Options", "DENY")

		// Enable XSS filtering in browsers that support it (older browsers)
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		next.ServeHTTP(w, r)
	})
}

// BUG-L03 FIX: MaxBytesMiddleware limits the request body size to prevent
// denial-of-service attacks where an attacker sends extremely large request
// bodies to exhaust server memory. Previously, there was no limit on request
// body size. Now, requests exceeding maxBytes receive a 413 Request Entity Too
// Large response. Default is 1MB which is generous for form submissions.
const MaxBodyBytes int64 = 1 << 20 // 1 MB

func MaxBytesMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
		next.ServeHTTP(w, r)
	})
}
