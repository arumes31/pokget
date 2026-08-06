package source

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/PuerkitoBio/goquery"
	"pokget/internal/catalog"
)

func getDocument(ctx context.Context, options HTTPOptions, endpoint string, fetch catalog.FetchRequest) (*goquery.Document, responseMeta, error) {
	req, err := options.newRequest(ctx, http.MethodGet, endpoint, fetch)
	if err != nil {
		return nil, responseMeta{}, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := options.client().Do(req)
	if err != nil {
		return nil, responseMeta{}, fmt.Errorf("catalog source: GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	meta := responseMeta{
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		NotModified:  resp.StatusCode == http.StatusNotModified,
	}
	if meta.NotModified {
		return nil, meta, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return nil, meta, fmt.Errorf("catalog source: GET %s returned %s", endpoint, resp.Status)
	}
	document, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, options.maxBodyBytes()+1))
	if err != nil {
		return nil, meta, fmt.Errorf("catalog source: parse HTML %s: %w", endpoint, err)
	}
	return document, meta, nil
}
