package source

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"pokget/internal/catalog"
)

type ScryfallProvider struct {
	HTTP        HTTPOptions
	ManifestURL string
	BulkType    string
}

func (p *ScryfallProvider) ID() string         { return "scryfall" }
func (p *ScryfallProvider) Game() catalog.Game { return catalog.GameMagic }

type scryfallBulkList struct {
	Data []scryfallBulk `json:"data"`
}

type scryfallBulk struct {
	Type             string `json:"type"`
	UpdatedAt        string `json:"updated_at"`
	JSONLDownloadURI string `json:"jsonl_download_uri"`
}

type scryfallCard struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Language        string            `json:"lang"`
	SetCode         string            `json:"set"`
	SetName         string            `json:"set_name"`
	CollectorNumber string            `json:"collector_number"`
	Rarity          string            `json:"rarity"`
	ReleasedAt      string            `json:"released_at"`
	Games           []string          `json:"games"`
	ImageURIs       map[string]string `json:"image_uris"`
	Faces           []struct {
		Name      string            `json:"name"`
		ImageURIs map[string]string `json:"image_uris"`
	} `json:"card_faces"`
}

func (p *ScryfallProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	manifestURL := p.ManifestURL
	if manifestURL == "" {
		manifestURL = "https://api.scryfall.com/bulk-data"
	}
	bulkType := p.BulkType
	if bulkType == "" {
		bulkType = "all_cards"
	}
	var list scryfallBulkList
	meta, err := getJSON(ctx, p.HTTP, manifestURL, request, &list)
	if err != nil {
		return catalog.FetchResult{}, err
	}
	result := catalog.FetchResult{ETag: meta.ETag, LastModified: meta.LastModified, CompleteSnapshot: true, NotModified: meta.NotModified}
	if meta.NotModified {
		return result, nil
	}
	var selected *scryfallBulk
	for i := range list.Data {
		if list.Data[i].Type == bulkType {
			selected = &list.Data[i]
			break
		}
	}
	if selected == nil || selected.JSONLDownloadURI == "" {
		return result, fmt.Errorf("scryfall: bulk type %q has no JSONL download", bulkType)
	}
	result.UpstreamVersion = selected.UpdatedAt
	if request.UpstreamVersion != "" && request.UpstreamVersion == result.UpstreamVersion {
		result.NotModified = true
		return result, nil
	}

	req, err := p.HTTP.newRequest(ctx, http.MethodGet, selected.JSONLDownloadURI, catalog.FetchRequest{})
	if err != nil {
		return result, err
	}
	resp, err := p.HTTP.client().Do(req)
	if err != nil {
		return result, fmt.Errorf("scryfall: download bulk data: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("scryfall: bulk download returned %s", resp.Status)
	}

	compressed := io.LimitReader(resp.Body, p.HTTP.maxBodyBytes()+1)
	gzipReader, err := gzip.NewReader(compressed)
	if err != nil {
		return result, fmt.Errorf("scryfall: open gzip bulk data: %w", err)
	}
	defer gzipReader.Close()
	scanner := bufio.NewScanner(gzipReader)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		var card scryfallCard
		if err := json.Unmarshal(scanner.Bytes(), &card); err != nil {
			return result, fmt.Errorf("scryfall: decode JSONL record %d: %w", result.Count+1, err)
		}
		if !containsString(card.Games, "paper") {
			continue
		}
		record := catalog.CardRecord{
			SourceCardID:    card.ID,
			Name:            card.Name,
			SetCode:         card.SetCode,
			SetName:         card.SetName,
			CollectorNumber: card.CollectorNumber,
			Language:        card.Language,
			Rarity:          card.Rarity,
			ReleasedAt:      parseDate(card.ReleasedAt),
			Metadata:        append(json.RawMessage(nil), scanner.Bytes()...),
		}
		if imageURL := preferredScryfallImage(card.ImageURIs); imageURL != "" {
			record.Images = append(record.Images, catalog.ImageRecord{SourceImageID: card.ID + ":front", Kind: "front", URL: imageURL})
		}
		for faceIndex, face := range card.Faces {
			if imageURL := preferredScryfallImage(face.ImageURIs); imageURL != "" {
				record.Images = append(record.Images, catalog.ImageRecord{
					SourceImageID: fmt.Sprintf("%s:face:%d", card.ID, faceIndex),
					Kind:          fmt.Sprintf("face_%d", faceIndex),
					URL:           imageURL,
				})
			}
		}
		record.Printings = []catalog.PrintingRecord{{
			SourcePrintingID: card.ID,
			SetCode:          card.SetCode,
			SetName:          card.SetName,
			CollectorNumber:  card.CollectorNumber,
			Rarity:           card.Rarity,
			Language:         card.Language,
			ReleasedAt:       record.ReleasedAt,
			SourceImageIDs:   imageIDs(record.Images),
		}}
		if err := record.Validate(); err != nil {
			return result, err
		}
		if err := emit(record); err != nil {
			return result, err
		}
		result.Count++
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("scryfall: read JSONL bulk data: %w", err)
	}
	return result, nil
}

func preferredScryfallImage(images map[string]string) string {
	for _, key := range []string{"normal", "large", "png", "small"} {
		if strings.TrimSpace(images[key]) != "" {
			return images[key]
		}
	}
	return ""
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
