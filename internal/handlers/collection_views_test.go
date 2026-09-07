package handlers

import (
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pokget/internal/auth"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/PuerkitoBio/goquery"
	"github.com/gorilla/mux"
)

func TestIndexLoadsOnlyShellMetadata(t *testing.T) {
	for _, tc := range []struct{ target, path string }{
		{"/?view=home&page=3", "/dashboard?page=3"},
		{"/?view=binders&binder=b1&page=2", "/binders/b1?page=2"},
	} {
		t.Run(tc.target, func(t *testing.T) {
			h, mock, cleanup := setupTestHandler(t)
			defer cleanup()
			h.Templates = template.Must(template.New("index.html").Parse(`{{ .InitialPath }}|{{ .Currency }}`))
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			session, _ := auth.Store.Get(req, "session")
			session.Values["user_id"] = "test-user"
			cookieResponse := httptest.NewRecorder()
			if err := session.Save(req, cookieResponse); err != nil {
				t.Fatal(err)
			}
			for _, cookie := range cookieResponse.Result().Cookies() {
				req.AddCookie(cookie)
			}
			mock.ExpectQuery("SELECT currency").WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("USD"))
			rr := httptest.NewRecorder()
			h.Index(rr, req)
			if rr.Code != http.StatusOK || rr.Body.String() != tc.path+"|USD" {
				t.Fatalf("shell response = %d %q", rr.Code, rr.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBinderCollectionPagesAndPriceAvailability(t *testing.T) {
	for _, tc := range []struct {
		page           string
		offset         int
		shown          int
		previous, next bool
	}{
		{"1", 0, 24, false, true}, {"2", 24, 3, true, false},
		{"999999999999999999999999", 0, 24, false, true}, {"999", 24, 3, true, false},
	} {
		t.Run(tc.page, func(t *testing.T) {
			h, mock, cleanup := setupTestHandler(t)
			defer cleanup()
			h.Templates = parseApplicationTemplates(t)
			mock.ExpectQuery("SELECT id, name, COALESCE").WithArgs("b1", "test-user").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "description"}).AddRow("b1", "Main", ""))
			mock.ExpectQuery(`SELECT COUNT\(\*\).*WHERE p.binder_id = \$1 AND p.user_id = \$2`).WithArgs("b1", "test-user").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(27))
			rows := sqlmock.NewRows([]string{"id", "condition", "custom", "cid", "name", "set", "image", "usd", "eur", "game"})
			count := tc.shown
			if tc.next {
				count++
			}
			for i := 0; i < count; i++ {
				var custom interface{}
				if i == 1 {
					custom = 0.0
				}
				rows.AddRow(fmt.Sprintf("p%d", i), "NM", custom, fmt.Sprintf("c%d", i), fmt.Sprintf("Complete card name %d", i), "Set", "image.png", nil, nil, "Pokemon")
			}
			mock.ExpectQuery(`SELECT p.id, p.condition.*c.price_usd, c.price_eur.*WHERE p.binder_id = \$1 AND p.user_id = \$2.*ORDER BY p.added_at DESC, p.id DESC LIMIT \$3 OFFSET \$4`).WithArgs("b1", "test-user", 25, tc.offset).WillReturnRows(rows)
			renderUserDataExpectation(mock, "test-user")
			rr := httptest.NewRecorder()
			h.BinderDetail(rr, mux.SetURLVars(authedRequest(t, http.MethodGet, "/binders/b1?page="+tc.page, ""), map[string]string{"id": "b1"}))
			if rr.Code != http.StatusOK {
				t.Fatalf("response = %d %s", rr.Code, rr.Body.String())
			}
			doc, err := goquery.NewDocumentFromReader(rr.Body)
			if err != nil {
				t.Fatal(err)
			}
			if got := doc.Find("h3").Length(); got != tc.shown {
				t.Errorf("displayed cards = %d, want %d", got, tc.shown)
			}
			if !strings.Contains(doc.Text(), "27 cards") {
				t.Error("overall count missing")
			}
			if got := strings.Count(doc.Text(), "Price unavailable"); got != tc.shown-1 {
				t.Errorf("missing prices = %d, want %d (custom zero must remain valued)", got, tc.shown-1)
			}
			if !strings.Contains(doc.Text(), "0.00") {
				t.Error("explicit custom zero missing")
			}
			if (doc.Find(`a[rel=prev]`).Length() > 0) != tc.previous || (doc.Find(`a[rel=next]`).Length() > 0) != tc.next {
				t.Error("wrong pagination links")
			}
			doc.Find("h3").Each(func(_ int, name *goquery.Selection) {
				if name.HasClass("truncate") {
					t.Error("card name still truncated")
				}
			})
			if tc.next {
				link := doc.Find(`a[rel=next]`)
				if href, _ := link.Attr("href"); href != "/?binder=b1&page=2&view=binders" {
					t.Errorf("next href = %q", href)
				}
				if fragment, _ := link.Attr("hx-get"); fragment != "/binders/b1?page=2" {
					t.Errorf("next fragment = %q", fragment)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWantlistMissingPriceDoesNotClaimZeroProgress(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	h.Templates = parseApplicationTemplates(t)
	mock.ExpectQuery(`SELECT w.id.*c.price_usd, c.price_eur`).WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"id", "cid", "target", "notes", "name", "set", "usd", "eur", "image"}).AddRow("w1", "c1", 20.0, "", "Unpriced card", "Set", nil, nil, ""))
	renderUserDataExpectation(mock, "test-user")
	rr := httptest.NewRecorder()
	h.Wantlist(rr, authedRequest(t, http.MethodGet, "/wantlist", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("response = %d %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Price unavailable") || strings.Contains(body, "Market / target") {
		t.Fatal("missing market price must be disclosed without a fabricated ratio")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDashboardUsesOverallAggregateAndBoundedCards(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	h.Templates = parseApplicationTemplates(t)
	mock.ExpectQuery("SELECT currency").WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("EUR"))
	mock.ExpectQuery("SELECT c.set_name").WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"set", "owned", "total"}))
	mock.ExpectQuery("SELECT condition_multipliers").WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"multipliers", "currency"}).AddRow(`{"LP":0.75}`, "EUR"))
	mock.ExpectQuery(`SELECT COUNT\(\*\).*COUNT\(COALESCE\(p.custom_price.*SUM\(CASE WHEN p.custom_price IS NOT NULL.*WHERE p.user_id = \$1`).WithArgs("test-user", `{"LP":0.75}`, "EUR").WillReturnRows(sqlmock.NewRows([]string{"total", "priced", "value"}).AddRow(1000, 999, 14567.89))
	rows := sqlmock.NewRows([]string{"id", "condition", "custom", "notes", "grade", "public", "binder", "cid", "name", "set", "image", "usd", "eur", "game"})
	for i := 0; i < 25; i++ {
		rows.AddRow(fmt.Sprintf("p%d", i), "LP", nil, "", "", false, "b1", fmt.Sprintf("c%d", i), "Full readable card name", "Set", "", nil, nil, "Pokemon")
	}
	mock.ExpectQuery(`SELECT p.id.*c.price_usd, c.price_eur.*WHERE p.user_id = \$1.*ORDER BY p.added_at DESC, p.id DESC LIMIT \$2 OFFSET \$3`).WithArgs("test-user", 25, 24).WillReturnRows(rows)
	mock.ExpectQuery("SELECT xp, rank_title").WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"xp", "rank"}).AddRow(0, "Novice"))
	mock.ExpectQuery("SELECT COUNT").WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery("SELECT valuation").WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"value"}))
	renderUserDataExpectation(mock, "test-user")
	rr := httptest.NewRecorder()
	h.Dashboard(rr, authedRequest(t, http.MethodGet, "/dashboard?page=2", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("response = %d %s", rr.Code, rr.Body.String())
	}
	doc, err := goquery.NewDocumentFromReader(rr.Body)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Find("[data-portfolio-item]").Length() != 24 {
		t.Error("dashboard did not bound visible cards to 24")
	}
	if !strings.Contains(doc.Find(".collection-summary").Text(), "1000") || !strings.Contains(doc.Text(), "14567.89") {
		t.Error("summary must use whole collection aggregate")
	}
	if strings.Count(doc.Text(), "Price unavailable") != 24 {
		t.Error("catalog NULL prices rendered as zero")
	}
	if got, _ := doc.Find(`a[rel=next]`).Attr("hx-get"); got != "/dashboard?page=3" {
		t.Errorf("next page = %q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicVaultPagesOnlySharedCards(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	h.Templates = parseApplicationTemplates(t)
	mock.ExpectQuery("SELECT id, email").WithArgs("shared").WillReturnRows(sqlmock.NewRows([]string{"id", "email", "rank", "xp", "currency"}).AddRow("owner", "name@example.com", "Novice", 0, "EUR"))
	mock.ExpectQuery(`SELECT COUNT\(\*\).*WHERE p.user_id = \$1 AND p.is_public = TRUE`).WithArgs("owner").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(25))
	rows := sqlmock.NewRows([]string{"id", "condition", "format", "grade", "grading", "notes", "name", "set", "usd", "eur", "image", "game"}).AddRow("p25", "NM", "raw", nil, nil, nil, "A completely readable long card name", "Set", nil, nil, "", "Pokemon")
	mock.ExpectQuery(`SELECT p.id.*c.price_usd, c.price_eur.*WHERE p.user_id = \$1 AND p.is_public = TRUE.*ORDER BY p.added_at DESC, p.id DESC LIMIT \$2 OFFSET \$3`).WithArgs("owner", 25, 24).WillReturnRows(rows)
	rr := httptest.NewRecorder()
	h.PublicVault(rr, mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/vault/shared?page=2", nil), map[string]string{"slug": "shared"}))
	if rr.Code != http.StatusOK {
		t.Fatalf("response = %d %s", rr.Code, rr.Body.String())
	}
	doc, err := goquery.NewDocumentFromReader(rr.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Text(), "25 shared cards") || !strings.Contains(doc.Text(), "Price unavailable") {
		t.Error("public count or price availability missing")
	}
	if doc.Find("h3.truncate").Length() != 0 {
		t.Error("public card name truncated")
	}
	if got, _ := doc.Find(`a[rel=prev]`).Attr("href"); got != "/vault/shared?page=1" {
		t.Errorf("previous page = %q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
