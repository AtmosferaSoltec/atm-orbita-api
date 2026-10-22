-- Plan 05: movimientos (ingresos y egresos).

-- +goose Up
CREATE TABLE transactions (
    id          UUID          PRIMARY KEY,
    user_id     UUID          NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    account_id  UUID          NOT NULL,
    category_id UUID          NOT NULL,
    -- Copia del tipo de la categoría, para sumar sin unir con categorías. La clave
    -- foránea de abajo impide que se desalinee.
    kind        VARCHAR(10)   NOT NULL,
    amount      NUMERIC(14,2) NOT NULL,
    -- El día ya calculado en la zona del perfil: sin valor por defecto, porque la
    -- fecha del servidor (UTC) puede ser otra.
    occurred_on DATE          NOT NULL,
    description VARCHAR(500)  NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT transactions_amount_positive CHECK (amount > 0),
    CONSTRAINT transactions_kind CHECK (kind IN ('income', 'expense')),
    -- Segunda barrera: aunque el código olvidara comprobarlo, un movimiento no
    -- puede apuntar a la cuenta ni a la categoría de otra persona, ni a una
    -- categoría de otro tipo.
    CONSTRAINT transactions_account_fk FOREIGN KEY (account_id, user_id)
        REFERENCES accounts (id, user_id),
    CONSTRAINT transactions_category_fk FOREIGN KEY (category_id, user_id, kind)
        REFERENCES categories (id, user_id, kind)
);

-- La lista, del más reciente al más antiguo.
CREATE INDEX transactions_user_date_idx ON transactions (user_id, occurred_on DESC, created_at DESC)
    WHERE deleted_at IS NULL;
-- Los saldos.
CREATE INDEX transactions_account_idx ON transactions (account_id) INCLUDE (kind, amount)
    WHERE deleted_at IS NULL;
-- Los reportes (plan 07).
CREATE INDEX transactions_user_category_idx ON transactions (user_id, category_id, occurred_on)
    WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE transactions;
