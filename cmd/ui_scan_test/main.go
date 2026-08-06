package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	defaultBaseURL  = "http://localhost:18066"
	defaultEmail    = "pokget-ui-smoke@example.invalid"
	defaultPassword = "PokgetSmoke!2026"
	defaultTimeout  = 2 * time.Minute

	loginEmailSelector       = `#login-email`
	loginPasswordSelector    = `#login-password`
	loginSubmitSelector      = `form[hx-post="/auth/login"] button[type="submit"]`
	registerEmailSelector    = `#register-email`
	registerPasswordSelector = `#register-password`
	registerConfirmSelector  = `#register-confirm-password`
	registerSubmitSelector   = `form[hx-post="/auth/register"] button[type="submit"]`
	scanNavigationSelector   = `button[hx-get="/centering"]`
	fileInputSelector        = `#main-content input[type="file"]`
	scanUploadSelector       = `[data-testid="scan-selected-crop"]`
	detectedCardIDSelector   = `[data-testid="detected-card-id"]`
)

type config struct {
	baseURL           string
	email             string
	password          string
	fixture           string
	expectedID        string
	expectedName      string
	game              string
	language          string
	verificationToken string
	chromePath        string
	timeout           time.Duration
	headless          bool
	register          bool
}

type authState struct {
	Path          string `json:"path"`
	LoginError    string `json:"loginError"`
	RegisterError string `json:"registerError"`
	IsLoggedIn    bool   `json:"isLoggedIn"`
	IsRegistered  bool   `json:"isRegistered"`
}

type scanResult struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	StateID   string `json:"stateID"`
	StateName string `json:"stateName"`
	Visible   bool   `json:"visible"`
}

func main() {
	cfg, err := parseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "ui scan smoke configuration: %v\n", err)
		os.Exit(2)
	}

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "ui scan smoke failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("ui scan smoke passed: id=%q name=%q\n", cfg.expectedID, cfg.expectedName)
}

