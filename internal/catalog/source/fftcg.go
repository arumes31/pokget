package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"pokget/internal/catalog"
)

// FFTCGProvider imports the Final Fantasy Trading Card Game catalog from Square Enix's official database.
type FFTCGProvider struct {
	HTTP         HTTPOptions
	BaseURL      string
	ImageBaseURL string
	Language     string
}

func (p *FFTCGProvider) ID() string         { return "fftcg" }
func (p *FFTCGProvider) Game() catalog.Game { return catalog.GameFFTCG }

type fftcgCard struct {
	ID         any      `json:"id"`
	Code       string   `json:"code"`
	Image      string   `json:"image"`
	Element    []string `json:"element"`
	Rarity     string   `json:"rarity"`
	Cost       any      `json:"cost"`
	Power      any      `json:"power"`
	Category1  string   `json:"category_1"`
	Category2  string   `json:"category_2"`
	Multicard  any      `json:"multicard"`
	ExBurst    any      `json:"ex_burst"`
	Set        []string `json:"set"`
	NameEN     string   `json:"name_en"`
	TypeEN     string   `json:"type_en"`
	JobEN      string   `json:"job_en"`
	TextEN     string   `json:"text_en"`
	NameJA     string   `json:"name_ja"`
	TypeJA     string   `json:"type_ja"`
	JobJA      string   `json:"job_ja"`
	TextJA     string   `json:"text_ja"`
	NameDE     string   `json:"name_de"`
	TypeDE     string   `json:"type_de"`
	JobDE      string   `json:"job_de"`
	TextDE     string   `json:"text_de"`
	NameES     string   `json:"name_es"`
	TypeES     string   `json:"type_es"`
	JobES      string   `json:"job_es"`
	TextES     string   `json:"text_es"`
	NameFR     string   `json:"name_fr"`
	TypeFR     string   `json:"type_fr"`
	JobFR      string   `json:"job_fr"`
	TextFR     string   `json:"text_fr"`
	NameIT     string   `json:"name_it"`
	TypeIT     string   `json:"type_it"`
	JobIT      string   `json:"job_it"`
	TextIT     string   `json:"text_it"`
}

type fftcgResponse struct {
	Cards []fftcgCard `json:"cards"`
	Count int         `json:"count"`
}

var fftcgFilenamePattern = regexp.MustCompile(`"filename"\s*:\s*"([^"]+)"`)

func (p *FFTCGProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://fftcg.square-enix-games.com"
	}
	imageBaseURL := strings.TrimRight(p.ImageBaseURL, "/")
	if imageBaseURL == "" {
		imageBaseURL = "https://fftcg.cdn.sewest.net"
	}

	lang := strings.ToLower(strings.TrimSpace(p.Language))
	if lang == "" {
		lang = "en"
	}

	endpoint := baseURL + "/en/get-cards"
	reqPayload := map[string]any{
		"language":   "",
		"text":       "",
		"type":       []string{},
		"element":    []string{},
		"cost":       []string{},
		"rarity":     []string{},
		"power":      []string{},
		"category_1": []string{},
		"set":        []string{},
		"multicard":  "",
		"ex_burst":   "",
		"code":       "",
		"special":    "",
		"exactmatch": 0,
	}

	payloadBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return catalog.FetchResult{}, fmt.Errorf("fftcg: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return catalog.FetchResult{}, fmt.Errorf("fftcg: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	userAgent := p.HTTP.UserAgent
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	}
	req.Header.Set("User-Agent", userAgent)
	if request.ETag != "" {
		req.Header.Set("If-None-Match", request.ETag)
	}
	if request.LastModified != "" {
		req.Header.Set("If-Modified-Since", request.LastModified)
	}

	resp, err := p.HTTP.client().Do(req)
	if err != nil {
		return catalog.FetchResult{}, fmt.Errorf("fftcg: POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	result := catalog.FetchResult{
		ETag:             resp.Header.Get("ETag"),
		LastModified:     resp.Header.Get("Last-Modified"),
		CompleteSnapshot: true,
		NotModified:      resp.StatusCode == http.StatusNotModified,
	}
	if result.NotModified {
		return result, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return result, fmt.Errorf("fftcg: POST %s returned status %s", endpoint, resp.Status)
	}

	var response fftcgResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, p.HTTP.maxBodyBytes()+1)).Decode(&response); err != nil {
		return result, fmt.Errorf("fftcg: decode response: %w", err)
	}

	seen := make(map[string]struct{}, len(response.Cards))
	for _, card := range response.Cards {
		code := strings.TrimSpace(card.Code)
		if code == "" {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}

		name := p.cardNameForLang(card, lang)
		if name == "" {
			name = card.NameEN
		}
		if name == "" {
			name = card.NameJA
		}
		if name == "" {
			name = code
		}

		setName := ""
		if len(card.Set) > 0 && strings.TrimSpace(card.Set[0]) != "" {
			setName = strings.TrimSpace(card.Set[0])
		}
		setCode := fftcgSetCode(code, setName)
		if setName == "" {
			setName = setCode
		}

		rarity := mapFFTCGRarity(card.Rarity)
		record := catalog.CardRecord{
			SourceCardID:    code,
			Name:            name,
			SetCode:         setCode,
			SetName:         setName,
			CollectorNumber: code,
			Language:        lang,
			Rarity:          rarity,
			Metadata:        rawMetadata(card),
		}

		// Extract image filename
		if match := fftcgFilenamePattern.FindStringSubmatch(card.Image); len(match) == 2 {
			filename := match[1]
			// Support language-specific card face if available
			switch lang {
			case "de", "es", "fr", "it":
				filename = strings.Replace(filename, "_eg.", "_"+lang+".", 1)
			}
			imgURL := imageBaseURL + "/" + strings.TrimPrefix(filename, "/")
			record.Images = []catalog.ImageRecord{{
				SourceImageID: code + ":front",
				Kind:          "front",
				URL:           imgURL,
			}}
		}

		record.Printings = []catalog.PrintingRecord{{
			SourcePrintingID: code,
			SetCode:          setCode,
			SetName:          setName,
			CollectorNumber:  code,
			Language:         lang,
			Rarity:           rarity,
			SourceImageIDs:   imageIDs(record.Images),
			Metadata:         rawMetadata(card),
		}}

		if err := record.Validate(); err != nil {
			return result, err
		}
		if err := emit(record); err != nil {
			return result, err
		}
		result.Count++
	}

	return result, nil
}

func (p *FFTCGProvider) cardNameForLang(card fftcgCard, lang string) string {
	switch lang {
	case "ja":
		return strings.TrimSpace(card.NameJA)
	case "de":
		return strings.TrimSpace(card.NameDE)
	case "es":
		return strings.TrimSpace(card.NameES)
	case "fr":
		return strings.TrimSpace(card.NameFR)
	case "it":
		return strings.TrimSpace(card.NameIT)
	default:
		return strings.TrimSpace(card.NameEN)
	}
}

func fftcgSetCode(code, setName string) string {
	if idx := strings.Index(code, "-"); idx > 0 {
		return "Opus " + code[:idx]
	}
	if setName != "" {
		return setName
	}
	return "FFTCG"
}

func mapFFTCGRarity(rarity string) string {
	switch strings.ToUpper(strings.TrimSpace(rarity)) {
	case "C":
		return "Common"
	case "R":
		return "Rare"
	case "H":
		return "Hero"
	case "L":
		return "Legend"
	case "S":
		return "Starter"
	case "B":
		return "Boss"
	case "PR":
		return "Promo"
	default:
		if rarity != "" {
			return rarity
		}
		return "Normal"
	}
}
