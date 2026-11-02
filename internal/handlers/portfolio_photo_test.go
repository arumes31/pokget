package handlers

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"

	"pokget/internal/service"
)

func jpegFixture(payload string) []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00}, []byte(payload)...)
}

// storePhoto puts a photo in the store and returns its relative path.
func storePhoto(t *testing.T, store *service.PhotoStore, userID, itemID, payload string) string {
	t.Helper()
	token, err := store.Stage(userID, "token-"+itemID, jpegFixture(payload))
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	relative, err := store.Promote(userID, token, itemID)
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	return relative
}

func TestPortfolioPhotoServesTheCollectorsOwnPhoto(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	h.Photos = service.NewPhotoStore(t.TempDir())
	relative := storePhoto(t, h.Photos, "test-user", "item-1", "collector-photo")

	mock.ExpectQuery("SELECT COALESCE\\(photo_path").WithArgs("item-1", "test-user").
		WillReturnRows(sqlmock.NewRows([]string{"photo", "updated"}).AddRow(relative, time.Now().UTC()))

	request := mux.SetURLVars(authedRequest(t, http.MethodGet, "/portfolio/item-1/photo", ""), map[string]string{"id": "item-1"})
	recorder := httptest.NewRecorder()
	h.PortfolioPhoto(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("content type = %q", got)
	}
	if !strings.Contains(recorder.Body.String(), "collector-photo") {
		t.Fatal("the served photo is not the stored one")
	}
	if etag := recorder.Header().Get("ETag"); etag == "" {
		t.Fatal("expected an ETag for cache revalidation")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}


func TestPortfolioPhotoRevalidatesWithETag(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	h.Photos = service.NewPhotoStore(t.TempDir())
	relative := storePhoto(t, h.Photos, "test-user", "item-1", "collector-photo")
	updated := time.Unix(1700000000, 0).UTC()

	mock.ExpectQuery("SELECT COALESCE\\(photo_path").WithArgs("item-1", "test-user").
		WillReturnRows(sqlmock.NewRows([]string{"photo", "updated"}).AddRow(relative, updated))

	request := mux.SetURLVars(authedRequest(t, http.MethodGet, "/portfolio/item-1/photo", ""), map[string]string{"id": "item-1"})
	request.Header.Set("If-None-Match", `"1700000000"`)
	recorder := httptest.NewRecorder()
	h.PortfolioPhoto(recorder, request)

	if recorder.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", recorder.Code)
	}
	if recorder.Body.Len() != 0 {
		t.Fatal("a revalidated photo must not be resent")
	}
}

func TestPortfolioPhotoHidesOtherCollectorsPhotos(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	h.Photos = service.NewPhotoStore(t.TempDir())

	// The ownership check is part of the query, so a foreign item yields no row.
	mock.ExpectQuery("SELECT COALESCE\\(photo_path").WithArgs("item-9", "test-user").
		WillReturnError(sql.ErrNoRows)

	request := mux.SetURLVars(authedRequest(t, http.MethodGet, "/portfolio/item-9/photo", ""), map[string]string{"id": "item-9"})
	recorder := httptest.NewRecorder()
	h.PortfolioPhoto(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestPortfolioCardRendersPhotoAndBothMarkets(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	h.Templates = parseApplicationTemplates(t)
	h.Photos = service.NewPhotoStore(t.TempDir())

	rows := sqlmock.NewRows([]string{
		"id", "condition", "grade", "notes", "custom_price", "is_public", "photo", "binder",
		"cid", "name", "set", "set_code", "number", "language", "game", "rarity", "image", "usd", "eur",
	}).AddRow(
		"item-1", "NM", "", "stored flat", nil, false, "items/test-user/item-1.jpg", "",
		"swsh1-1", "Celebi V", "Sword & Shield", "swsh1", "1", "ja", "pokemon", "Holo Rare V",
		"https://example.test/celebi.png", 3.1, 2.55,
	)
	mock.ExpectQuery("SELECT p.id, p.condition").WithArgs("item-1", "test-user").WillReturnRows(rows)
	renderUserDataExpectation(mock, "test-user")

	request := mux.SetURLVars(authedRequest(t, http.MethodGet, "/portfolio/item-1", ""), map[string]string{"id": "item-1"})
	recorder := httptest.NewRecorder()
	h.PortfolioCard(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{"/portfolio/item-1/photo", "Celebi V", "2.55", "3.10", "stored flat"} {
		if !bytes.Contains([]byte(body), []byte(want)) {
			t.Fatalf("detail page misses %q", want)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPortfolioCardReturnsNotFoundForForeignItems(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	h.Templates = parseApplicationTemplates(t)
	mock.ExpectQuery("SELECT p.id, p.condition").WithArgs("item-9", "test-user").WillReturnError(sql.ErrNoRows)

	request := mux.SetURLVars(authedRequest(t, http.MethodGet, "/portfolio/item-9", ""), map[string]string{"id": "item-9"})
	recorder := httptest.NewRecorder()
	h.PortfolioCard(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestPortfolioPhotoURLStaysEmptyWithoutAPhoto(t *testing.T) {
	if got := portfolioPhotoURL("item-1", ""); got != "" {
		t.Fatalf("photo url = %q, want empty", got)
	}
	if got := portfolioPhotoURL("", "items/x.jpg"); got != "" {
		t.Fatalf("photo url = %q, want empty", got)
	}
	if got := portfolioPhotoURL("item-1", "items/x.jpg"); got != "/portfolio/item-1/photo" {
		t.Fatalf("photo url = %q", got)
	}
}

func TestPortfolioPhotoReportsMissingPhotos(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	h.Photos = service.NewPhotoStore(t.TempDir())

	mock.ExpectQuery("SELECT COALESCE\\(photo_path").WithArgs("item-1", "test-user").
		WillReturnRows(sqlmock.NewRows([]string{"photo", "updated"}).AddRow("", nil))

	request := mux.SetURLVars(authedRequest(t, http.MethodGet, "/portfolio/item-1/photo", ""), map[string]string{"id": "item-1"})
	recorder := httptest.NewRecorder()
	h.PortfolioPhoto(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}