func parseFlags(args []string, output io.Writer) (config, error) {
	cfg := config{}
	flags := flag.NewFlagSet("ui_scan_test", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&cfg.baseURL, "base-url", defaultBaseURL, "Pokget base URL")
	flags.StringVar(&cfg.email, "email", defaultEmail, "test account email")
	flags.StringVar(&cfg.password, "password", defaultPassword, "test account password")
	flags.StringVar(&cfg.fixture, "fixture", "", "absolute or relative card image path")
	flags.StringVar(&cfg.expectedID, "expected-id", "", "exact expected card ID")
	flags.StringVar(&cfg.expectedName, "expected-name", "", "exact expected card name")
	flags.StringVar(&cfg.game, "game", "pokemon", "TCG selected in the scanner UI")
	flags.StringVar(&cfg.language, "lang", "eng", "Tesseract language code used by the scanner UI")
	flags.StringVar(
		&cfg.verificationToken,
		"verification-token",
		"",
		"optional email-verification token for a pre-registered account",
	)
	flags.StringVar(&cfg.chromePath, "chrome-path", "", "optional Chrome or Chromium executable path")
	flags.DurationVar(&cfg.timeout, "timeout", defaultTimeout, "overall browser smoke timeout")
	flags.BoolVar(&cfg.headless, "headless", true, "run Chrome in headless mode")
	flags.BoolVar(&cfg.register, "register", true, "register the account if login fails")

	if err := flags.Parse(args); err != nil {
		return config{}, fmt.Errorf("parsing flags: %w", err)
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %q", flags.Args())
	}
	if err := validateConfig(&cfg); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func validateConfig(cfg *config) error {
	baseURL, err := url.ParseRequestURI(strings.TrimSpace(cfg.baseURL))
	if err != nil {
		return fmt.Errorf("invalid base url: %w", err)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return fmt.Errorf("base url scheme must be http or https")
	}
	if baseURL.Host == "" {
		return fmt.Errorf("base url must include a host")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	baseURL.RawQuery = ""
	baseURL.Fragment = ""
	cfg.baseURL = strings.TrimRight(baseURL.String(), "/")

	address, err := mail.ParseAddress(strings.TrimSpace(cfg.email))
	if err != nil {
		return fmt.Errorf("invalid email: %w", err)
	}
	cfg.email = strings.TrimSpace(cfg.email)
	if address.Address != cfg.email {
		return fmt.Errorf("email must not include a display name")
	}
	if cfg.password == "" {
		return fmt.Errorf("password must not be empty")
	}
	if cfg.expectedID = strings.TrimSpace(cfg.expectedID); cfg.expectedID == "" {
		return fmt.Errorf("expected id must not be empty")
	}
	if cfg.expectedName = strings.TrimSpace(cfg.expectedName); cfg.expectedName == "" {
		return fmt.Errorf("expected name must not be empty")
	}
	cfg.game = strings.TrimSpace(cfg.game)
	supportedGames := map[string]bool{
		"pokemon":       true,
		"magic":         true,
		"one_piece":     true,
		"lorcana":       true,
		"weiss_schwarz": true,
		"yugioh":        true,
	}
	if !supportedGames[cfg.game] {
		return fmt.Errorf("unsupported game %q", cfg.game)
	}
	if cfg.language = strings.TrimSpace(cfg.language); cfg.language == "" {
		return fmt.Errorf("language must not be empty")
	}
	if cfg.timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}

	if strings.TrimSpace(cfg.fixture) == "" {
		return fmt.Errorf("fixture must not be empty")
	}
	fixture, err := filepath.Abs(cfg.fixture)
	if err != nil {
		return fmt.Errorf("resolving fixture path: %w", err)
	}
	info, err := os.Stat(fixture)
	if err != nil {
		return fmt.Errorf("checking fixture: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("fixture must be a regular file")
	}
	cfg.fixture = fixture

	if cfg.chromePath != "" {
		chromePath, err := filepath.Abs(cfg.chromePath)
		if err != nil {
			return fmt.Errorf("resolving chrome path: %w", err)
		}
		if _, err := os.Stat(chromePath); err != nil {
			return fmt.Errorf("checking chrome path: %w", err)
		}
		cfg.chromePath = chromePath
	}

	return nil
}

func run(cfg config) error {
	allocatorOptions := append(
		[]chromedp.ExecAllocatorOption{},
		chromedp.DefaultExecAllocatorOptions[:]...,
	)
	allocatorOptions = append(
		allocatorOptions,
		chromedp.Flag("headless", cfg.headless),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-sandbox", true),
	)
	if cfg.chromePath != "" {
		allocatorOptions = append(allocatorOptions, chromedp.ExecPath(cfg.chromePath))
	}

	allocatorContext, cancelAllocator := chromedp.NewExecAllocator(
		context.Background(),
		allocatorOptions...,
	)
	defer cancelAllocator()

	browserContext, cancelBrowser := chromedp.NewContext(allocatorContext)
	defer cancelBrowser()

	ctx, cancelTimeout := context.WithTimeout(browserContext, cfg.timeout)
	defer cancelTimeout()

	if err := authenticate(ctx, cfg); err != nil {
		return fmt.Errorf("authenticating through rendered ui: %w", err)
	}
	result, err := scanFixture(ctx, cfg)
	if err != nil {
		return fmt.Errorf("scanning fixture through rendered ui: %w", err)
	}
	if err := validateScanResult(result, cfg.expectedID, cfg.expectedName); err != nil {
		return err
	}
	return nil
}

func authenticate(ctx context.Context, cfg config) error {
	if cfg.verificationToken != "" {
		if err := verifyAccount(ctx, cfg.baseURL, cfg.verificationToken); err != nil {
			return fmt.Errorf("verifying pre-registered account: %w", err)
		}
	}
	if err := navigateToLogin(ctx, cfg.baseURL); err != nil {
		return err
	}

	state, err := login(ctx, cfg.email, cfg.password)
	if err == nil && state.IsLoggedIn {
		return nil
	}
	if !cfg.register {
		return loginFailure(err, state)
	}

	if err := register(ctx, cfg.email, cfg.password); err != nil {
		return fmt.Errorf("registering account: %w", err)
	}
	return errors.New(
		"account registered but email verification is required; obtain the token from the " +
			"confirmation email, then rerun with -verification-token, or use an existing verified account",
	)
}

func navigateToLogin(ctx context.Context, baseURL string) error {
	if err := chromedp.Run(
		ctx,
		chromedp.Navigate(pageURL(baseURL, "/auth")),
		chromedp.WaitVisible(loginEmailSelector, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("opening login page: %w", err)
	}
	return nil
}

func login(ctx context.Context, email, password string) (authState, error) {
	if err := chromedp.Run(
		ctx,
		chromedp.SetValue(loginEmailSelector, email, chromedp.ByQuery),
		chromedp.SetValue(loginPasswordSelector, password, chromedp.ByQuery),
		chromedp.Click(loginSubmitSelector, chromedp.ByQuery),
	); err != nil {
		return authState{}, fmt.Errorf("submitting login form: %w", err)
	}

	return waitForAuthState(ctx, `
(() => {
    const error = document.querySelector('#auth-error')?.textContent.trim() || '';
    const loggedIn = document.querySelector('button[hx-get="/centering"]') !== null;
    return loggedIn || error.length > 0;
})()`)
}

func register(ctx context.Context, email, password string) error {
	if err := chromedp.Run(
		ctx,
		chromedp.Click(`//button[normalize-space()="Register"]`, chromedp.BySearch),
		chromedp.WaitVisible(registerEmailSelector, chromedp.ByQuery),
		chromedp.SetValue(registerEmailSelector, email, chromedp.ByQuery),
		chromedp.SetValue(registerPasswordSelector, password, chromedp.ByQuery),
		chromedp.SetValue(registerConfirmSelector, password, chromedp.ByQuery),
		chromedp.Click(registerSubmitSelector, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("submitting registration form: %w", err)
	}

	state, err := waitForAuthState(ctx, `
(() => {
    const text = document.querySelector('#main-content')?.textContent || '';
    const error = document.querySelector('#auth-register-error')?.textContent.trim() || '';
    return text.includes('Registration successful') || error.length > 0;
})()`)
	if err != nil {
		return err
	}
	if state.RegisterError != "" {
		return fmt.Errorf("registration rejected: %s", state.RegisterError)
	}
	if !state.IsRegistered {
		return fmt.Errorf("registration did not render its success state")
	}
	return nil
}

func verifyAccount(ctx context.Context, baseURL, token string) error {
	confirmationURL := pageURL(baseURL, "/auth/confirm") + "?token=" + url.QueryEscape(token)
	if err := chromedp.Run(
		ctx,
		chromedp.Navigate(confirmationURL),
		chromedp.WaitVisible(`form[hx-post="/auth/confirm"] button[type="submit"]`, chromedp.ByQuery),
		chromedp.Click(`form[hx-post="/auth/confirm"] button[type="submit"]`, chromedp.ByQuery),
		chromedp.Poll(
			`document.querySelector('#confirm-content')?.textContent.includes('Identity Verified') === true`,
			nil,
			chromedp.WithPollingTimeout(15*time.Second),
		),
	); err != nil {
		return fmt.Errorf("submitting confirmation form: %w", err)
	}
	return nil
}

func waitForAuthState(ctx context.Context, condition string) (authState, error) {
	var state authState
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var lastError error
	for {
		var isReady bool
		err := chromedp.Run(ctx, chromedp.Evaluate(condition, &isReady))
		if err == nil && isReady {
			err = chromedp.Run(ctx, chromedp.Evaluate(`
(() => {
    const content = document.querySelector('#main-content')?.textContent || '';
    return {
        path: location.pathname,
        loginError: document.querySelector('#auth-error')?.textContent.trim() || '',
        registerError: document.querySelector('#auth-register-error')?.textContent.trim() || '',
        isLoggedIn: document.querySelector('button[hx-get="/centering"]') !== null,
        isRegistered: content.includes('Registration successful')
    };
		})()`, &state))
			if err == nil {
				return state, nil
			}
		}
		if err != nil {
			lastError = err
		}

		select {
		case <-ctx.Done():
			return state, fmt.Errorf("waiting for authentication response: %w", ctx.Err())
		case <-timer.C:
			if lastError != nil {
				return state, fmt.Errorf(
					"waiting for authentication response: %w (for local http, set SECURE_COOKIES=false)",
					lastError,
				)
			}
			return state, errors.New(
				"authentication response timed out; for local http, set SECURE_COOKIES=false",
			)
		case <-ticker.C:
		}
	}
}

func loginFailure(err error, state authState) error {
	if err != nil {
		return err
	}
	if state.LoginError != "" {
		return fmt.Errorf("login rejected: %s", state.LoginError)
	}
	return fmt.Errorf(
		"login did not establish a session (path %q); for local http, set SECURE_COOKIES=false",
		state.Path,
	)
}

func scanFixture(ctx context.Context, cfg config) (scanResult, error) {
	gameJSON, err := json.Marshal(cfg.game)
	if err != nil {
		return scanResult{}, fmt.Errorf("encoding scanner game: %w", err)
	}
	languageJSON, err := json.Marshal(cfg.language)
	if err != nil {
		return scanResult{}, fmt.Errorf("encoding scanner language: %w", err)
	}

	if err := chromedp.Run(
		ctx,
		chromedp.Navigate(pageURL(cfg.baseURL, "/")),
		chromedp.WaitVisible(scanNavigationSelector, chromedp.ByQuery),
		chromedp.Evaluate(
			`localStorage.setItem('pokget_scan_lang', `+string(languageJSON)+`)`,
			nil,
		),
		chromedp.Evaluate(
			`localStorage.setItem('pokget_scan_game', `+string(gameJSON)+`)`,
			nil,
		),
		chromedp.Click(scanNavigationSelector, chromedp.ByQuery),
		chromedp.WaitReady(fileInputSelector, chromedp.ByQuery),
		chromedp.SetUploadFiles(fileInputSelector, []string{cfg.fixture}, chromedp.ByQuery),
		chromedp.Poll(
			`(() => {
				const root = document.querySelector('#main-content > [x-data]');
				if (!root || !window.Alpine) return false;
				const state = Alpine.$data(root);
				return Boolean(state.previewURL) && !state.scanning;
			})()`,
			nil,
			chromedp.WithPollingTimeout(15*time.Second),
			chromedp.WithPollingInterval(100*time.Millisecond),
		),
		chromedp.WaitVisible(scanUploadSelector, chromedp.ByQuery),
		chromedp.Click(scanUploadSelector, chromedp.ByQuery),
		chromedp.Poll(
			`(() => {
                const root = document.querySelector('#main-content > [x-data]');
                if (!root || !window.Alpine) return false;
                const state = Alpine.$data(root);
				const panel = root.querySelector('[x-show="detectedCard"]');
				if (state.detectedCard === '' || !panel) return false;
				const bounds = panel.getBoundingClientRect();
				const style = getComputedStyle(panel);
				return style.display !== 'none' && style.visibility !== 'hidden' &&
					bounds.width > 0 && bounds.height > 0;
            })()`,
			nil,
			chromedp.WithPollingTimeout(cfg.timeout),
			chromedp.WithPollingInterval(100*time.Millisecond),
		),
	); err != nil {
		var debugState string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`
(() => {
    const root = document.querySelector('#main-content > [x-data]');
    if (!root || !window.Alpine) return JSON.stringify({root: Boolean(root), alpine: Boolean(window.Alpine)});
    const state = Alpine.$data(root);
    return JSON.stringify({
        detectedCard: state.detectedCard,
        detectedID: state.detectedID,
        scanning: state.scanning,
        scanStep: state.scanStep,
        scanStatus: state.scanStatus,
        needsReview: state.needsReview,
        topMatches: state.topMatches,
        panelDisplay: getComputedStyle(root.querySelector('[x-show="detectedCard"]')).display
    });
})()`, &debugState))
		return scanResult{}, fmt.Errorf("uploading fixture and waiting for scan result: %w (state: %s)", err, debugState)
	}

	var result scanResult
	if err := chromedp.Run(ctx, chromedp.Evaluate(`
(() => {
    const root = document.querySelector('#main-content > [x-data]');
    const panel = root?.querySelector('[x-show="detectedCard"]');
	const nameNode = panel?.querySelector('[x-text="detectedCard"]');
	const idNode = panel?.querySelector('`+detectedCardIDSelector+`');
	if (!root || !panel || !nameNode || !idNode || !window.Alpine) {
		return {id: '', name: '', stateID: '', stateName: '', visible: false};
    }
    const state = Alpine.$data(root);
	const panelStyle = getComputedStyle(panel);
	const nameStyle = getComputedStyle(nameNode);
	const idStyle = getComputedStyle(idNode);
	const panelBounds = panel.getBoundingClientRect();
	const nameBounds = nameNode.getBoundingClientRect();
	const idBounds = idNode.getBoundingClientRect();
	const visible = panelStyle.display !== 'none' && panelStyle.visibility !== 'hidden' &&
		nameStyle.display !== 'none' && nameStyle.visibility !== 'hidden' &&
		idStyle.display !== 'none' && idStyle.visibility !== 'hidden' &&
		panelBounds.width > 0 && panelBounds.height > 0 &&
		nameBounds.width > 0 && nameBounds.height > 0 &&
		idBounds.width > 0 && idBounds.height > 0;
    return {
		id: String(idNode.textContent || ''),
		name: String(nameNode.textContent || ''),
		stateID: String(state.detectedID || ''),
		stateName: String(state.detectedCard || ''),
        visible: visible
    };
})()`, &result)); err != nil {
		return scanResult{}, fmt.Errorf("reading rendered scan result: %w", err)
	}
	return result, nil
}

func validateScanResult(result scanResult, expectedID, expectedName string) error {
	if !result.Visible {
		return fmt.Errorf("scan result panel is not visible")
	}
	if actualID := strings.TrimSpace(result.ID); actualID != expectedID {
		return fmt.Errorf("detected card id %q, expected exact id %q", actualID, expectedID)
	}
	if actualName := strings.TrimSpace(result.Name); actualName != expectedName {
		return fmt.Errorf("detected card name %q, expected exact name %q", actualName, expectedName)
	}
	if stateID := strings.TrimSpace(result.StateID); stateID != expectedID {
		return fmt.Errorf("scanner state id %q, expected exact id %q", stateID, expectedID)
	}
	if stateName := strings.TrimSpace(result.StateName); stateName != expectedName {
		return fmt.Errorf("scanner state name %q, expected exact name %q", stateName, expectedName)
	}
	return nil
}

func pageURL(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}
