-- Un id que ya existe no se pisa: el caso de uso decide si es un reintento.
-- name: InsertTransaction :execrows
INSERT INTO transactions (id, user_id, account_id, category_id, kind, amount, occurred_on, description, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
ON CONFLICT (id) DO NOTHING;

-- name: GetTransaction :one
SELECT id, user_id, account_id, category_id, kind, amount, occurred_on, description, deleted_at
FROM transactions
WHERE id = $1;

-- name: UpdateTransaction :execrows
UPDATE transactions
SET account_id = $3, category_id = $4, kind = $5, amount = $6, occurred_on = $7,
    description = $8, updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: DeleteTransaction :execrows
UPDATE transactions
SET deleted_at = now(), updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- Los movimientos de un periodo, con su cuenta y su categoría, del más reciente
-- al más antiguo. account_id y pattern (ILIKE, ya escapado) son opcionales.
-- name: ListEntries :many
SELECT t.id, t.kind, t.amount, t.occurred_on, t.description,
       a.id AS account_id, a.name AS account_name, a.type AS account_type, a.currency AS account_currency,
       c.id AS category_id, c.name AS category_name, c.kind AS category_kind, c.color AS category_color
FROM transactions t
JOIN accounts a ON a.id = t.account_id
JOIN categories c ON c.id = t.category_id
WHERE t.user_id = sqlc.arg(user_id) AND t.deleted_at IS NULL
  AND t.occurred_on BETWEEN sqlc.arg(date_from) AND sqlc.arg(date_to)
  AND (sqlc.narg(account_id)::uuid IS NULL OR t.account_id = sqlc.narg(account_id))
  AND (sqlc.narg(pattern)::text IS NULL OR t.description ILIKE sqlc.narg(pattern))
ORDER BY t.occurred_on DESC, t.created_at DESC, t.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountEntries :one
SELECT count(*)
FROM transactions t
WHERE t.user_id = sqlc.arg(user_id) AND t.deleted_at IS NULL
  AND t.occurred_on BETWEEN sqlc.arg(date_from) AND sqlc.arg(date_to)
  AND (sqlc.narg(account_id)::uuid IS NULL OR t.account_id = sqlc.narg(account_id))
  AND (sqlc.narg(pattern)::text IS NULL OR t.description ILIKE sqlc.narg(pattern));
