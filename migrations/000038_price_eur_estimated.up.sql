-- Not every marketplace publishes EUR prices (Cardmarket is not crawlable for
-- most TCGs), so the worker derives EUR from the USD market price with the daily
-- ECB reference rate. This flag marks those values as converted instead of
-- scraped, so neither the UI nor an operator mistakes them for a Cardmarket quote.
ALTER TABLE cards ADD COLUMN IF NOT EXISTS price_eur_estimated BOOLEAN NOT NULL DEFAULT FALSE;
