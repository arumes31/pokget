package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPServerScanOutlivesWriteDeadline(t *testing.T) {
	for _, test := range []struct {
		name   string
		method string
		path   string
		wantOK bool
	}{
		{"scan", http.MethodPost, "/api/scan", true},
		{"binder naming", http.MethodPost, "/binders/auto-name", true},
		{"ordinary request", http.MethodPost, "/portfolio", false},
		{"scan wrong method", http.MethodGet, "/api/scan", false},
		{"binder naming wrong method", http.MethodGet, "/binders/auto-name", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := newHTTPServer(newTestConfig(t), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				time.Sleep(60 * time.Millisecond)
				_, _ = io.WriteString(w, "complete")
			}))
			server := httptest.NewUnstartedServer(app.Handler)
			server.Config.WriteTimeout = 10 * time.Millisecond
			server.Start()
			defer server.Close()
			request, err := http.NewRequest(test.method, server.URL+test.path, strings.NewReader("crop"))
			if err != nil {
				t.Fatal(err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				if test.wantOK {
					t.Fatalf("scan connection expired before completion: %v", err)
				}
				return
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if !test.wantOK {
				t.Fatal("ordinary request unexpectedly lost its write deadline")
			}
			if err != nil || string(body) != "complete" {
				t.Fatalf("scan result=%q error=%v", body, err)
			}
		})
	}
}
