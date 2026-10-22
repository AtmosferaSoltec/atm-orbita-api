-- Plan 06: transferencias entre dos cuentas propias.

-- +goose Up
CREATE TABLE transfers (
    id              UUID          PRIMARY KEY,
    user_id         UUID          NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    from_account_id UUID          NOT NULL,
    to_account_id   UUID          NOT NULL,
    -- Lo que mueve los saldos son los dos montos; el tipo de cambio es informativo.
    from_amount     NUMERIC(14,2) NOT NULL,
    to_amount       NUMERIC(14,2) NOT NULL,
    exchange_rate   NUMERIC(18,6),
    occurred_on     DATE          NOT NULL,
    note            VARCHAR(500)  NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ,
    CONSTRAINT transfers_amounts_positive CHECK (from_amount > 0 AND to_amount > 0),
    CONSTRAINT transfers_rate_positive CHECK (exchange_rate IS NULL OR exchange_rate > 0),
    CONSTRAINT transfers_distinct_accounts CHECK (from_account_id <> to_account_id),
    -- Las dos cuentas son de la misma persona que la transferencia.
    CONSTRAINT transfers_from_account_fk FOREIGN KEY (from_account_id, user_id)
        REFERENCES accounts (id, user_id),
    CONSTRAINT transfers_to_account_fk FOREIGN KEY (to_account_id, user_id)
        REFERENCES accounts (id, user_id)
);

CREATE INDEX transfers_user_date_idx ON transfers (user_id, occurred_on DESC, created_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX transfers_from_account_idx ON transfers (from_account_id) INCLUDE (from_amount)
    WHERE deleted_at IS NULL;
CREATE INDEX transfers_to_account_idx ON transfers (to_account_id) INCLUDE (to_amount)
    WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE transfers;
