package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"pokget/internal/detectiontest"
)

func TestValidateAcceptanceManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*detectiontest.OutputManifest)
		wantError string
	}{
		{
			name: "all required games and variants",
		},
		{
			name: "wrong card count",
			mutate: func(manifest *detectiontest.OutputManifest) {
				manifest.Cards = manifest.Cards[:len(manifest.Cards)-1]
			},
			wantError: "got 5 cards, want exactly 6",
		},
		{
			name: "duplicate game",
			mutate: func(manifest *detectiontest.OutputManifest) {
				manifest.Cards[len(manifest.Cards)-1].Card.GameSlug = manifest.Cards[0].Card.GameSlug
			},
			wantError: `duplicate game slug "pokemon"`,
		},
		{
			name: "unsupported game",
			mutate: func(manifest *detectiontest.OutputManifest) {
				manifest.Cards[len(manifest.Cards)-1].Card.GameSlug = "unsupported"
			},
			wantError: `unsupported game slug "unsupported"`,
		},
		{
			name: "wrong variant count",
			mutate: func(manifest *detectiontest.OutputManifest) {
				manifest.Cards[0].Artifacts = manifest.Cards[0].Artifacts[:len(requiredVariants)-1]
			},
			wantError: `game "pokemon" has 6 variants, want exactly 7`,
		},
		{
			name: "duplicate variant",
			mutate: func(manifest *detectiontest.OutputManifest) {
				manifest.Cards[0].Artifacts[len(requiredVariants)-1].Variant = requiredVariants[0]
			},
			wantError: `game "pokemon" has duplicate variant "source"`,
		},
		{
			name: "unsupported variant",
			mutate: func(manifest *detectiontest.OutputManifest) {
				manifest.Cards[0].Artifacts[len(requiredVariants)-1].Variant = "contrast"
			},
			wantError: `game "pokemon" has unsupported variant "contrast"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			manifest := validAcceptanceManifest()
			if tt.mutate != nil {
				tt.mutate(&manifest)
			}
			err := validateAcceptanceManifest(manifest)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("validateAcceptanceManifest() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("validateAcceptanceManifest() error = %v, want containing %q", err, tt.wantError)
			}
		})
	}
}

func TestScanRequiresExplicitNeedsReview(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		response   string
		wantReview bool
		wantError  string
	}{
		{
			name:       "explicit false",
			response:   `{"detected":"Furret","id":"card-id","confidence":100,"needs_review":false}`,
			wantReview: false,
		},
		{
			name:       "explicit true",
			response:   `{"detected":"Furret","id":"card-id","confidence":80,"needs_review":true}`,
			wantReview: true,
		},
		{
			name:      "missing",
			response:  `{"detected":"Furret","id":"card-id","confidence":100}`,
			wantError: "missing boolean needs_review",
		},
		{
			name:      "null",
			response:  `{"detected":"Furret","id":"card-id","confidence":100,"needs_review":null}`,
			wantError: "missing boolean needs_review",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				fmt.Fprint(writer, tt.response)
			}))
			defer server.Close()

			imagePath := writeScanInput(t)
			response, err := scan(t.Context(), server.Client(), server.URL, imagePath)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("scan() error = %v, want containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("scan() error = %v", err)
			}
			if response.NeedsReview == nil {
				t.Fatal("scan() NeedsReview = nil, want explicit value")
			}
			if *response.NeedsReview != tt.wantReview {
				t.Fatalf("scan() NeedsReview = %t, want %t", *response.NeedsReview, tt.wantReview)
			}
		})
	}
}

func TestScanResponseAcceptanceRequiresExactIdentityAndNoReview(t *testing.T) {
	t.Parallel()

	noReview := false
	review := true
	tests := []struct {
		name     string
		response scanResponse
		want     bool
	}{
		{
			name: "exact identity without review",
			response: scanResponse{
				ID:          "canonical-id",
				Detected:    "Furret",
				NeedsReview: &noReview,
			},
			want: true,
		},
		{
			name: "different canonical id",
			response: scanResponse{
				ID:          "other-id",
				Detected:    "Furret",
				NeedsReview: &noReview,
			},
		},
		{
			name: "different canonical name",
			response: scanResponse{
				ID:          "canonical-id",
				Detected:    "furret",
				NeedsReview: &noReview,
			},
		},
		{
			name: "review required",
			response: scanResponse{
				ID:          "canonical-id",
				Detected:    "Furret",
				NeedsReview: &review,
			},
		},
		{
			name: "review field absent",
			response: scanResponse{
				ID:       "canonical-id",
				Detected: "Furret",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := acceptScanResponse(tt.response, "canonical-id", "Furret")
			if got != tt.want {
				t.Fatalf("acceptance result = %t, want %t", got, tt.want)
			}
		})
	}
}

func validAcceptanceManifest() detectiontest.OutputManifest {
	manifest := detectiontest.OutputManifest{
		Cards: make([]detectiontest.OutputCard, 0, len(requiredGameSlugs)),
	}
	for _, slug := range requiredGameSlugs {
		selected := detectiontest.OutputCard{
			Card:      detectiontest.Card{GameSlug: slug},
			Artifacts: make([]detectiontest.Artifact, 0, len(requiredVariants)),
		}
		for _, variant := range requiredVariants {
			selected.Artifacts = append(selected.Artifacts, detectiontest.Artifact{Variant: variant})
		}
		manifest.Cards = append(manifest.Cards, selected)
	}
	return manifest
}

func writeScanInput(t *testing.T) string {
	t.Helper()

	path := t.TempDir() + string(os.PathSeparator) + "card.png"
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write scan input: %v", err)
	}
	return path
}
