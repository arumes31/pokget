package source

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"pokget/internal/catalog"
)

const DefaultLanguages = "en,de,ja,fr,zh-cn,zh-tw,ko"

func catalogLanguages(value string) []string {
	var languages []string
	for _, language := range strings.Split(value, ",") {
		language = strings.ToLower(strings.TrimSpace(language))
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
func fetchLanguages(ctx context.Context, languages []string, request catalog.FetchRequest, fetch func(context.Context, string, catalog.FetchRequest) (catalog.FetchResult, error)) (catalog.FetchResult, error) {
	result := catalog.FetchResult{}
	for _, language := range languages {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		part, err := fetch(ctx, language, catalog.FetchRequest{Mode: request.Mode})
		if err != nil {
			return result, fmt.Errorf("catalog language %s: %w", language, err)
		}
		if part.NotModified || !part.CompleteSnapshot || part.Count == 0 {
			return result, fmt.Errorf("catalog language %s: incomplete snapshot", language)
		}
		result.Count += part.Count
	}
	result.CompleteSnapshot = true
	return result, nil
}
