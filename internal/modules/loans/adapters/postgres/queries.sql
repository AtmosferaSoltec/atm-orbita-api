-- name: ListLoanDebtors :many
SELECT * FROM loan_debtors WHERE user_id=$1 ORDER BY id FOR SHARE;
-- name: GetLoanDebtor :one
SELECT * FROM loan_debtors WHERE user_id=$1 AND id=$2 FOR SHARE;
-- name: SharedLoanDebtor :one
SELECT * FROM loan_debtors WHERE share_token=$1 FOR SHARE;
-- name: LockLoanDebtor :one
SELECT id FROM loan_debtors WHERE user_id=$1 AND id=$2 FOR UPDATE;
-- name: InsertLoanDebtor :execrows
INSERT INTO loan_debtors(id,user_id,name,currency,created_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO NOTHING;
-- name: RenameLoanDebtor :exec
UPDATE loan_debtors SET name=$3,updated_at=now() WHERE user_id=$1 AND id=$2;
-- name: DeleteLoanDebtor :exec
DELETE FROM loan_debtors WHERE user_id=$1 AND id=$2;
-- name: SetLoanShare :exec
UPDATE loan_debtors SET share_token=$3,updated_at=now() WHERE user_id=$1 AND id=$2;
-- name: ListLoanMovements :many
SELECT * FROM loan_movements WHERE user_id=$1 AND debtor_id=ANY(sqlc.arg(debtor_ids)::uuid[]) ORDER BY occurred_on,created_at,id;
-- name: ListLoanPlans :many
SELECT * FROM loan_month_plans WHERE user_id=$1 AND debtor_id=ANY(sqlc.arg(debtor_ids)::uuid[]) ORDER BY month;
-- name: InsertLoanMovement :execrows
INSERT INTO loan_movements(id,debtor_id,user_id,kind,amount,occurred_on,applied_month,title,note,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO NOTHING;
-- name: UpdateLoanMovement :exec
UPDATE loan_movements SET amount=$4,occurred_on=$5,applied_month=$6,title=$7,note=$8,updated_at=now() WHERE user_id=$1 AND debtor_id=$2 AND id=$3;
-- name: DeleteLoanMovement :exec
DELETE FROM loan_movements WHERE user_id=$1 AND debtor_id=$2 AND id=$3;
-- name: DeleteDebtorPlans :exec
DELETE FROM loan_month_plans WHERE user_id=$1 AND debtor_id=$2;
-- name: InsertLoanPlan :exec
INSERT INTO loan_month_plans(debtor_id,user_id,month,amount) VALUES($1,$2,$3,$4);
