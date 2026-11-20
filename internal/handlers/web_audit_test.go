package handlers

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"pokget/internal/auth"
	"pokget/internal/claudedetect"
	"pokget/internal/models"
	"pokget/internal/service"
)

// captureString records the string argument a statement was executed with.
type captureString struct{ value *string }

func (c captureString) Match(v driver.Value) bool {
	s, ok := v.(string)
	if ok {
		*c.value = s
	}
	return ok
}

func registerForm(email, password string) *http.Request {
	form := url.Values{"email": {email}, "password": {password}, "confirm_password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func confirmForm(token, password string) *http.Request {
	form := url.Values{"token": {token}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/auth/confirm", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// reRegister runs Register for an email that already has an unverified row
// whose last mail is old enough to send another one, and returns the stored hash.
func reRegister(t *testing.T, h *Handler, mock sqlmock.Sqlmock, email, password string) string {
	t.Helper()
	var stored string
	mock.ExpectQuery("SELECT is_verified, last_email_sent_at FROM users WHERE email").WithArgs(email).
		WillReturnRows(sqlmock.NewRows([]string{"is_verified", "last_email_sent_at"}).AddRow(false, time.Now().Add(-10*time.Minute)))
	mock.ExpectExec(`UPDATE users SET password_hash = \$1, verification_token = \$2`).
		WithArgs(captureString{&stored}, sqlmock.AnyArg(), email).
		WillReturnResult(sqlmock.NewResult(0, 1))
	rr := httptest.NewRecorder()
	h.Register(rr, registerForm(email, password))
	if rr.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want 201: %s", rr.Code, rr.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("re-registration did not replace the password and token: %v", err)
	}
	return stored
}

func expectConfirmLookup(mock sqlmock.Sqlmock, token, hash string) {
	mock.ExpectQuery("SELECT id, password_hash FROM users WHERE verification_token").
		WithArgs(hashVerificationToken(token)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "password_hash"}).AddRow("user-1", hash))
}

