package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// liveConfig points the benchmark at the public server instead of the
// in-process pipeline, so the run exercises authentication, CSRF, upload
// limits and the scan endpoint exactly like the browser does.
type liveConfig struct {
	BaseURL  string
	Email    string
	Password string
}

type liveSession struct {
	client *http.Client
	base   string
	csrf   string
}

var csrfTokenPattern = regexp.MustCompile(`name="gorilla\.csrf\.Token" value="([^"]+)"`)

func newLiveSession(ctx context.Context, cfg liveConfig) (*liveSession, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, errors.New("base URL is required")
	}
	if cfg.Email == "" || cfg.Password == "" {
		return nil, errors.New("email and password are required for the live scan")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	session := &liveSession{
		client: &http.Client{Jar: jar, Timeout: 60 * time.Second},
		base:   base,
	}
	token, err := session.fetchCSRF(ctx, base+"/auth")
	if err != nil {
		return nil, fmt.Errorf("load login page: %w", err)
	}

	form := url.Values{
		"email":              {cfg.Email},
		"password":           {cfg.Password},
		"gorilla.csrf.Token": {token},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-CSRF-Token", token)
	request.Header.Set("Origin", base)
	request.Header.Set("Referer", base+"/auth")
	response, err := session.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login failed with status %d: %s", response.StatusCode, firstLine(body))
	}
	if response.Request != nil && response.Request.URL != nil && response.Request.URL.Path == "/auth" {
		return nil, errors.New("login returned the auth page instead of the dashboard")
	}

	// The CSRF cookie is not rotated by login, so the token from /auth stays
	// valid for the authenticated session. Keep it instead of scraping the
	// dashboard, which does not render a hidden token field.
	session.csrf = token
	return session, nil
}

func (s *liveSession) fetchCSRF(ctx context.Context, target string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", response.StatusCode)
	}
	match := csrfTokenPattern.FindSubmatch(body)
	if match == nil {
		return "", errors.New("CSRF token not found in page")
	}
	// html/template escapes "+" as &#43; inside attribute values; the browser
	// decodes it, so the harness must too or every POST is rejected.
	return html.UnescapeString(string(match[1])), nil
}

type liveScanResponse struct {
	Detected        string  `json:"detected"`
	ID              string  `json:"id"`
	CollectorNumber string  `json:"collector_number"`
	Confidence      float64 `json:"confidence"`
	NeedsReview     bool    `json:"needs_review"`
	Text            string  `json:"text"`
	TopMatches      []struct {
		ID              string  `json:"id"`
		Name            string  `json:"name"`
		CollectorNumber string  `json:"collector_number"`
		Confidence      float64 `json:"confidence"`
		NeedsReview     bool    `json:"needs_review"`
	} `json:"top_matches"`
}

func (s *liveSession) scan(ctx context.Context, game, language string, image []byte) (*liveScanResponse, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("card_image", "card.png")
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(image); err != nil {
		return nil, err
	}
	if err := writer.WriteField("game", game); err != nil {
		return nil, err
	}
	if err := writer.WriteField("lang", language); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/api/scan", &body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-CSRF-Token", s.csrf)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Origin", s.base)
	request.Header.Set("Referer", s.base+"/dashboard")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scan status %d: %s", response.StatusCode, firstLine(payload))
	}
	var parsed liveScanResponse
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, fmt.Errorf("decode scan response: %w", err)
	}
	return &parsed, nil
}

func scanOneLive(ctx context.Context, imageClient *http.Client, session *liveSession,
	combination combination, sample sampleCard, timeout time.Duration) resultRow {
	row := resultRow{
		Game:           combination.game,
		Language:       combination.language,
		ExpectedID:     sample.Card.ID,
		ExpectedName:   sample.Card.Name,
		ExpectedNumber: sample.Card.CollectorNumber,
	}
	started := time.Now()
	imageBytes, err := fetchImage(ctx, imageClient, sample.Card.ImageURL)
	if err != nil {
		row.Error = err.Error()
		row.DurationMS = time.Since(started).Milliseconds()
		return row
	}

	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var result *liveScanResponse
	for attempt := 1; attempt <= 3; attempt++ {
		result, err = session.scan(scanCtx, combination.game, combination.language, imageBytes)
		if err != nil && strings.Contains(err.Error(), "status 429") && attempt < 3 {
			select {
			case <-scanCtx.Done():
				row.Error = scanCtx.Err().Error()
				row.DurationMS = time.Since(started).Milliseconds()
				return row
			case <-time.After(6 * time.Second):
			}
			continue
		}
		break
	}
	row.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		row.Error = err.Error()
		return row
	}

	row.Status = "matched"
	if result.NeedsReview {
		row.Status = "needs_review"
	}
	row.ActualID = result.ID
	row.ActualName = result.Detected
	row.ActualNumber = result.CollectorNumber
	row.Confidence = result.Confidence
	row.Exact = result.ID != "" && result.ID == sample.Card.ID
	row.SamePrinting = !row.Exact &&
		strings.EqualFold(strings.TrimSpace(result.Detected), strings.TrimSpace(sample.Card.Name)) &&
		strings.EqualFold(strings.TrimSpace(result.CollectorNumber), strings.TrimSpace(sample.Card.CollectorNumber)) &&
		result.Detected != ""
	for _, match := range result.TopMatches {
		if match.ID != "" && match.ID == sample.Card.ID {
			row.ReviewCandidate = true
			break
		}
	}
	if !row.Exact {
		row.Notes = differenceNotes(sample.Card, result.Text)
	}
	return row
}

func firstLine(payload []byte) string {
	line := strings.TrimSpace(string(payload))
	if index := strings.IndexByte(line, '\n'); index >= 0 {
		line = line[:index]
	}
	if len(line) > 200 {
		line = line[:200]
	}
	return line
}
