-- Plan 10: una compra pagada apunta al egreso de su pago (de ledger: sin clave
-- foránea entre módulos). La cuenta, el monto y la fecha del pago viven en ese
-- egreso, que es la fuente de verdad: no se copian aquí.

-- +goose Up
ALTER TABLE credit_purchases
    ADD COLUMN paid_at                TIMESTAMPTZ,
    ADD COLUMN payment_transaction_id UUID;

ALTER TABLE credit_purchases
    ADD CONSTRAINT credit_purchases_paid_has_payment
        CHECK ((status = 'paid') = (payment_transaction_id IS NOT NULL));

-- +goose Down
ALTER TABLE credit_purchases
    DROP CONSTRAINT credit_purchases_paid_has_payment,
    DROP COLUMN payment_transaction_id,
    DROP COLUMN paid_at;
