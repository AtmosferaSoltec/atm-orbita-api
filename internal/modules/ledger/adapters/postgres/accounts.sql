-- Todas las cuentas activas con su saldo, en una sola consulta. Por ahora el
-- saldo es el inicial; los planes 05 y 06 suman aquí movimientos y transferencias.
-- name: ListAccounts :many
SELECT a.id, a.name, a.type, a.currency, a.include_in_savings,
       a.initial_balance::numeric AS balance
FROM accounts a
WHERE a.user_id = $1 AND a.archived_at IS NULL
ORDER BY a.sort_order, a.created_at, a.id;

-- Un id que ya existe no se pisa: el caso de uso decide si es un reintento.
-- name: InsertAccount :execrows
INSERT INTO accounts (id, user_id, name, type, currency, initial_balance, include_in_savings, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO NOTHING;

-- name: GetAccount :one
SELECT a.id, a.user_id, a.name, a.type, a.currency, a.initial_balance, a.include_in_savings,
       a.initial_balance::numeric AS balance, a.archived_at
FROM accounts a
WHERE a.id = $1;

-- name: UpdateAccount :execrows
UPDATE accounts
SET name = $3, type = $4, include_in_savings = $5, updated_at = now()
WHERE id = $1 AND user_id = $2 AND archived_at IS NULL;

-- name: ArchiveAccount :execrows
UPDATE accounts
SET archived_at = now(), updated_at = now()
WHERE id = $1 AND user_id = $2 AND archived_at IS NULL;
