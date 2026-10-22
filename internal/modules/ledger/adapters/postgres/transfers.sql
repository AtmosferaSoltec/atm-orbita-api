-- Un id que ya existe no se pisa: el caso de uso decide si es un reintento.
-- name: InsertTransfer :execrows
INSERT INTO transfers (id, user_id, from_account_id, to_account_id, from_amount, to_amount,
                       exchange_rate, occurred_on, note, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
ON CONFLICT (id) DO NOTHING;

-- name: GetTransfer :one
SELECT id, user_id, from_account_id, to_account_id, from_amount, to_amount, exchange_rate,
       occurred_on, note, deleted_at
FROM transfers
WHERE id = $1;

-- name: UpdateTransfer :execrows
UPDATE transfers
SET from_account_id = $3, to_account_id = $4, from_amount = $5, to_amount = $6,
    exchange_rate = $7, occurred_on = $8, note = $9, updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: DeleteTransfer :execrows
UPDATE transfers
SET deleted_at = now(), updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- Las transferencias pedidas por ListEntryKeys, con sus dos cuentas.
-- name: ListTransfersByIDs :many
SELECT tr.id, tr.from_amount, tr.to_amount, tr.exchange_rate, tr.occurred_on, tr.note,
       fa.id AS from_id, fa.name AS from_name, fa.type AS from_type, fa.currency AS from_currency,
       ta.id AS to_id, ta.name AS to_name, ta.type AS to_type, ta.currency AS to_currency
FROM transfers tr
JOIN accounts fa ON fa.id = tr.from_account_id
JOIN accounts ta ON ta.id = tr.to_account_id
WHERE tr.user_id = sqlc.arg(user_id) AND tr.id = ANY(sqlc.arg(ids)::uuid[]);
