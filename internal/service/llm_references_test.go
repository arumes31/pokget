package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pokget/internal/models"
)

func TestLLMArtworkURLsUseExactTrustedHTTPSHosts(t *testing.T) {
	for _, raw := range []string{
		"https://en.onepiece-cardgame.com/images/card.png?version=2",
		"https://assets.tcgdex.net/en/swsh/swsh3/136/high.webp",
		"https://cards.scryfall.io/normal/front/example.jpg",
	} {
		if got := allowedLLMArtworkURL(raw); got != raw {
			t.Errorf("trusted image URL rejected: %s", raw)
		}
	}
	for _, raw := range []string{
		"http://en.onepiece-cardgame.com/image.png",
		"https://user:secret@en.onepiece-cardgame.com/image.png",
		"https://en.onepiece-cardgame.com:443/image.png",
		"https://en.onepiece-cardgame.com:/image.png",
		"https://en.onepiece-cardgame.com/image.png#fragment",
		"https://en.onepiece-cardgame.com/image.png#",
		"https://en.onepiece-cardgame.com.evil.invalid/image.png",
		"https://en.onepiece-cardgame.com@evil.invalid/image.png",
		"https://localhost/image.png", "https://127.0.0.1/image.png", "https://10.0.0.1/image.png",
		"https://[::1]/image.png", "https://untrusted.invalid/image.png",
	} {
		if got := allowedLLMArtworkURL(raw); got != "" {
			t.Errorf("unsafe artwork URL accepted: %s", raw)
		}
	}
}

func TestLLMArtworkReferencesKeepGroupsCompleteWithinBudget(t *testing.T) {
	group := func(name string, count int) []candidateEvidence {
		var candidates []candidateEvidence
		for index := range count {
			card := referenceTestCards()[0]
			card.ID, card.Name = fmt.Sprintf("%s-%d", name, index), name
			card.ImageURL = fmt.Sprintf("https://en.onepiece-cardgame.com/%s-%d.png", name, index)
			candidates = append(candidates, candidateEvidence{Card: card, Score: 1000 - index})
		}
		return candidates
	}
	for _, test := range []struct {
		name      string
		shortlist []candidateEvidence
		want      []string
	}{
		{name: "whole groups until cap", shortlist: append(append(group("first", 6), group("does-not-fit", 3)...), group("last", 2)...), want: []string{"first-0", "first-1", "first-2", "first-3", "first-4", "first-5", "last-0", "last-1"}},
		{name: "oversized group omitted", shortlist: append(group("oversized", 9), group("small", 2)...), want: []string{"small-0", "small-1"}},
		{name: "singleton omitted", shortlist: group("single", 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			var eligible []models.Card
			for _, candidate := range test.shortlist {
				eligible = append(eligible, candidate.Card)
			}
			got := shortlistArtworkReferences(test.shortlist, eligible)
			if len(got) != len(test.want) {
				t.Fatalf("references=%v want=%v", got, test.want)
			}
			for index := range got {
				if got[index].CardID != test.want[index] {
					t.Fatalf("reference order=%v want=%v", got, test.want)
				}
			}
		})
	}
	for _, invalidURL := range []string{"", "https://untrusted.invalid/art.png"} {
		candidates := group("incomplete", 2)
		candidates[1].Card.ImageURL = invalidURL
		eligible := []models.Card{candidates[0].Card, candidates[1].Card}
		if got := shortlistArtworkReferences(candidates, eligible); len(got) != 0 {
			t.Fatal("part of a group with unavailable artwork was attached")
		}
	}
	complete := group("cut-by-shortlist", 9)
	var eligible []models.Card
	for _, candidate := range complete {
		eligible = append(eligible, candidate.Card)
	}
	if got := shortlistArtworkReferences(complete[:8], eligible); len(got) != 0 {
		t.Fatal("shortlist truncation produced a partial reference group")
	}
}

