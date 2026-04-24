ALTER TABLE auth_users
    ADD COLUMN IF NOT EXISTS synth_configs JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS melodies JSONB NOT NULL DEFAULT '[]'::jsonb;
