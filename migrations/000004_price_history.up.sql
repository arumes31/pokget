-- Create price_history table
CREATE TABLE IF NOT EXISTS price_history (
    id SERIAL PRIMARY KEY,
    card_id TEXT NOT NULL,
    price_usd DECIMAL(12,2),
    price_eur DECIMAL(12,2),
    recorded_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Add price_alerts table (Improvement #38)
CREATE TABLE IF NOT EXISTS price_alerts (
    id SERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    card_id TEXT NOT NULL,
    target_price DECIMAL(12,2) NOT NULL,
    currency TEXT DEFAULT 'USD',
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
