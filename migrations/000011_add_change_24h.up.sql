-- Add change_24h column to cards table
ALTER TABLE cards ADD COLUMN IF NOT EXISTS change_24h NUMERIC(5, 2) DEFAULT 0.00;