func TestLLMPrimaryOmitsUnsafeOrTextOnlyReferences(t *testing.T) {
	for _, mode := range []string{"unsafe member", "text only"} {
		t.Run(mode, func(t *testing.T) {
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if strings.Contains(string(body), "onepiece-cardgame.com") || strings.Contains(string(body), "untrusted.invalid") || strings.Contains(string(body), "reference_card_id") {
					t.Error("unsafe, partial, or text-only reference group was sent")
				}
				writePrimaryLLMResponse(w, `{"card_id":"art-b"}`)
			}))
			defer primary.Close()
			fallback := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected fallback") }))
			defer fallback.Close()
			cards := referenceTestCards()
			var imageData []byte
			if mode == "unsafe member" {
				cards[1].ImageURL = "https://untrusted.invalid/art.png"
				imageData = []byte("\x89PNG\r\n\x1a\n")
			}
			service := primarySecurityService(t, primary.URL, fallback.URL, nil)
			if _, err := service.FuzzyMatchCardWithImageContext(context.Background(), "Foxy OP07-071", imageData, cards); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func referenceTestCards() []models.Card {
	return []models.Card{
		{ID: "art-a", Name: "Foxy", Game: "one_piece", Language: "en", Set: "500 Years", SetCode: "OP07", CollectorNumber: "OP07-071", Variant: "Normal", ImageURL: "https://en.onepiece-cardgame.com/images/cardlist/card/OP07-071_p1.png?260828"},
		{ID: "art-b", Name: "Foxy", Game: "one_piece", Language: "en", Set: "500 Years", SetCode: "OP07", CollectorNumber: "OP07-071", Variant: "Normal", ImageURL: "https://en.onepiece-cardgame.com/images/cardlist/card/OP07-071.png?260828"},
	}
}

func TestLLMPrimaryVisionLabelsCompleteArtworkReferences(t *testing.T) {
	cards := referenceTestCards()
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer primary-secret-marker" || strings.Contains(string(body), "primary-secret-marker") {
			t.Error("primary credential must appear only in its authorization header")
		}
		var request struct {
			Messages []struct {
				Content []struct {
					Type, Text string
					ImageURL   struct{ URL string } `json:"image_url"`
				}
			}
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
		}
		if len(request.Messages) != 1 || len(request.Messages[0].Content) != 6 {
			t.Error("expected the scan plus both labeled artwork references")
		} else {
			parts := request.Messages[0].Content
			if !strings.HasPrefix(parts[1].ImageURL.URL, "data:image/png;base64,") {
				t.Error("the scanned card must be the first image")
			}
			for index, card := range cards {
				if !strings.Contains(parts[2+index*2].Text, card.ID) || parts[3+index*2].ImageURL.URL != card.ImageURL {
					t.Errorf("reference artwork is not mapped to printing %s", card.ID)
				}
			}
			if !strings.Contains(parts[0].Text, "filename") || !strings.Contains(parts[0].Text, "reference") {
				t.Error("prompt must distinguish the scan from art references and reject filename inference")
			}
		}
		writePrimaryLLMResponse(w, `{"card_id":"art-b"}`)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected fallback") }))
	defer fallback.Close()
	service := primarySecurityService(t, primary.URL, fallback.URL, nil)
	result, err := service.FuzzyMatchCardWithImageContext(context.Background(), "Foxy OP07-071", []byte("\x89PNG\r\n\x1a\n"), cards)
	if err != nil || result == nil || result.CardID != "art-b" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestLLMArtworkReferencesStayOutOfTextFallback(t *testing.T) {
	for _, primaryEnabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "primary failure", false: "Ollama only"}[primaryEnabled], func(t *testing.T) {
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
			defer primary.Close()
			fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if r.Header.Get("Authorization") != "" || strings.Contains(string(body), "image_url") || strings.Contains(string(body), "onepiece-cardgame.com") || strings.Contains(string(body), "primary-secret-marker") {
					t.Error("text fallback received artwork or primary credentials")
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"response": `{"card_id":"art-b"}`})
			}))
			defer fallback.Close()
			primaryURL := ""
			if primaryEnabled {
				primaryURL = primary.URL
			}
			service := primarySecurityService(t, primaryURL, fallback.URL, nil)
			result, err := service.FuzzyMatchCardWithImageContext(context.Background(), "Foxy OP07-071", []byte("\x89PNG\r\n\x1a\n"), referenceTestCards())
			if err != nil || result == nil || result.CardID != "art-b" {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}
