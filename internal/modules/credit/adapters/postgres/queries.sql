-- name: ListCards :many
-- Las tarjetas activas, en el orden en que se crearon (el caso de uso las
-- ordena por deuda y fecha límite).
SELECT id, name, currency, created_at
FROM credit_cards
WHERE user_id = $1 AND archived_at IS NULL
ORDER BY created_at, id;

-- name: InsertCard :execrows
INSERT INTO credit_cards (id, user_id, name, currency, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $5)
ON CONFLICT (id) DO NOTHING;

-- name: GetCard :one
SELECT id, user_id, name, currency, archived_at
FROM credit_cards
WHERE id = $1;

-- name: UpdateCard :execrows
UPDATE credit_cards
SET name = $3, currency = $4, updated_at = now()
WHERE id = $1 AND user_id = $2 AND archived_at IS NULL;

-- name: ArchiveCard :execrows
UPDATE credit_cards
SET archived_at = now(), updated_at = now()
WHERE id = $1 AND user_id = $2 AND archived_at IS NULL;

-- name: CountPendingByCard :one
SELECT count(*)
FROM credit_purchases
WHERE card_id = $1 AND user_id = $2 AND status = 'pending' AND deleted_at IS NULL;

-- name: ListPendingPurchases :many
-- Las compras pendientes con su tarjeta, de la fecha límite más próxima a la más lejana.
SELECT p.id, p.card_id, p.category_id, p.description, p.amount, p.currency,
       p.purchase_date, p.due_date, c.name AS card_name, c.currency AS card_currency
FROM credit_purchases p
JOIN credit_cards c ON c.id = p.card_id
WHERE p.user_id = $1 AND p.status = 'pending' AND p.deleted_at IS NULL
ORDER BY p.due_date, p.created_at, p.id;

-- name: InsertPurchase :execrows
INSERT INTO credit_purchases (id, user_id, card_id, category_id, description, amount, currency,
                              purchase_date, due_date, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
ON CONFLICT (id) DO NOTHING;

-- name: GetPurchase :one
SELECT id, user_id, card_id, category_id, description, amount, currency, purchase_date,
       due_date, status, deleted_at
FROM credit_purchases
WHERE id = $1;

-- name: UpdatePurchase :execrows
UPDATE credit_purchases
SET card_id = $3, category_id = $4, description = $5, amount = $6, currency = $7,
    due_date = $8, updated_at = now()
WHERE id = $1 AND user_id = $2 AND status = 'pending' AND deleted_at IS NULL;

-- name: DeletePurchase :execrows
UPDATE credit_purchases
SET deleted_at = now(), updated_at = now()
WHERE id = $1 AND user_id = $2 AND status = 'pending' AND deleted_at IS NULL;

-- name: LockPurchase :one
-- Bloquea la compra hasta el fin de la transacción: de dos pagos simultáneos,
-- el segundo espera y la encuentra ya pagada.
SELECT id, user_id, card_id, category_id, description, amount, currency, purchase_date,
       due_date, status, deleted_at
FROM credit_purchases
WHERE id = $1
FOR UPDATE;

-- name: MarkPurchasePaid :execrows
UPDATE credit_purchases
SET status = 'paid', paid_at = now(), payment_transaction_id = $3, updated_at = now()
WHERE id = $1 AND user_id = $2 AND status = 'pending' AND deleted_at IS NULL;

-- name: RevertPurchasePayment :one
-- La compra vuelve a pendiente; devuelve su tarjeta para mostrarla de nuevo.
UPDATE credit_purchases
SET status = 'pending', paid_at = NULL, payment_transaction_id = NULL, updated_at = now()
WHERE id = $1 AND user_id = $2 AND status = 'paid'
RETURNING card_id;

-- name: UnarchiveCard :exec
UPDATE credit_cards
SET archived_at = NULL, updated_at = now()
WHERE id = $1 AND user_id = $2 AND archived_at IS NOT NULL;
