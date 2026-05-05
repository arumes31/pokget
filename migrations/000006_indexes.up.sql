-- Add indexes for performance optimization
CREATE INDEX IF NOT EXISTS idx_cards_game ON cards(game);
CREATE INDEX IF NOT EXISTS idx_cards_name ON cards(name);
CREATE INDEX IF NOT EXISTS idx_portfolio_user_id ON portfolio(user_id);
CREATE INDEX IF NOT EXISTS idx_price_history_card_id ON price_history(card_id);
CREATE INDEX IF NOT EXISTS idx_price_alerts_user_id ON price_alerts(user_id);
