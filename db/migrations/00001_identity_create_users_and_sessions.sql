-- Plan 01: cuentas de usuario y sesiones renovables.

-- +goose Up
CREATE TABLE users (
    id            UUID         PRIMARY KEY,
    -- Guardado ya recortado y en minúsculas: así el índice único no distingue mayúsculas.
    email         VARCHAR(254) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ,
    CONSTRAINT users_email_lowercase CHECK (email = lower(email))
);
CREATE UNIQUE INDEX users_email_key ON users (email);

-- Una por dispositivo. El access token lleva su id (sid) y el middleware
-- comprueba en cada petición que no esté revocada.
CREATE TABLE sessions (
    id             UUID         PRIMARY KEY,
    user_id        UUID         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_used_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    revoked_at     TIMESTAMPTZ,
    revoked_reason VARCHAR(30),
    user_agent     VARCHAR(255)
);
CREATE INDEX sessions_user_active_idx ON sessions (user_id) WHERE revoked_at IS NULL;

-- Solo el hash SHA-256: el token en claro solo existe en el teléfono.
CREATE TABLE refresh_tokens (
    id         UUID        PRIMARY KEY,
    session_id UUID        NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    token_hash BYTEA       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);
CREATE UNIQUE INDEX refresh_tokens_token_hash_key ON refresh_tokens (token_hash);
CREATE INDEX refresh_tokens_session_idx ON refresh_tokens (session_id);

-- +goose Down
DROP TABLE refresh_tokens;
DROP TABLE sessions;
DROP TABLE users;
