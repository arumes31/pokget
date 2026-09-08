package source

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"pokget/internal/catalog"
)

type bulkTransport func(*http.Request) (*http.Response, error)

func (f bulkTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBulkBody struct {
	io.Reader
	closed bool
}

func (b *trackedBulkBody) Close() error { b.closed = true; return nil }

func TestScryfallDownloadFinishesBeforeImport(t *testing.T) {
	for _, mode := range []string{"complete", "oversized", "truncated", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			var data bytes.Buffer
			writer := gzip.NewWriter(&data)
			for range 2 {
				_, _ = fmt.Fprintln(writer, `{"id":"paper","name":"Lotus","lang":"en","set":"base","set_name":"Base","collector_number":"1","games":["paper"]}`)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			payload := data.Bytes()
			if mode == "truncated" {
				payload = payload[:len(payload)-8]
			}
			body := &trackedBulkBody{Reader: bytes.NewReader(payload)}
			client := &http.Client{Transport: bulkTransport(func(r *http.Request) (*http.Response, error) {
				var responseBody io.ReadCloser = body
				if r.URL.Path == "/manifest" {
					responseBody = io.NopCloser(strings.NewReader(`{"data":[{"type":"all_cards","updated_at":"v1","jsonl_download_uri":"https://bulk.example/cards"}]}`))
				}
				return &http.Response{StatusCode: 200, Body: responseBody, Header: make(http.Header)}, nil
			})}
			p := &ScryfallProvider{HTTP: HTTPOptions{Client: client}, ManifestURL: "https://bulk.example/manifest"}
			if mode == "oversized" {
				p.HTTP.MaxBodyBytes = int64(len(payload) - 1)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			count := 0
			_, err := p.Fetch(ctx, catalog.FetchRequest{}, func(catalog.CardRecord) error {
				if !body.closed {
					t.Error("database import is still holding the HTTP download open")
				}
				count++
				if mode == "cancelled" {
					cancel()
				}
				return nil
			})
			if mode == "complete" && (err != nil || count != 2) {
				t.Fatalf("count=%d err=%v", count, err)
			}
			if mode != "complete" && err == nil {
				t.Fatal("expected an explicit incomplete-download or cancellation error")
			}
			if mode == "oversized" && count != 0 {
				t.Fatal("oversized download was imported")
			}
			if mode == "cancelled" && count != 1 {
				t.Fatal("import ignored cancellation")
			}
		})
	}
}
