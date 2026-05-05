-- Add game and rarity to cards table
ALTER TABLE cards ADD COLUMN IF NOT EXISTS game TEXT DEFAULT 'Pokemon';
ALTER TABLE cards ADD COLUMN IF NOT EXISTS rarity TEXT;

-- Update current cards to have Pokemon game
UPDATE cards SET game = 'Pokemon' WHERE game IS NULL;
