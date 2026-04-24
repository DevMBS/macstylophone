CREATE TABLE IF NOT EXISTS schema_migrations (
    name TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS auth_users (
    id UUID PRIMARY KEY,
    nickname TEXT NOT NULL,
    nickname_normalized TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL,
    email_normalized TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    google_subject TEXT NOT NULL UNIQUE,
    google_email TEXT NOT NULL,
    google_email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    google_name TEXT,
    google_picture_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS auth_users_email_normalized_idx ON auth_users (email_normalized);
CREATE INDEX IF NOT EXISTS auth_users_nickname_normalized_idx ON auth_users (nickname_normalized);
