package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"pokget/internal/catalog"
)

const defaultMaxJSONBytes int64 = 512 << 20

type HTTPOptions struct {
	Client       *http.Client
	UserAgent    string
	MaxBodyBytes int64
	RequestDelay time.Duration
}

func (o HTTPOptions) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

func (o HTTPOptions) maxBodyBytes() int64 {
	if o.MaxBodyBytes > 0 {
		return o.MaxBodyBytes
	}
	return defaultMaxJSONBytes
}

func (o HTTPOptions) newRequest(ctx context.Context, method, url string, fetch catalog.FetchRequest) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog source: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(o.UserAgent) != "" {
		req.Header.Set("User-Agent", o.UserAgent)
	}
	if fetch.ETag != "" {
		req.Header.Set("If-None-Match", fetch.ETag)
	}
	if fetch.LastModified != "" {
		req.Header.Set("If-Modified-Since", fetch.LastModified)
	}
	return req, nil
}

type responseMeta struct {
	ETag         string
	LastModified string
	NotModified  bool
}

func getJSON(ctx context.Context, options HTTPOptions, url string, fetch catalog.FetchRequest, destination interface{}) (responseMeta, error) {
	req, err := options.newRequest(ctx, http.MethodGet, url, fetch)
	if err != nil {
		return responseMeta{}, err
	}
	resp, err := options.client().Do(req)
	if err != nil {
		return responseMeta{}, fmt.Errorf("catalog source: GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	meta := responseMeta{
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		NotModified:  resp.StatusCode == http.StatusNotModified,
	}
	if meta.NotModified {
		return meta, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return meta, fmt.Errorf("catalog source: GET %s returned %s", url, resp.Status)
	}

	limited := io.LimitReader(resp.Body, options.maxBodyBytes()+1)
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(destination); err != nil {
		return meta, fmt.Errorf("catalog source: decode %s: %w", url, err)
	}
	if decoder.More() {
		return meta, fmt.Errorf("catalog source: response from %s exceeds %d bytes", url, options.maxBodyBytes())
	}
	return meta, nil
}

func rawMetadata(value interface{}) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

func parseDate(value string) *time.Time {
	if value == "" {
		return nil
	}
	for _, layout := range []string{time.DateOnly, time.RFC3339, time.RFC3339Nano} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return &parsed
		}
	}
	return nil
}

func waitRequestDelay(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
