-- Plan 11: códigos de 6 dígitos para recuperar la contraseña.

-- +goose Up
CREATE TABLE password_reset_codes (
    id         UUID        PRIMARY KEY,
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- HMAC-SHA256 del código con una clave derivada del secreto del servidor:
    -- si la base se filtra, el millón de códigos posibles no se puede probar.
    code_hash  BYTEA       NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    -- Intentos fallidos; al quinto el código deja de servir.
    attempts   SMALLINT    NOT NULL DEFAULT 0,
    -- Usado, o reemplazado por uno pedido después.
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX password_reset_codes_active_idx ON password_reset_codes (user_id) WHERE used_at IS NULL;
-- Para contar los envíos de las últimas 24 horas.
CREATE INDEX password_reset_codes_user_created_idx ON password_reset_codes (user_id, created_at);

-- +goose Down
DROP TABLE password_reset_codes;