// Attacker pre-registers the victim's address, then the victim registers:
// the victim's password is the one the inbox owner can confirm.
func TestRegisterAfterPreRegistrationLetsOwnerConfirmOwnPassword(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	h.Mailer = &service.MockMailer{}

	reRegister(t, h, mock, "victim@example.com", "attacker-password")
	victimHash := reRegister(t, h, mock, "victim@example.com", "victim-password")

	expectConfirmLookup(mock, "inbox-token", victimHash)
	mock.ExpectExec("UPDATE users SET is_verified = TRUE").
		WithArgs("user-1", hashVerificationToken("inbox-token"), victimHash).
		WillReturnResult(sqlmock.NewResult(0, 1))
	rr := httptest.NewRecorder()
	h.ProcessConfirmEmail(rr, confirmForm("inbox-token", "victim-password"))
	if rr.Code != http.StatusOK {
		t.Fatalf("owner confirm status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Victim registers, then an attacker re-registers: confirming the inbox link
// with the victim's (now stale) password must not activate the attacker's one.
func TestConfirmRejectsPasswordThatIsNotTheStoredOne(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	h.Mailer = &service.MockMailer{}

	attackerHash := reRegister(t, h, mock, "victim@example.com", "attacker-password")

	expectConfirmLookup(mock, "inbox-token", attackerHash)
	rr := httptest.NewRecorder()
	h.ProcessConfirmEmail(rr, confirmForm("inbox-token", "victim-password"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("confirm with a different password status = %d, want 400", rr.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterAgainWithinResendThrottleReplacesOnlyPassword(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	sent := make(chan string, 1)
	h.Mailer = mailerFunc(func(to, _ string) error { sent <- to; return nil })

	mock.ExpectQuery("SELECT is_verified, last_email_sent_at FROM users WHERE email").WithArgs("user@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"is_verified", "last_email_sent_at"}).AddRow(false, time.Now().Add(-time.Minute)))
	mock.ExpectExec(`UPDATE users SET password_hash = \$1 WHERE email = \$2 AND is_verified = FALSE`).
		WithArgs(sqlmock.AnyArg(), "user@example.com").
		WillReturnResult(sqlmock.NewResult(0, 1))
	rr := httptest.NewRecorder()
	h.Register(rr, registerForm("user@example.com", "long-enough-pass"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rr.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sent:
		t.Fatal("a throttled re-registration sent another email")
	case <-time.After(100 * time.Millisecond):
	}
}

// An account confirmed between the lookup and the update must keep its password.
func TestRegisterAgainSkipsAccountVerifiedMeanwhile(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	sent := make(chan string, 1)
	h.Mailer = mailerFunc(func(to, _ string) error { sent <- to; return nil })

	mock.ExpectQuery("SELECT is_verified, last_email_sent_at FROM users WHERE email").WithArgs("user@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"is_verified", "last_email_sent_at"}).AddRow(false, nil))
	mock.ExpectExec(`UPDATE users SET password_hash = \$1, verification_token = \$2.*is_verified = FALSE`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "user@example.com").
		WillReturnResult(sqlmock.NewResult(0, 0))
	rr := httptest.NewRecorder()
	h.Register(rr, registerForm("user@example.com", "long-enough-pass"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rr.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sent:
		t.Fatal("mail sent for an account that was verified meanwhile")
	case <-time.After(100 * time.Millisecond):
	}
}

// The resend path only rotates the token: the password stays the registrant's,
// and confirming still needs it.
func TestResendKeepsPasswordAndConfirmStillNeedsIt(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	h.Mailer = &service.MockMailer{}

	mock.ExpectQuery("SELECT id, email, last_email_sent_at").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "last_email_sent_at", "is_verified"}).
			AddRow("user-1", "user@example.com", time.Now().Add(-10*time.Minute), false))
	mock.ExpectExec(`UPDATE users SET verification_token = \$1, verification_expires_at = NOW\(\) \+ INTERVAL '24 hours', last_email_sent_at = NOW\(\) WHERE id = \$2 AND is_verified = FALSE`).
		WithArgs(sqlmock.AnyArg(), "user-1").WillReturnResult(sqlmock.NewResult(0, 1))
	req := httptest.NewRequest(http.MethodPost, "/auth/resend", strings.NewReader("email=user@example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ResendVerification(httptest.NewRecorder(), req)

	expectConfirmLookup(mock, "new-token", fastPasswordHash(t, "registrant-password"))
	rr := httptest.NewRecorder()
	h.ProcessConfirmEmail(rr, confirmForm("new-token", ""))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("confirm without password status = %d, want 400", rr.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRenderShowsEuroSign(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	h.Templates = template.Must(template.New("x").Parse(`{{define "symbol.html"}}{{.CurrencySymbol}}{{end}}`))
	renderUserDataExpectation(mock, "test-user")

	rr := httptest.NewRecorder()
	h.render(rr, authedRequest(t, http.MethodGet, "/", ""), "symbol.html", nil)
	if got := rr.Body.String(); got != "€" {
		t.Fatalf("currency symbol = %q (% x), want €", got, got)
	}
}

func TestPortfolioCardInvalidUUIDIsNotFound(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	mock.ExpectQuery("SELECT p.id").WillReturnError(&pq.Error{Code: "22P02", Message: "invalid input syntax for type uuid"})

	rr := httptest.NewRecorder()
	h.PortfolioCard(rr, authedRequest(t, http.MethodGet, "/portfolio/not-a-uuid", ""))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestExecuteScanReturnsUncatalogedMatchWithoutPersisting(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	pipeline := service.NewDetectionPipeline(nil, nil)
	pipeline.Claude = stubScanDetector{} // reads Pikachu, which the catalog lacks
	h.Detection = pipeline
	h.MockCards = []models.Card{{ID: "sv1-004", Name: "Charmander", Game: "pokemon", Language: "en", SetCode: "SV1", CollectorNumber: "004"}}
	before := len(h.MockCards)

	rr := httptest.NewRecorder()
	h.executeScan(rr, scanRequest(t, testCardPNG(t), map[string]string{"game": "pokemon", "lang": "en"}))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["detected"] != "Pikachu" || resp["id"] != "" {
		t.Fatalf("detected=%v id=%v, want Pikachu without an id", resp["detected"], resp["id"])
	}
	if resp["set"] != "SV1" {
		t.Fatalf("set = %v, want the read set code so adding keeps it", resp["set"])
	}
	if len(h.MockCards) != before {
		t.Fatal("a scan added a card to the shared cache")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPersistSyntheticCardStaysOutOfSharedCatalog(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	mock.ExpectExec("INSERT INTO catalog_sources").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO cards .*'claude', \$1, false, '\{\}'\)\s+ON CONFLICT \(id\) DO NOTHING`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	before := len(h.MockCards)

	h.persistSyntheticCard(context.Background(), models.Card{ID: "card_x", Name: "Mew", Set: "Promo"})
	if len(h.MockCards) != before {
		t.Fatal("a user-added card entered the scan cache")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReloadCardsOnlyExcludesUserAddedCards(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	mock.ExpectQuery(`FROM cards WHERE .*source_id IS DISTINCT FROM 'claude'`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := h.ReloadCardsOnly(); err != nil {
		t.Fatalf("reload query does not exclude user-added cards: %v", err)
	}
}

func TestExecuteScanRequiresGame(t *testing.T) {
	pipeline := service.NewDetectionPipeline(nil, nil)
	pipeline.Claude = stubScanDetector{}
	h := &Handler{MockCards: []models.Card{{ID: "card-1", Name: "Pikachu", Game: "pokemon", Language: "en"}}, Detection: pipeline}

	rr := httptest.NewRecorder()
	h.executeScan(rr, scanRequest(t, testCardPNG(t), map[string]string{"lang": "en"}))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("scan without game status = %d, want 400", rr.Code)
	}
}

// blockingDetector holds the detector until released or the scan context ends.
type blockingDetector struct {
	stubScanDetector
	entered chan struct{}
	release chan struct{}
}

func (d blockingDetector) Detect(ctx context.Context, img []byte, lang, game string) (*claudedetect.DetectedCardInfo, error) {
	select {
	case d.entered <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-d.release:
	case <-time.After(3 * time.Second):
	}
	return d.stubScanDetector.Detect(ctx, img, lang, game)
}

func (d blockingDetector) DetectImages(ctx context.Context, _ [][]byte, lang, game string) (*claudedetect.DetectedCardInfo, error) {
	return d.Detect(ctx, nil, lang, game)
}

func TestExecuteScanTimesOutWithPrimaryProviderConfigured(t *testing.T) {
	pipeline := service.NewDetectionPipeline(nil, &service.LLMService{PrimaryBaseURL: "https://primary.example/v1"})
	pipeline.Claude = blockingDetector{entered: make(chan struct{}, 1), release: make(chan struct{})}
	h := &Handler{
		MockCards:   []models.Card{{ID: "card-1", Name: "Pikachu", Game: "pokemon", Language: "en"}},
		Detection:   pipeline,
		ScanTimeout: 100 * time.Millisecond,
	}
	started := time.Now()
	rr := httptest.NewRecorder()
	h.executeScan(rr, scanRequest(t, testCardPNG(t), map[string]string{"game": "pokemon", "lang": "en"}))
	if elapsed := time.Since(started); elapsed > 2*time.Second || rr.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d after %v, want 408 within the scan timeout", rr.Code, elapsed)
	}
}

func TestAPIScanAllowsOneInFlightScanPerUser(t *testing.T) {
	detector := blockingDetector{entered: make(chan struct{}, 1), release: make(chan struct{})}
	pipeline := service.NewDetectionPipeline(nil, nil)
	pipeline.Claude = detector
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	mock.MatchExpectationsInOrder(false)
	for range 2 {
		mock.ExpectQuery("SELECT currency FROM users").WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("EUR"))
	}
	h.Detection = pipeline
	h.MockCards = []models.Card{{ID: "card-1", Name: "Pikachu", Game: "pokemon", Language: "en"}}
	userRequest := func() *http.Request {
		req := scanRequest(t, testCardPNG(t), map[string]string{"game": "pokemon", "lang": "en"})
		return req.WithContext(context.WithValue(req.Context(), auth.UserContextKey{}, "user-1"))
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.APIScan(httptest.NewRecorder(), userRequest())
	}()
	defer func() { close(detector.release); wg.Wait() }()
	select {
	case <-detector.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first scan never reached the detector")
	}

	second := make(chan int, 1)
	go func() {
		rr := httptest.NewRecorder()
		h.APIScan(rr, userRequest())
		second <- rr.Code
	}()
	select {
	case code := <-second:
		if code != http.StatusTooManyRequests {
			t.Fatalf("second concurrent scan status = %d, want 429", code)
		}
	case <-time.After(time.Second):
		t.Fatal("second concurrent scan of the same user was not rejected")
	}
}

func TestFreeTextFieldsAreBounded(t *testing.T) {
	long := strings.Repeat("x", 2001)
	for name, call := range map[string]func(*Handler, http.ResponseWriter){
		"misprint description": func(h *Handler, w http.ResponseWriter) {
			h.SubmitError(w, authedRequest(t, http.MethodPost, "/errors/submit", url.Values{"card_id": {"c"}, "error_type": {"Miscut"}, "description": {long}, "multiplier": {"2"}}.Encode()))
		},
		"misprint type": func(h *Handler, w http.ResponseWriter) {
			h.SubmitError(w, authedRequest(t, http.MethodPost, "/errors/submit", url.Values{"card_id": {"c"}, "error_type": {strings.Repeat("x", 65)}, "description": {"d"}, "multiplier": {"2"}}.Encode()))
		},
		"portfolio notes": func(h *Handler, w http.ResponseWriter) {
			h.EditPortfolioItem(w, authedRequest(t, http.MethodPost, "/portfolio/edit", url.Values{"item_id": {"i"}, "notes": {long}}.Encode()))
		},
		"portfolio grade": func(h *Handler, w http.ResponseWriter) {
			h.EditPortfolioItem(w, authedRequest(t, http.MethodPost, "/portfolio/edit", url.Values{"item_id": {"i"}, "grade": {strings.Repeat("9", 33)}}.Encode()))
		},
		"add notes": func(h *Handler, w http.ResponseWriter) {
			h.AddCardToPortfolio(w, authedRequest(t, http.MethodPost, "/portfolio/add", url.Values{"card_id": {"c"}, "notes": {long}}.Encode()))
		},
		"binder name": func(h *Handler, w http.ResponseWriter) {
			h.CreateBinder(w, authedRequest(t, http.MethodPost, "/binders/create", url.Values{"name": {strings.Repeat("x", 101)}}.Encode()))
		},
		"binder description": func(h *Handler, w http.ResponseWriter) {
			h.CreateBinder(w, authedRequest(t, http.MethodPost, "/binders/create", url.Values{"name": {"n"}, "description": {long}}.Encode()))
		},
		"wantlist notes": func(h *Handler, w http.ResponseWriter) {
			h.AddToWantlist(w, authedRequest(t, http.MethodPost, "/wantlist/add", url.Values{"card_id": {"c"}, "notes": {long}}.Encode()))
		},
		"wantlist edit notes": func(h *Handler, w http.ResponseWriter) {
			h.UpdateWantlistItem(w, authedRequest(t, http.MethodPost, "/wantlist/edit", url.Values{"item_id": {"i"}, "target_price": {"1"}, "notes": {long}}.Encode()))
		},
	} {
		t.Run(name, func(t *testing.T) {
			h, mock, cleanup := setupTestHandler(t)
			defer cleanup()
			rr := httptest.NewRecorder()
			call(h, rr)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rr.Code)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A reload that started later must win even when an earlier one finishes last.
func TestReloadCardsOnlyPublishesNewestSnapshot(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	mock.MatchExpectationsInOrder(false)
	columns := []string{"id", "name", "set_name", "price_usd", "price_eur", "image_url", "variant", "change_24h", "phash",
		"game", "language", "rarity", "set_code", "collector_number", "catalog_active", "printed_name", "source_card_id", "set_total"}
	row := func(id string) *sqlmock.Rows {
		return sqlmock.NewRows(columns).AddRow(id, id, "s", 0, 0, "", "", 0, nil, "pokemon", "en", "", "", "", true, "", "", 0)
	}
	mock.ExpectQuery("SELECT id, name").WillDelayFor(300 * time.Millisecond).WillReturnRows(row("old"))
	mock.ExpectQuery("SELECT id, name").WillReturnRows(row("new"))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = h.ReloadCardsOnly() }()
	time.Sleep(50 * time.Millisecond)
	go func() { defer wg.Done(); _, _ = h.ReloadCardsOnly() }()
	wg.Wait()
	if len(h.MockCards) != 1 || h.MockCards[0].ID != "new" {
		t.Fatalf("cache = %+v, want the newest snapshot", h.MockCards)
	}
}

func TestDeletePortfolioItemRevokesAddXP(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	mock.ExpectQuery("SELECT COALESCE\\(photo_path").WillReturnRows(sqlmock.NewRows([]string{"p"}).AddRow(""))
	mock.ExpectExec("DELETE FROM portfolio").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE users SET xp = GREATEST\(xp \+ \$1, 0\)`).WithArgs(-100, "test-user").
		WillReturnRows(sqlmock.NewRows([]string{"xp"}).AddRow(0))
	mock.ExpectExec("UPDATE users SET rank_title").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(0, 1))

	rr := httptest.NewRecorder()
	h.DeletePortfolioItem(rr, authedRequest(t, http.MethodPost, "/portfolio/delete", "item_id=item-1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	h.Audit.Close()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("deleting a card kept the XP its add granted: %v", err)
	}
}

func TestAPICardPriceRecordsCheckAndSkipsCachedWrites(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	mock.MatchExpectationsInOrder(false)
	h.Prices = service.NewPriceService(map[string][]service.PriceClient{
		"pokemon": {&service.MockPriceClient{FixedEUR: 12.5, FixedUSD: 14}},
	})
	for range 2 {
		mock.ExpectQuery("SELECT id, name").WithArgs("card-1").
			WillReturnRows(priceCardRow().AddRow("card-1", "Charizard", "Base Set", "BS", "4", "pokemon", "en", "Rare", "normal", "tcgdex", "base1-4", 0, 0))
		mock.ExpectQuery("SELECT currency FROM users").WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("EUR"))
	}
	mock.ExpectExec(`price_eur_estimated = CASE WHEN quote.eur > 0 THEN FALSE .*price_checked_at = NOW\(\)`).WillReturnResult(sqlmock.NewResult(0, 1))
	for range 2 {
		h.APICardPrice(httptest.NewRecorder(), authedRequest(t, http.MethodGet, "/api/cards/price?id=card-1", ""))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("fresh quote did not record the check: %v", err)
	}
	// A sentinel the cached lookup must not consume.
	mock.ExpectQuery("SELECT id, name").WithArgs("card-1").
		WillReturnRows(priceCardRow().AddRow("card-1", "Charizard", "Base Set", "BS", "4", "pokemon", "en", "Rare", "normal", "tcgdex", "base1-4", 0, 0))
	mock.ExpectQuery("SELECT currency FROM users").WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("EUR"))
	mock.ExpectExec("UPDATE cards").WillReturnResult(sqlmock.NewResult(0, 1))
	h.APICardPrice(httptest.NewRecorder(), authedRequest(t, http.MethodGet, "/api/cards/price?id=card-1", ""))
	if mock.ExpectationsWereMet() == nil {
		t.Fatal("a cached quote was written to the database again")
	}
}
