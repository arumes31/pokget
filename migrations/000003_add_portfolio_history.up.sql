-- Create table for tracking historical portfolio valuation
CREATE TABLE IF NOT EXISTS portfolio_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    valuation NUMERIC(15, 2) NOT NULL,
    recorded_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_portfolio_history_user_date ON portfolio_history(user_id, recorded_at);
