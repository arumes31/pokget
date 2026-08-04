package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseFlags(t *testing.T) {
	t.Parallel()

	fixture := filepath.Join(t.TempDir(), "card.jpg")
	if err := os.WriteFile(fixture, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	cfg, err := parseFlags([]string{
		"-base-url", "http://localhost:18066/",
		"-email", "smoke@example.com",
		"-password", "secret",
		"-fixture", fixture,
		"-expected-id", "card-123",
		"-expected-name", "Exact Card",
		"-game", "weiss_schwarz",
		"-lang", "jpn",
		"-timeout", "45s",
		"-headless=false",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseFlags returned error: %v", err)
	}

	if cfg.baseURL != "http://localhost:18066" {
		t.Errorf("baseURL = %q, expected normalized URL", cfg.baseURL)
	}
	if cfg.fixture != fixture {
		t.Errorf("fixture = %q, expected %q", cfg.fixture, fixture)
	}
	if cfg.language != "jpn" {
		t.Errorf("language = %q, expected jpn", cfg.language)
	}
	if cfg.game != "weiss_schwarz" {
		t.Errorf("game = %q, expected weiss_schwarz", cfg.game)
	}
	if cfg.timeout != 45*time.Second {
		t.Errorf("timeout = %s, expected 45s", cfg.timeout)
	}
	if cfg.headless {
		t.Error("headless = true, expected false")
	}
}

func TestParseFlagsRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	fixture := filepath.Join(t.TempDir(), "card.jpg")
	if err := os.WriteFile(fixture, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	tests := []struct {
		name          string
		args          []string
		expectedError string
	}{
		{
			name: "unsupported base url scheme",
			args: []string{
				"-base-url", "ftp://localhost",
				"-fixture", fixture,
				"-expected-id", "id",
				"-expected-name", "name",
			},
			expectedError: "scheme must be http or https",
		},
		{
			name: "unsupported game",
			args: []string{
				"-fixture", fixture,
				"-expected-id", "id",
				"-expected-name", "name",
				"-game", "chess",
			},
			expectedError: "unsupported game",
		},
		{
			name: "missing fixture",
			args: []string{
				"-fixture", filepath.Join(t.TempDir(), "missing.jpg"),
				"-expected-id", "id",
				"-expected-name", "name",
			},
			expectedError: "checking fixture",
		},
		{
			name: "missing expected id",
			args: []string{
				"-fixture", fixture,
				"-expected-name", "name",
			},
			expectedError: "expected id must not be empty",
		},
		{
			name: "missing expected name",
			args: []string{
				"-fixture", fixture,
				"-expected-id", "id",
			},
			expectedError: "expected name must not be empty",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags(test.args, &bytes.Buffer{})
			if err == nil {
				t.Fatal("parseFlags returned nil error")
			}
			if !strings.Contains(err.Error(), test.expectedError) {
				t.Errorf("error = %q, expected to contain %q", err, test.expectedError)
			}
		})
	}
}

func TestValidateScanResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		result        scanResult
		expectedError string
	}{
		{
			name: "exact visible match",
			result: scanResult{
				ID:        "card-123",
				Name:      "Exact Card",
				StateID:   "card-123",
				StateName: "Exact Card",
				Visible:   true,
			},
		},
		{
			name: "hidden result",
			result: scanResult{
				ID:        "card-123",
				Name:      "Exact Card",
				StateID:   "card-123",
				StateName: "Exact Card",
			},
			expectedError: "not visible",
		},
		{
			name: "id case mismatch",
			result: scanResult{
				ID:        "CARD-123",
				Name:      "Exact Card",
				StateID:   "card-123",
				StateName: "Exact Card",
				Visible:   true,
			},
			expectedError: "expected exact id",
		},
		{
			name: "name case mismatch",
			result: scanResult{
				ID:        "card-123",
				Name:      "exact card",
				StateID:   "card-123",
				StateName: "Exact Card",
				Visible:   true,
			},
			expectedError: "expected exact name",
		},
		{
			name: "state id mismatch",
			result: scanResult{
				ID:        "card-123",
				Name:      "Exact Card",
				StateID:   "different-id",
				StateName: "Exact Card",
				Visible:   true,
			},
			expectedError: "scanner state id",
		},
		{
			name: "state name mismatch",
			result: scanResult{
				ID:        "card-123",
				Name:      "Exact Card",
				StateID:   "card-123",
				StateName: "Different Card",
				Visible:   true,
			},
			expectedError: "scanner state name",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateScanResult(test.result, "card-123", "Exact Card")
			if test.expectedError == "" {
				if err != nil {
					t.Errorf("validateScanResult returned error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("validateScanResult returned nil error")
			}
			if !strings.Contains(err.Error(), test.expectedError) {
				t.Errorf("error = %q, expected to contain %q", err, test.expectedError)
			}
		})
	}
}

func TestPageURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		baseURL  string
		path     string
		expected string
	}{
		{
			name:     "plain",
			baseURL:  "http://localhost:18066",
			path:     "/centering",
			expected: "http://localhost:18066/centering",
		},
		{
			name:     "trailing and leading slashes",
			baseURL:  "http://localhost:18066/",
			path:     "/auth",
			expected: "http://localhost:18066/auth",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if actual := pageURL(test.baseURL, test.path); actual != test.expected {
				t.Errorf("pageURL() = %q, expected %q", actual, test.expected)
			}
		})
	}
}
