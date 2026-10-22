-- Plan 09: tarjetas de crédito y compras pendientes de pago.

-- +goose Up
CREATE TABLE credit_cards (
    id          UUID        PRIMARY KEY,
    user_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        VARCHAR(60) NOT NULL,
    -- Moneda de las compras nuevas; cambiarla no toca las que ya existen.
    currency    CHAR(3)     NOT NULL,
    archived_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT credit_cards_name_not_blank CHECK (length(btrim(name)) BETWEEN 1 AND 60),
    CONSTRAINT credit_cards_id_user_key UNIQUE (id, user_id)
);

CREATE INDEX credit_cards_user_idx ON credit_cards (user_id, created_at) WHERE archived_at IS NULL;

-- Una compra pendiente no es un movimiento: no toca saldos ni reportes hasta que
-- se paga (plan 10). La categoría es de ledger: sin clave foránea entre módulos;
-- credit la valida al crear y al editar, y las categorías nunca se borran.
CREATE TABLE credit_purchases (
    id            UUID          PRIMARY KEY,
    user_id       UUID          NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    card_id       UUID          NOT NULL,
    category_id   UUID          NOT NULL,
    description   VARCHAR(500)  NOT NULL DEFAULT '',
    amount        NUMERIC(14,2) NOT NULL,
    -- Copia de la moneda de la tarjeta al comprar.
    currency      CHAR(3)       NOT NULL,
    purchase_date DATE          NOT NULL,
    due_date      DATE          NOT NULL,
    status        VARCHAR(10)   NOT NULL DEFAULT 'pending',
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ,
    CONSTRAINT credit_purchases_amount_positive CHECK (amount > 0),
    CONSTRAINT credit_purchases_status CHECK (status IN ('pending', 'paid')),
    CONSTRAINT credit_purchases_due_after_purchase CHECK (due_date >= purchase_date),
    CONSTRAINT credit_purchases_card_fk FOREIGN KEY (card_id, user_id)
        REFERENCES credit_cards (id, user_id)
);

CREATE INDEX credit_purchases_pending_idx ON credit_purchases (user_id, due_date)
    WHERE status = 'pending' AND deleted_at IS NULL;

-- +goose Down
DROP TABLE credit_purchases;
DROP TABLE credit_cards;
