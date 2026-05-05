-- Add variant column to cards table
ALTER TABLE cards ADD COLUMN IF NOT EXISTS variant TEXT DEFAULT 'Normal';
