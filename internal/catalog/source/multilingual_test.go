package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pokget/internal/catalog"
)

func TestTCGdexMultilingualSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" {
			t.Error("a single-language validator must not suppress another language")
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) == 2 {
			_, _ = fmt.Fprint(w, `[{"id":"base"}]`)
		} else {
			_, _ = fmt.Fprintf(w, `{"id":"base","name":"Base","cards":[{"id":"base-1","localId":"1","name":%q}]}`, parts[0])
		}
	}))
	defer server.Close()
	p := &TCGdexProvider{HTTP: testHTTP(server), BaseURL: server.URL, Language: "de,en,ja,fr,zh-cn,zh-tw,ko,de"}
	var records []catalog.CardRecord
	result, err := p.Fetch(context.Background(), catalog.FetchRequest{ETag: "old-en"}, func(r catalog.CardRecord) error { records = append(records, r); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 7 || result.Count != 7 || !result.CompleteSnapshot || result.NotModified || result.ETag != "" {
		t.Fatalf("records=%+v result=%+v", records, result)
	}
	ids := make(map[string]bool)
	for _, record := range records {
		id, err := catalog.CardID(p.ID(), record.SourceCardID, record.Language)
		if err != nil || ids[id] {
			t.Fatalf("language identity collision: %+v %v", record, err)
		}
		ids[id] = true
	}
}

func TestTCGdexMultilingualFailureDoesNotCompleteSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/de/") {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/en/sets" {
			_, _ = fmt.Fprint(w, `[{"id":"base"}]`)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":"base","name":"Base","cards":[{"id":"base-1","localId":"1","name":"Example"}]}`)
	}))
	defer server.Close()
	p := &TCGdexProvider{HTTP: testHTTP(server), BaseURL: server.URL, Language: "en,de"}
	result, err := p.Fetch(context.Background(), catalog.FetchRequest{}, func(catalog.CardRecord) error { return nil })
	if err == nil || result.CompleteSnapshot {
		t.Fatalf("incomplete language import must fail: result=%+v err=%v", result, err)
	}
}

func TestLorcanaMultilingualSnapshotUsesPublishedLanguages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		language := strings.Split(strings.Trim(r.URL.Path, "/"), "/")[0]
		if language != "en" && language != "de" && language != "fr" {
			t.Errorf("unsupported language requested: %s", language)
		}
		if r.Header.Get("If-None-Match") != "" {
			t.Error("single-language validator reused")
		}
		_, _ = fmt.Fprintf(w, `{"metadata":{"language":%q,"formatVersion":"v1","generatedOn":"today"},"sets":{"1":{"name":"Base"}},"cards":[{"id":1,"fullName":"Example","number":1,"setCode":"1"}]}`, language)
	}))
	defer server.Close()
	p := &LorcanaJSONProvider{HTTP: testHTTP(server), BaseURL: server.URL, Language: DefaultLanguages}
	var languages []string
	result, err := p.Fetch(context.Background(), catalog.FetchRequest{ETag: "en", UpstreamVersion: "v1:today"}, func(r catalog.CardRecord) error { languages = append(languages, r.Language); return nil })
	if err != nil || result.Count != 3 || !result.CompleteSnapshot || result.NotModified || strings.Join(languages, ",") != "en,de,fr" {
		t.Fatalf("result=%+v languages=%v err=%v", result, languages, err)
	}
}
