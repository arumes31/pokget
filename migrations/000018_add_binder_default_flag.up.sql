ALTER TABLE binders ADD COLUMN is_default BOOLEAN DEFAULT FALSE;

-- Ensure only one default binder per user
CREATE UNIQUE INDEX idx_binders_user_id_is_default ON binders (user_id) WHERE (is_default = TRUE);

-- Create a default binder for every user who doesn't have one
-- Note: 'description' might not exist yet if migration 17 hasn't run, 
-- but in the summary it said 17 was added.
INSERT INTO binders (user_id, name, is_default, description)
SELECT id, 'Main Vault', TRUE, 'Primary repository for all scanned assets.'
FROM users
WHERE NOT EXISTS (
    SELECT 1 FROM binders WHERE user_id = users.id AND is_default = TRUE
);
