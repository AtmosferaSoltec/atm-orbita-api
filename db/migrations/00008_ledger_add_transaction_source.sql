-- Plan 10: de dónde viene un movimiento. El egreso del pago de una compra con
-- tarjeta apunta a esa compra (source_id), que es de credit: sin clave foránea
-- entre módulos.

-- +goose Up
ALTER TABLE transactions
    ADD COLUMN source    VARCHAR(20) NOT NULL DEFAULT 'manual',
    ADD COLUMN source_id UUID;

ALTER TABLE transactions
    ADD CONSTRAINT transactions_source CHECK (source IN ('manual', 'credit_payment')),
    ADD CONSTRAINT transactions_source_id CHECK ((source = 'credit_payment') = (source_id IS NOT NULL));

-- Una compra tiene a lo sumo un egreso de pago vivo.
CREATE UNIQUE INDEX transactions_credit_payment_key ON transactions (source_id)
    WHERE source = 'credit_payment' AND deleted_at IS NULL;

-- +goose Down
DROP INDEX transactions_credit_payment_key;
ALTER TABLE transactions
    DROP CONSTRAINT transactions_source_id,
    DROP CONSTRAINT transactions_source,
    DROP COLUMN source_id,
    DROP COLUMN source;
