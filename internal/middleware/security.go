package middleware

import (
	"net/http"
)

// SecurityHeadersMiddleware adds standard security headers to all responses.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent browsers from guessing the MIME type
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Prevent clickjacking by not allowing the site to be framed
		w.Header().Set("X-Frame-Options", "DENY")

		// Enable XSS filtering in browsers that support it
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		next.ServeHTTP(w, r)
	})
}
