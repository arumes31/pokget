package source

import (
	"context"
	"fmt"
	"strings"

	"pokget/internal/catalog"
)

// DuelMastersProvider imports the classic Duel Masters TCG card catalog.
type DuelMastersProvider struct {
	HTTP    HTTPOptions
	BaseURL string
}

func (p *DuelMastersProvider) ID() string         { return "duel_masters" }
func (p *DuelMastersProvider) Game() catalog.Game { return catalog.GameDuelMasters }

type dmCardPrinting struct {
	Set         string `json:"set"`
	ID          string `json:"id"`
	Rarity      string `json:"rarity"`
	Illustrator string `json:"illustrator"`
	Flavor      string `json:"flavor"`
}

type dmCard struct {
	Name          string           `json:"name"`
	Civilizations []string         `json:"civilizations"`
	Cost          any              `json:"cost"`
	Power         string           `json:"power"`
	Type          string           `json:"type"`
	Text          string           `json:"text"`
	Printings     []dmCardPrinting `json:"printings"`
}

type dmDatabase struct {
	Cards []dmCard `json:"cards"`
}

func (p *DuelMastersProvider) Fetch(ctx context.Context, request catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://raw.githubusercontent.com/Latepate64/duel-masters-json/master"
	}
	endpoint := baseURL + "/DuelMastersCards.json"

	var db dmDatabase
	meta, err := getJSON(ctx, p.HTTP, endpoint, request, &db)
	if err != nil {
		return catalog.FetchResult{}, err
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

	seen := make(map[string]struct{})
	for _, card := range db.Cards {
		name := strings.TrimSpace(card.Name)
		if name == "" {
			continue
		}

		if len(card.Printings) == 0 {
			card.Printings = []dmCardPrinting{{Set: "Duel Masters", ID: "1", Rarity: "Common"}}
		}

		for idx, printing := range card.Printings {
			setCode := dmExtractSetCode(printing.Set)
			collectorNum := strings.TrimSpace(printing.ID)
			if collectorNum == "" {
				collectorNum = fmt.Sprintf("%d", idx+1)
			}
			sourceID := fmt.Sprintf("%s-%s", setCode, collectorNum)
			if _, exists := seen[sourceID]; exists {
				sourceID = fmt.Sprintf("%s-%s-%d", setCode, collectorNum, idx)
			}
			seen[sourceID] = struct{}{}

			rarity := strings.TrimSpace(printing.Rarity)
			if rarity == "" {
				rarity = "Normal"
			}

			record := catalog.CardRecord{
				SourceCardID:    sourceID,
				Name:            name,
				SetCode:         setCode,
				SetName:         printing.Set,
				CollectorNumber: collectorNum,
				Language:        "en",
				Rarity:          rarity,
				Metadata:        rawMetadata(card),
				Printings: []catalog.PrintingRecord{{
					SourcePrintingID: sourceID,
					SetCode:          setCode,
					SetName:          printing.Set,
					CollectorNumber:  collectorNum,
					Language:         "en",
					Rarity:           rarity,
					Metadata:         rawMetadata(printing),
				}},
			}

			if err := record.Validate(); err != nil {
				return result, err
			}
			if err := emit(record); err != nil {
				return result, err
			}
			result.Count++
		}
	}

	return result, nil
}

func dmExtractSetCode(setName string) string {
	parts := strings.Fields(setName)
	if len(parts) > 0 && strings.HasPrefix(parts[0], "DM-") {
		return parts[0]
	}
	if len(parts) > 0 {
		return parts[0]
	}
	return "DM"
}
