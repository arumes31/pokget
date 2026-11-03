// Command pricegaps reports market-price coverage of the catalog per game,
// language and rarity, and optionally probes every configured marketplace
// category.
//
// It answers the operational question "does every TCG/language/rarity tier have
// working prices?" after a bulk price cycle:
//
//	go run ./cmd/pricegaps                     # coverage report from the database
//	go run ./cmd/pricegaps -sources            # additionally probe TCGplayer categories
//	go run ./cmd/pricegaps -min 100 -table game  # only one dimension
//
// The database connection uses the usual DATABASE_* environment variables.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"pokget/internal/db"
	"pokget/internal/service"
)

func main() {
	minCards := flag.Int("min", 50, "only report groups with at least this many cards")
	table := flag.String("table", "all", "which dimension to report: all, game, language or rarity")
	probe := flag.Bool("sources", false, "probe every marketplace category for reachability")
	flag.Parse()

	var exitErr error
	if *table == "all" || *table == "game" || *table == "language" || *table == "rarity" {
		if err := reportCoverage(*table, *minCards); err != nil {
			exitErr = err
		}
	} else {
		exitErr = fmt.Errorf("unknown -table %q (want all, game, language or rarity)", *table)
	}

	if *probe && exitErr == nil {
		if err := probeCategories(); err != nil {
			exitErr = err
		}
	}
	if exitErr != nil {
		fmt.Fprintln(os.Stderr, "pricegaps:", exitErr)
		os.Exit(1)
	}
}

func reportCoverage(table string, minCards int) error {
	connection, err := db.Connect()
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = connection.Close() }()

	queries := []struct {
		name  string
		table string
		query string
	}{
		{
			name:  "game",
			table: "game",
			query: `SELECT COALESCE(NULLIF(game, ''), '(none)'), count(*),
			               count(*) FILTER (WHERE price_usd > 0),
			               count(*) FILTER (WHERE price_eur > 0),
			               count(*) FILTER (WHERE price_eur_estimated),
			               count(*) FILTER (WHERE COALESCE(price_usd, 0) = 0 AND COALESCE(price_eur, 0) = 0)
			        FROM cards WHERE superseded_by_card_id IS NULL
			        GROUP BY 1 HAVING count(*) >= $1 ORDER BY 2 DESC`,
		},
		{
			name:  "language",
			table: "language",
			query: `SELECT COALESCE(NULLIF(game, ''), '(none)') || ' / ' || COALESCE(NULLIF(language, ''), '(none)'),
			               count(*),
			               count(*) FILTER (WHERE price_usd > 0),
			               count(*) FILTER (WHERE price_eur > 0),
			               count(*) FILTER (WHERE price_eur_estimated),
			               count(*) FILTER (WHERE COALESCE(price_usd, 0) = 0 AND COALESCE(price_eur, 0) = 0)
			        FROM cards WHERE superseded_by_card_id IS NULL
			        GROUP BY 1 HAVING count(*) >= $1 ORDER BY 2 DESC`,
		},
		{
			name:  "rarity",
			table: "rarity",
			query: `SELECT COALESCE(NULLIF(game, ''), '(none)') || ' / ' || COALESCE(NULLIF(rarity, ''), '(none)'),
			               count(*),
			               count(*) FILTER (WHERE price_usd > 0),
			               count(*) FILTER (WHERE price_eur > 0),
			               count(*) FILTER (WHERE price_eur_estimated),
			               count(*) FILTER (WHERE COALESCE(price_usd, 0) = 0 AND COALESCE(price_eur, 0) = 0)
			        FROM cards WHERE superseded_by_card_id IS NULL
			        GROUP BY 1 HAVING count(*) >= $1 ORDER BY 1`,
		},
	}

	for _, entry := range queries {
		if table != "all" && table != entry.table {
			continue
		}
		if err := printCoverage(connection, entry.name, entry.query, minCards); err != nil {
			return err
		}
	}
	return nil
}

func printCoverage(connection *sql.DB, name, query string, minCards int) error {
	rows, err := connection.Query(query, minCards)
	if err != nil {
		return fmt.Errorf("%s coverage: %w", name, err)
	}
	defer func() { _ = rows.Close() }()

	fmt.Printf("\nprice coverage by %s\n", name)
	fmt.Printf("%-46s %9s %9s %9s %9s %9s %6s\n", "GROUP", "CARDS", "USD", "EUR", "EUR-FX", "NONE", "EUR%")
	unpriced := 0
	for rows.Next() {
		var (
			group                          string
			cards, usd, eur, eurFX, none   int
		)
		if err := rows.Scan(&group, &cards, &usd, &eur, &eurFX, &none); err != nil {
			return fmt.Errorf("%s coverage scan: %w", name, err)
		}
		percent := 0.0
		if cards > 0 {
			percent = float64(eur) * 100 / float64(cards)
		}
		if none > 0 {
			unpriced += none
		}
		fmt.Printf("%-46s %9d %9d %9d %9d %9d %5.1f%%\n", group, cards, usd, eur, eurFX, none, percent)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%s coverage read: %w", name, err)
	}
	if unpriced > 0 {
		fmt.Printf("cards without any market price: %d\n", unpriced)
	}
	return nil
}

// probeCategories checks that every configured marketplace category is reachable
// and reports how many sets it publishes.
func probeCategories() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	categories := service.PriceCategories()
	sort.Slice(categories, func(i, j int) bool { return categories[i].Game < categories[j].Game })
	fmt.Printf("\nmarketplace categories (TCGCSV)\n")
	fmt.Printf("%-22s %-6s %-9s %s\n", "GAME", "LANG", "CATEGORY", "GROUPS")
	failures := 0
	for _, category := range categories {
		language := category.Language
		if language == "" {
			language = "any"
		}
		count, err := countGroups(ctx, category.Category)
		if err != nil {
			failures++
			fmt.Printf("%-22s %-6s %-9d ERROR %v\n", category.Game, language, category.Category, err)
			continue
		}
		fmt.Printf("%-22s %-6s %-9d %d\n", category.Game, language, category.Category, count)
	}
	if failures > 0 {
		return fmt.Errorf("%d marketplace categories failed", failures)
	}
	return nil
}

func countGroups(ctx context.Context, category int) (int, error) {
	url := fmt.Sprintf("https://tcgcsv.com/tcgplayer/%d/groups", category)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("User-Agent", "pokget-pricegaps/1.0 (+https://github.com/arumes31/pokget)")
	response, err := (&http.Client{Timeout: 60 * time.Second}).Do(request)
	if err != nil {
		return 0, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	var payload struct {
		Results []struct {
			GroupID int `json:"groupId"`
		} `json:"results"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return 0, err
	}
	return len(payload.Results), nil
}
