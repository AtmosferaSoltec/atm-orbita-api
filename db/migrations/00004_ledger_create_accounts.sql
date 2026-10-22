-- Plan 04: cuentas (efectivo, débito, ahorro, otra), cada una en una sola moneda.

-- +goose Up
CREATE TABLE accounts (
    id                 UUID          PRIMARY KEY,
    user_id            UUID          NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name               VARCHAR(60)   NOT NULL,
    type               VARCHAR(10)   NOT NULL,
    currency           CHAR(3)       NOT NULL,
    -- Sin CHECK de signo: que no sea negativo lo valida el API al crear, y los
    -- datos de ejemplo de Android necesitan uno negativo. El saldo no se guarda:
    -- se calcula a partir de este valor y de los movimientos (planes 05 y 06).
    initial_balance    NUMERIC(14,2) NOT NULL DEFAULT 0,
    include_in_savings BOOLEAN       NOT NULL DEFAULT true,
    sort_order         INT           NOT NULL DEFAULT 0,
    archived_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT accounts_type CHECK (type IN ('cash', 'debit', 'savings', 'other')),
    CONSTRAINT accounts_name_not_blank CHECK (length(btrim(name)) BETWEEN 1 AND 60),
    -- Para las claves foráneas compuestas de movimientos, transferencias y pagos
    -- (planes 05, 06 y 10): nadie apunta a la cuenta de otra persona.
    CONSTRAINT accounts_id_user_key UNIQUE (id, user_id)
);

CREATE INDEX accounts_user_idx ON accounts (user_id, sort_order, created_at) WHERE archived_at IS NULL;

-- Los usuarios que ya existían reciben "Efectivo" en su moneda principal, como
-- si acabaran de registrarse.
INSERT INTO accounts (id, user_id, name, type, currency)
SELECT gen_random_uuid(), p.user_id, 'Efectivo', 'cash', p.main_currency
FROM profiles p;

-- +goose Down
DROP TABLE accounts;
