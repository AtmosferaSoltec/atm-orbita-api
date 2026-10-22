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

-- Una página de la lista: movimientos y transferencias mezclados, del más
-- reciente al más antiguo. Solo trae qué es cada fila; los detalles los leen
-- ListTransactionsByIDs y ListTransfersByIDs. account_id coincide con la cuenta
-- del movimiento o con cualquiera de las dos de la transferencia; pattern (ILIKE,
-- ya escapado) busca en la descripción o en la nota. Los dos son opcionales.
-- name: ListEntryKeys :many
SELECT e.entry_type::text AS entry_type, e.id::uuid AS id
FROM (
    SELECT 'transaction' AS entry_type, t.id, t.occurred_on, t.created_at
    FROM transactions t
    WHERE t.user_id = sqlc.arg(user_id) AND t.deleted_at IS NULL
      AND t.occurred_on BETWEEN sqlc.arg(date_from) AND sqlc.arg(date_to)
      AND (sqlc.narg(account_id)::uuid IS NULL OR t.account_id = sqlc.narg(account_id))
      AND (sqlc.narg(pattern)::text IS NULL OR t.description ILIKE sqlc.narg(pattern))
    UNION ALL
    SELECT 'transfer' AS entry_type, tr.id, tr.occurred_on, tr.created_at
    FROM transfers tr
    WHERE tr.user_id = sqlc.arg(user_id) AND tr.deleted_at IS NULL
      AND tr.occurred_on BETWEEN sqlc.arg(date_from) AND sqlc.arg(date_to)
      AND (sqlc.narg(account_id)::uuid IS NULL
           OR tr.from_account_id = sqlc.narg(account_id) OR tr.to_account_id = sqlc.narg(account_id))
      AND (sqlc.narg(pattern)::text IS NULL OR tr.note ILIKE sqlc.narg(pattern))
) e
ORDER BY e.occurred_on DESC, e.created_at DESC, e.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountEntries :one
SELECT (
    (SELECT count(*) FROM transactions t
     WHERE t.user_id = sqlc.arg(user_id) AND t.deleted_at IS NULL
       AND t.occurred_on BETWEEN sqlc.arg(date_from) AND sqlc.arg(date_to)
       AND (sqlc.narg(account_id)::uuid IS NULL OR t.account_id = sqlc.narg(account_id))
       AND (sqlc.narg(pattern)::text IS NULL OR t.description ILIKE sqlc.narg(pattern)))
  + (SELECT count(*) FROM transfers tr
     WHERE tr.user_id = sqlc.arg(user_id) AND tr.deleted_at IS NULL
       AND tr.occurred_on BETWEEN sqlc.arg(date_from) AND sqlc.arg(date_to)
       AND (sqlc.narg(account_id)::uuid IS NULL
            OR tr.from_account_id = sqlc.narg(account_id) OR tr.to_account_id = sqlc.narg(account_id))
       AND (sqlc.narg(pattern)::text IS NULL OR tr.note ILIKE sqlc.narg(pattern)))
)::bigint AS total;

-- Los movimientos pedidos por ListEntryKeys, con su cuenta y su categoría.
-- name: ListTransactionsByIDs :many
SELECT t.id, t.kind, t.amount, t.occurred_on, t.description,
       a.id AS account_id, a.name AS account_name, a.type AS account_type, a.currency AS account_currency,
       c.id AS category_id, c.name AS category_name, c.kind AS category_kind, c.color AS category_color
FROM transactions t
JOIN accounts a ON a.id = t.account_id
JOIN categories c ON c.id = t.category_id
WHERE t.user_id = sqlc.arg(user_id) AND t.id = ANY(sqlc.arg(ids)::uuid[]);

-- Para los reportes (plan 07): la suma de los movimientos de un periodo por
-- moneda de la cuenta, tipo y categoría, hecha en la base. Las categorías
-- archivadas siguen apareciendo con los movimientos que tuvieron.
-- name: TotalsByCategory :many
SELECT a.currency, t.kind, c.id AS category_id, c.name AS category_name, c.color AS category_color,
       SUM(t.amount)::numeric AS total, count(*) AS movements
FROM transactions t
JOIN accounts a ON a.id = t.account_id
JOIN categories c ON c.id = t.category_id
WHERE t.user_id = sqlc.arg(user_id) AND t.deleted_at IS NULL
  AND t.occurred_on BETWEEN sqlc.arg(date_from) AND sqlc.arg(date_to)
GROUP BY a.currency, t.kind, c.id, c.name, c.color;
