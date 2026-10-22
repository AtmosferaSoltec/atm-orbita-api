-- Todas las cuentas activas con su saldo, en una sola consulta: saldo inicial +
-- ingresos − egresos (el plan 06 suma aquí las transferencias).
-- name: ListAccounts :many
SELECT a.id, a.name, a.type, a.currency, a.include_in_savings,
       (a.initial_balance + COALESCE(t.net, 0))::numeric AS balance
FROM accounts a
LEFT JOIN (
    SELECT tx.account_id, SUM(CASE WHEN tx.kind = 'income' THEN tx.amount ELSE -tx.amount END) AS net
    FROM transactions tx
    WHERE tx.user_id = sqlc.arg(user_id) AND tx.deleted_at IS NULL
    GROUP BY tx.account_id
) t ON t.account_id = a.id
WHERE a.user_id = sqlc.arg(user_id) AND a.archived_at IS NULL
ORDER BY a.sort_order, a.created_at, a.id;

-- Un id que ya existe no se pisa: el caso de uso decide si es un reintento.
-- name: InsertAccount :execrows
INSERT INTO accounts (id, user_id, name, type, currency, initial_balance, include_in_savings, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO NOTHING;

-- name: GetAccount :one
SELECT a.id, a.user_id, a.name, a.type, a.currency, a.initial_balance, a.include_in_savings,
       (a.initial_balance + COALESCE((
           SELECT SUM(CASE WHEN t.kind = 'income' THEN t.amount ELSE -t.amount END)
           FROM transactions t
           WHERE t.account_id = a.id AND t.deleted_at IS NULL
       ), 0))::numeric AS balance,
       a.archived_at
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
