package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"pokget/internal/catalog"
)

type WoWTCGProvider struct {
	HTTP         HTTPOptions
	BaseURL      string
	ImageBaseURL string
}

func (p *WoWTCGProvider) ID() string         { return "wowtcg" }
func (p *WoWTCGProvider) Game() catalog.Game { return catalog.GameWorldOfWarcraft }

type wowCard struct {
	CardID      any      `json:"cardid"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Subtype     string   `json:"subtype"`
	Subtype2    string   `json:"subtype2"`
	Faction     string   `json:"faction"`
	Image       string   `json:"image"`
	SetName     string   `json:"setname"`
	SetCode     string   `json:"setcode"`
	Rarity      string   `json:"rarity"`
	No          any      `json:"no"`
	Artist      string   `json:"artist"`
	Cost        any      `json:"cost"`
	Health      any      `json:"health"`
	Attack      any      `json:"atk"`
	Rules       string   `json:"rules"`
	Flavour     string   `json:"flavour"`
	Reprinted   any      `json:"reprinted"`
	ReprintedID any      `json:"reprintedid"`
	ClassIcons  []string `json:"classIcons"`
	Popularity  any      `json:"popularity"`
	Price       any      `json:"price"`
}

func parseAnyString(v any) string {
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		return ""
	}
}

func mapWoWRarity(r string) string {
	switch strings.ToLower(strings.TrimSpace(r)) {
	case "c":
		return "Common"
	case "u":
		return "Uncommon"
	case "r":
		return "Rare"
	case "e":
		return "Epic"
	case "l":
		return "Legendary"
	case "p":
		return "Promo"
	default:
		if r == "" {
			return "Common"
		}
		return r
	}
}

func mapWoWVariant(card wowCard) string {
	rarity := strings.ToLower(strings.TrimSpace(card.Rarity))
	setName := strings.ToLower(card.SetName)
	img := strings.ToLower(card.Image)

	if rarity == "l" || strings.Contains(setName, "loot") || strings.HasSuffix(img, "_loot") {
		return "Loot"
	}
	if strings.Contains(setName, "foil") || strings.HasSuffix(img, "_foil") {
		return "Foil"
	}
	if strings.Contains(setName, "(ea)") || strings.HasSuffix(img, "_ea") {
		return "Extended Art"
	}
	return "Normal"
}

func decompressWoWLZW(data string) (string, error) {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return "", fmt.Errorf("empty data")
	}

	parts := strings.Split(trimmed, ",")
	if len(parts) == 0 {
		return "", fmt.Errorf("empty tokens")
	}

	h := make([]string, 256)
	for i := 0; i < 256; i++ {
		h[i] = string(rune(i))
	}

	firstVal, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", fmt.Errorf("invalid initial token %q: %w", parts[0], err)
	}

	c := string(rune(firstVal))
	var b strings.Builder
	b.Grow(len(parts) * 2)
	b.WriteString(c)

	for i := 1; i < len(parts); i++ {
		token := parts[i]
		if token == "" {
			continue
		}
		d, err := strconv.Atoi(token)
		if err != nil {
			return "", fmt.Errorf("invalid token %q at %d: %w", token, i, err)
		}

		var g string
		if d < len(h) {
			g = h[d]
		} else if d == len(h) {
			g = c + string(c[0])
		}

		b.WriteString(g)
		if len(g) > 0 {
			h = append(h, c+string(g[0]))
		}
		c = g
	}

	return b.String(), nil
}

func (p *WoWTCGProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://wow.tcgbrowser.com"
	}

	endpoint := baseURL
	if !strings.HasSuffix(endpoint, ".dat") && !strings.HasSuffix(endpoint, ".json") {
		endpoint += "/dl/cards.dat"
	}

	imageBaseURL := strings.TrimRight(p.ImageBaseURL, "/")
	if imageBaseURL == "" {
		imageBaseURL = baseURL
	}

	req, err := p.HTTP.newRequest(ctx, http.MethodGet, endpoint, request)
	if err != nil {
		return catalog.FetchResult{}, err
	}
	req.Header.Set("Accept", "*/*")
	if strings.TrimSpace(p.HTTP.UserAgent) == "" {
		req.Header.Set("User-Agent", "PokgetScanner/1.0")
	}

	resp, err := p.HTTP.client().Do(req)
	if err != nil {
		return catalog.FetchResult{}, fmt.Errorf("catalog source wowtcg: GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	meta := responseMeta{
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		NotModified:  resp.StatusCode == http.StatusNotModified,
	}

	result := catalog.FetchResult{
		ETag:             meta.ETag,
		LastModified:     meta.LastModified,
		CompleteSnapshot: true,
		NotModified:      meta.NotModified,
	}
	if meta.NotModified {
		return result, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return result, fmt.Errorf("catalog source wowtcg: GET %s returned %s", endpoint, resp.Status)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, p.HTTP.maxBodyBytes()+1))
	if err != nil {
		return result, fmt.Errorf("catalog source wowtcg: reading %s: %w", endpoint, err)
	}

	trimmedBody := strings.TrimSpace(string(bodyBytes))
	var cardsMap map[string]wowCard

	if strings.HasPrefix(trimmedBody, "{") {
		if err := json.Unmarshal([]byte(trimmedBody), &cardsMap); err != nil {
			return result, fmt.Errorf("catalog source wowtcg: decode json map: %w", err)
		}
	} else if strings.HasPrefix(trimmedBody, "[") {
		var cardsList []wowCard
		if err := json.Unmarshal([]byte(trimmedBody), &cardsList); err != nil {
			return result, fmt.Errorf("catalog source wowtcg: decode json array: %w", err)
		}
		cardsMap = make(map[string]wowCard, len(cardsList))
		for i, c := range cardsList {
			key := parseAnyString(c.CardID)
			if key == "" {
				key = strconv.Itoa(i + 1)
			}
			cardsMap[key] = c
		}
	} else {
		decompressed, err := decompressWoWLZW(trimmedBody)
		if err != nil {
			return result, fmt.Errorf("catalog source wowtcg: decompress lzw: %w", err)
		}
		if err := json.Unmarshal([]byte(decompressed), &cardsMap); err != nil {
			return result, fmt.Errorf("catalog source wowtcg: decode decompressed json: %w", err)
		}
	}

	// Sort keys for deterministic emission
	keys := make([]string, 0, len(cardsMap))
	for k := range cardsMap {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		numI, errI := strconv.Atoi(keys[i])
		numJ, errJ := strconv.Atoi(keys[j])
		if errI == nil && errJ == nil {
			return numI < numJ
		}
		return keys[i] < keys[j]
	})

	for _, k := range keys {
		card := cardsMap[k]
		if strings.TrimSpace(card.Name) == "" {
			continue
		}

		sourceCardID := parseAnyString(card.CardID)
		if sourceCardID == "" {
			sourceCardID = k
		}

		setCode := strings.TrimSpace(card.SetCode)
		if setCode == "" {
			setCode = "WoW"
		}

		setName := strings.TrimSpace(card.SetName)
		if setName == "" {
			setName = setCode
		}

		collectorNumber := parseAnyString(card.No)
		if collectorNumber == "" || collectorNumber == "0" {
			collectorNumber = sourceCardID
		}

		rarity := mapWoWRarity(card.Rarity)
		variant := mapWoWVariant(card)

		var images []catalog.ImageRecord
		var imageIDs []string
		if strings.TrimSpace(card.Image) != "" {
			var imageURL string
			if strings.HasPrefix(card.Image, "http://") || strings.HasPrefix(card.Image, "https://") {
				imageURL = card.Image
			} else {
				imageURL = fmt.Sprintf("%s/images/cards/medium/%s.jpg", imageBaseURL, card.Image)
			}

			imgID := sourceCardID + ":front"
			images = append(images, catalog.ImageRecord{
				SourceImageID: imgID,
				Kind:          "front",
				URL:           imageURL,
			})
			imageIDs = append(imageIDs, imgID)
		}

		record := catalog.CardRecord{
			SourceCardID:    sourceCardID,
			Name:            card.Name,
			SetCode:         setCode,
			SetName:         setName,
			CollectorNumber: collectorNumber,
			Language:        "en",
			Rarity:          rarity,
			Variant:          variant,
			Images:          images,
			Printings: []catalog.PrintingRecord{{
				SourcePrintingID: sourceCardID,
				SetCode:          setCode,
				SetName:          setName,
				CollectorNumber:  collectorNumber,
				Language:         "en",
				Rarity:           rarity,
				Variant:          variant,
				SourceImageIDs:   imageIDs,
				Metadata:         rawMetadata(card),
			}},
			Metadata: rawMetadata(card),
		}

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
