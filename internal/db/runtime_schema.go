package db

import (
	"context"
	"database/sql"
	"fmt"
)

const runtimeSchemaQuery = `
WITH required(table_name, column_name) AS (
    VALUES
        ('cards', 'id'),
        ('cards', 'name'),
        ('cards', 'set_name'),
        ('cards', 'image_url'),
        ('cards', 'price_usd'),
        ('cards', 'price_eur'),
        ('cards', 'variant'),
        ('cards', 'change_24h'),
        ('cards', 'phash'),
        ('cards', 'game'),
        ('cards', 'language'),
        ('cards', 'rarity'),
        ('cards', 'source_id'),
        ('cards', 'source_card_id'),
        ('cards', 'set_code'),
        ('cards', 'collector_number'),
        ('cards', 'source_updated_at'),
        ('cards', 'source_metadata'),
        ('cards', 'catalog_active'),
        ('cards', 'first_seen_at'),
        ('cards', 'last_seen_at'),
        ('cards', 'last_seen_run_id'),
        ('cards', 'superseded_by_card_id'),
        ('price_history', 'card_id'),
        ('price_history', 'price_usd'),
        ('price_history', 'price_eur'),
        ('price_history', 'recorded_at'),
        ('price_alerts', 'id'),
        ('price_alerts', 'user_id'),
        ('price_alerts', 'card_id'),
        ('price_alerts', 'target_price'),
        ('price_alerts', 'is_active'),
        ('catalog_sources', 'id'),
        ('catalog_source_state', 'source_id'),
        ('catalog_sync_runs', 'id'),
        ('catalog_printings', 'id'),
        ('card_images', 'id'),
        ('card_fingerprints', 'image_id')
), missing AS (
    SELECT required.table_name, required.column_name
    FROM required
    LEFT JOIN information_schema.columns AS present
      ON present.table_schema = 'public'
     AND present.table_name = required.table_name
     AND present.column_name = required.column_name
    WHERE present.column_name IS NULL
)
SELECT string_agg(table_name || '.' || column_name, ', ' ORDER BY table_name, column_name)
FROM missing`

// ValidateRuntimeSchema ensures migrations produced the objects required by
// card scanning, catalog synchronization, and price synchronization.
func ValidateRuntimeSchema(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return fmt.Errorf("database connection is not initialized")
	}
	if ctx == nil {
		return fmt.Errorf("runtime schema validation context is nil")
	}

	var missing sql.NullString
	if err := database.QueryRowContext(ctx, runtimeSchemaQuery).Scan(&missing); err != nil {
		return fmt.Errorf("validate runtime schema: %w", err)
	}
	if missing.Valid && missing.String != "" {
		return fmt.Errorf("runtime schema is incomplete; missing: %s", missing.String)
	}
	return nil
}
