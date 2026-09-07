package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/PuerkitoBio/goquery"
)

func TestErrorDatabasePaginatesAndFilters(t *testing.T) {
	for _, tc := range []struct {
		name, query, pattern, kind string
		page, offset, rows, shown  int
		previous, next             bool
	}{
		{name: "first page", page: 1, rows: 25, shown: 24, next: true},
		{
			name: "escaped search on second page", query: "?page=2&q=100%25_%21&type=ink",
			pattern: "%100!%!_!!%", kind: "%ink%", page: 2, offset: 24, rows: 25,
			shown: 24, previous: true, next: true,
		},
		{name: "last page", query: "?page=3&type=holo", kind: "%holo%", page: 3, offset: 48, rows: 2, shown: 2, previous: true},
		{name: "no matches", query: "?q=missing&type=miscut", pattern: "%missing%", kind: "%miscut%", page: 1},
		{name: "negative page", query: "?page=-2", page: 1, rows: 1, shown: 1},
		{name: "overflow page", query: "?page=9223372036854775807", page: 1, rows: 1, shown: 1},
		{name: "unknown type", query: "?type=not-a-filter", page: 1, rows: 1, shown: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			rows := sqlmock.NewRows([]string{"id", "card_id", "type", "description", "multiplier", "name", "set", "image", "game"})
			for i := 0; i < tc.rows; i++ {
				rows.AddRow(fmt.Sprintf("report-%d", i), "card-1", "Ink error", "A detailed description", 1.5,
					fmt.Sprintf("Card %d", i), "Base", "/cards/pikachu.png", "Pokemon")
			}
			mock.ExpectQuery(`SELECT .* FROM error_cards e JOIN cards c ON e.card_id = c.id WHERE .*CONCAT_WS.*c.name.*c.set_name.*e.error_type.*e.description.*ILIKE \$1 ESCAPE '!'.*e.error_type ILIKE \$2.*ORDER BY e.created_at DESC, e.id DESC LIMIT \$3 OFFSET \$4`).
				WithArgs(tc.pattern, tc.kind, 25, tc.offset).WillReturnRows(rows).RowsWillBeClosed()
			handler := &Handler{DB: database, Templates: parseApplicationTemplates(t)}
			request := httptest.NewRequest(http.MethodGet, "/errors"+tc.query, nil)
			request.Header.Set("HX-Request", "true")
			response := httptest.NewRecorder()
			handler.ErrorDatabase(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
			}
			doc, err := goquery.NewDocumentFromReader(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if got := doc.Find("[data-misprint]").Length(); got != tc.shown {
				t.Errorf("visible report count = %d, want %d", got, tc.shown)
			}
			doc.Find("[data-misprint] img").Each(func(_ int, image *goquery.Selection) {
				if image.AttrOr("loading", "") != "lazy" || image.AttrOr("decoding", "") != "async" {
					t.Error("report images must load lazily and decode asynchronously")
				}
			})
			for _, link := range []struct {
				rel     string
				present bool
				page    int
			}{
				{rel: "prev", present: tc.previous, page: tc.page - 1},
				{rel: "next", present: tc.next, page: tc.page + 1},
			} {
				anchor := doc.Find(`a[rel="` + link.rel + `"]`)
				if (anchor.Length() == 1) != link.present {
					t.Errorf("%s link count = %d, want present %t", link.rel, anchor.Length(), link.present)
				}
				if !link.present {
					continue
				}
				address, err := url.Parse(anchor.AttrOr("href", ""))
				if err != nil {
					t.Fatal(err)
				}
				if address.Path != "/errors" || address.Query().Get("page") != fmt.Sprint(link.page) {
					t.Errorf("%s page URL = %q", link.rel, address)
				}
				if got := address.Query().Get("q"); got != request.URL.Query().Get("q") {
					t.Errorf("%s link query = %q, want preserved search", link.rel, got)
				}
				if tc.kind != "" && address.Query().Get("type") != request.URL.Query().Get("type") {
					t.Errorf("%s link does not preserve error type", link.rel)
				}
			}
			if tc.name == "no matches" && !strings.Contains(doc.Text(), "No matches") {
				t.Error("filtered empty response must explain that no reports match")
			}
			if got := doc.Find(`#misprint-search input[name="q"]`).AttrOr("value", ""); got != request.URL.Query().Get("q") {
				t.Errorf("search field value = %q, want retained query", got)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestErrorDatabaseRejectsExcessiveSearch(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/errors?q="+url.QueryEscape(strings.Repeat("界", 201)), nil)
	response := httptest.NewRecorder()
	(&Handler{}).ErrorDatabase(response, request)
	if response.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", response.Code)
	}
}
