-- Plan 02: las preferencias del usuario y el historial de sus tipos de cambio.

-- +goose Up
CREATE TABLE profiles (
    user_id            UUID        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    main_currency      CHAR(3)     NOT NULL,
    secondary_currency CHAR(3)     NOT NULL,
    -- En qué moneda se ve el ahorro total: siempre una de las dos.
    display_currency   CHAR(3)     NOT NULL,
    timezone           VARCHAR(64) NOT NULL,
    fx_mode            VARCHAR(10) NOT NULL DEFAULT 'manual',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT profiles_currency_format CHECK (
        main_currency ~ '^[A-Z]{3}$' AND secondary_currency ~ '^[A-Z]{3}$'
    ),
    CONSTRAINT profiles_distinct_currencies CHECK (main_currency <> secondary_currency),
    CONSTRAINT profiles_display_in_pair CHECK (display_currency IN (main_currency, secondary_currency)),
    CONSTRAINT profiles_fx_mode CHECK (fx_mode IN ('manual', 'auto'))
);

-- Un tipo de cambio que no existe es ausencia de fila, no un valor nulo. Cada
-- cambio agrega una fila: el historial no se pisa. user_id nulo = global (el
-- tipo de cambio automático, plan 14).
CREATE TABLE exchange_rates (
    id            UUID          PRIMARY KEY,
    user_id       UUID          REFERENCES users (id) ON DELETE CASCADE,
    from_currency CHAR(3)       NOT NULL,
    to_currency   CHAR(3)       NOT NULL,
    -- Unidades de to_currency por 1 de from_currency (1 US$ = S/ 3.20).
    rate          NUMERIC(18,6) NOT NULL,
    source        VARCHAR(10)   NOT NULL,
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT exchange_rates_rate_positive CHECK (rate > 0),
    CONSTRAINT exchange_rates_distinct CHECK (from_currency <> to_currency),
    CONSTRAINT exchange_rates_source CHECK (source IN ('manual', 'api')),
    CONSTRAINT exchange_rates_global_is_api CHECK (user_id IS NOT NULL OR source = 'api')
);
CREATE INDEX exchange_rates_lookup_idx
    ON exchange_rates (user_id, from_currency, to_currency, created_at DESC);

-- +goose Down
DROP TABLE exchange_rates;
DROP TABLE profiles;
