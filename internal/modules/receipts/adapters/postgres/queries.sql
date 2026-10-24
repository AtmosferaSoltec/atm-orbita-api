-- name: InsertReceipt :execrows
INSERT INTO receipts (id, user_id, series, number, issue_date, client_name, client_doc_type, client_doc_number,
                      description, gross, withholding, collected_on, source, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $14)
ON CONFLICT (id) DO NOTHING;

-- name: GetReceipt :one
SELECT * FROM receipts WHERE id = $1;

-- name: ListReceiptsOfYear :many
SELECT * FROM receipts
WHERE user_id = sqlc.arg(user_id) AND issue_date >= sqlc.arg(date_from) AND issue_date < sqlc.arg(date_to)
ORDER BY issue_date DESC, number DESC;

-- name: ListReceiptYears :many
SELECT DISTINCT EXTRACT(YEAR FROM issue_date)::int AS year
FROM receipts
WHERE user_id = $1
ORDER BY year DESC;

-- name: UpdateReceipt :execrows
UPDATE receipts
SET series = $3, number = $4, issue_date = $5, client_name = $6, client_doc_type = $7, client_doc_number = $8,
    description = $9, gross = $10, withholding = $11, collected_on = $12, source = $13, updated_at = now()
WHERE id = $1 AND user_id = $2;

-- name: SetReceiptCollected :execrows
UPDATE receipts SET collected_on = $3, updated_at = now() WHERE id = $1 AND user_id = $2;

-- name: DeleteReceipt :execrows
DELETE FROM receipts WHERE id = $1 AND user_id = $2;

-- name: GetYearLimit :one
SELECT amount FROM receipt_year_limits WHERE user_id = $1 AND year = $2;

-- name: UpsertYearLimit :exec
INSERT INTO receipt_year_limits (user_id, year, amount, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (user_id, year) DO UPDATE SET amount = EXCLUDED.amount, updated_at = now();

-- name: ListDocumentsOfReceipts :many
SELECT * FROM receipt_documents
WHERE user_id = $1 AND receipt_id = ANY(sqlc.arg(receipt_ids)::uuid[])
ORDER BY kind;

-- name: GetDocument :one
SELECT * FROM receipt_documents WHERE receipt_id = $1 AND user_id = $2 AND kind = $3;

-- name: ListStorageKeysOfReceipt :many
SELECT storage_key FROM receipt_documents WHERE receipt_id = $1 AND user_id = $2;

-- name: DeleteDocument :execrows
DELETE FROM receipt_documents WHERE receipt_id = $1 AND user_id = $2 AND kind = $3;

-- name: InsertDocument :exec
INSERT INTO receipt_documents (id, receipt_id, user_id, kind, file_name, size_bytes, content_type, storage_key, uploaded_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);
