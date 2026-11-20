package source

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"pokget/internal/catalog"
)

const DefaultLanguages = "en,de,ja,fr,zh-cn,zh-tw,ko,es"

func catalogLanguages(value string) []string {
	var languages []string
	for _, language := range strings.Split(value, ",") {
		language = strings.ToLower(strings.TrimSpace(language))
		if language == "spanish" {
			language = "es"
		}
		if language != "" && !slices.Contains(languages, language) {
			languages = append(languages, language)
		}
	}
	if len(languages) == 0 {
		return []string{"en"}
	}
	return languages
}

// A source has one snapshot and one validator. Fetch every language without
// conditional headers so an unchanged subset cannot deactivate another subset.
// A partial or empty language keeps the later languages syncing but makes the
// whole snapshot partial, so nothing of that language is deactivated.
func fetchLanguages(ctx context.Context, languages []string, request catalog.FetchRequest, fetch func(context.Context, string, catalog.FetchRequest) (catalog.FetchResult, error)) (catalog.FetchResult, error) {
	result := catalog.FetchResult{}
	partial := false
	for _, language := range languages {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		part, err := fetch(ctx, language, catalog.FetchRequest{Mode: request.Mode})
		if err != nil {
			return result, fmt.Errorf("catalog language %s: %w", language, err)
		}
		// The part's records were already emitted, so count them even when partial.
		result.Count += part.Count
		if part.NotModified || !part.CompleteSnapshot || part.Count == 0 {
			slog.Warn("Catalog: language snapshot is partial", "language", language,
				"records", part.Count, "not_modified", part.NotModified)
			partial = true
		}
	}
	result.CompleteSnapshot = !partial
	return result, nil
}
