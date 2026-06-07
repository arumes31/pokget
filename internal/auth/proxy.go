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

package auth

import (
	"net/http"
	"os"
	"strings"
)

// ProxyMiddleware handles reverse proxy and Cloudflare headers to extract the real client IP.
// Controllable via TRUST_PROXY and TRUST_CLOUDFLARE environment variables.
func ProxyMiddleware(next http.Handler) http.Handler {
	// Enabled by default unless explicitly set to "false"
	trustProxy := os.Getenv("TRUST_PROXY") != "false"
	trustCF := os.Getenv("TRUST_CLOUDFLARE") != "false"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var realIP string

		if trustCF {
			// Cloudflare specific header
			realIP = r.Header.Get("CF-Connecting-IP")
		}

		if realIP == "" && trustProxy {
			// Standard reverse proxy headers
			if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
				realIP = xrip
			} else if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				// X-Forwarded-For can be a comma-separated list; the first one is the original client
				// BOLT OPTIMIZATION: Use strings.IndexByte to find the first comma without allocating a slice of strings
				if idx := strings.IndexByte(xff, ','); idx != -1 {
					realIP = strings.TrimSpace(xff[:idx])
				} else {
					realIP = strings.TrimSpace(xff)
				}
			}
		}

		// If we found a real IP, we update r.RemoteAddr
		// Note: RemoteAddr usually contains the port, but we'll store the IP here for simplicity
		// as many internal logs/checks only look at this field.
		if realIP != "" {
			r.RemoteAddr = realIP
		}

		next.ServeHTTP(w, r)
	})
}
