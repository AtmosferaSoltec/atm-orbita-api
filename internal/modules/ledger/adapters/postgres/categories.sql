-- En el orden en que se crearon: las iniciales quedan como en la app
-- (Alimentación primero) y las nuevas, al final.
-- name: ListCategories :many
SELECT id, name, kind, color
FROM categories
WHERE user_id = $1 AND archived_at IS NULL
ORDER BY created_at, id;

-- Un id que ya existe no se pisa: el caso de uso decide si es un reintento.
-- name: InsertCategory :execrows
INSERT INTO categories (id, user_id, name, name_normalized, kind, color, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO NOTHING;

-- name: GetCategory :one
SELECT id, user_id, name, kind, color, archived_at
FROM categories
WHERE id = $1;

-- name: UpdateCategory :execrows
UPDATE categories
SET name = $3, name_normalized = $4, color = $5, updated_at = now()
WHERE id = $1 AND user_id = $2 AND archived_at IS NULL;

-- name: ArchiveCategory :execrows
UPDATE categories
SET archived_at = now(), name_normalized = NULL, updated_at = now()
WHERE id = $1 AND user_id = $2 AND archived_at IS NULL;

-- Para otros módulos (credit): las categorías pedidas del usuario, también las
-- archivadas (una compra conserva la suya).
-- name: ListCategoriesByIDs :many
SELECT id, name, kind, color, archived_at
FROM categories
WHERE user_id = sqlc.arg(user_id) AND id = ANY(sqlc.arg(ids)::uuid[]);
