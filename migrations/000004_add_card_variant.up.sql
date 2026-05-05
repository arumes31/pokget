-- Add variant column to portfolio table
ALTER TABLE portfolio ADD COLUMN IF NOT EXISTS variant TEXT DEFAULT 'Normal';
