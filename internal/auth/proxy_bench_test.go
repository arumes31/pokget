package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkProxyMiddlewareSplit(b *testing.B) {
	nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {})
	middleware := ProxyMiddleware(nextHandler)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "192.168.1.1, 10.0.0.1")
	rr := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		middleware.ServeHTTP(rr, req)
	}
}
