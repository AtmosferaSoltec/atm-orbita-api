-- +goose Up
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;
ALTER TABLE sessions ADD COLUMN is_web BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE external_identities (
    provider VARCHAR(30) NOT NULL,
    subject VARCHAR(255) NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject),
    UNIQUE (user_id, provider)
);

CREATE TABLE google_login_challenges (
    nonce_hash BYTEA PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX google_login_challenges_expiry_idx ON google_login_challenges(expires_at);

-- +goose Down
DROP TABLE google_login_challenges;
DROP TABLE external_identities;
ALTER TABLE sessions DROP COLUMN is_web;
-- Older binaries expect a hash. This sentinel cannot verify as a password.
UPDATE users SET password_hash = '' WHERE password_hash IS NULL;
ALTER TABLE users ALTER COLUMN password_hash SET NOT NULL;
