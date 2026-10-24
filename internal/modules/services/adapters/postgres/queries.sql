-- name: ListServices :many
SELECT * FROM services WHERE user_id=$1 ORDER BY name, id;
-- name: GetService :one
SELECT * FROM services WHERE user_id=$1 AND id=$2;
-- name: LockService :one
SELECT id FROM services WHERE user_id=$1 AND id=$2 FOR UPDATE;
-- name: InsertService :execrows
INSERT INTO services (id,user_id,name,color,currency,amount,billing_day,started_on,status,ended_on,note,shared,created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (id) DO NOTHING;
-- name: UpdateService :exec
UPDATE services SET name=$3,color=$4,currency=$5,amount=$6,billing_day=$7,started_on=$8,status=$9,ended_on=$10,note=$11,shared=$12,updated_at=now()
WHERE user_id=$1 AND id=$2;
-- name: DeleteService :exec
DELETE FROM services WHERE user_id=$1 AND id=$2;
-- name: ListMembers :many
SELECT * FROM service_members WHERE user_id=$1 ORDER BY service_id,sort_order,id;
-- name: DeleteRemovedMembers :exec
DELETE FROM service_members WHERE user_id=$1 AND service_id=$2 AND NOT (id=ANY(sqlc.arg(member_ids)::uuid[]));
-- name: UpsertMember :execrows
INSERT INTO service_members (id,service_id,user_id,name,amount,since_month,sort_order)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (id) DO UPDATE SET name=excluded.name,amount=excluded.amount,since_month=excluded.since_month,sort_order=excluded.sort_order
WHERE service_members.user_id=excluded.user_id AND service_members.service_id=excluded.service_id;
-- name: ListServicePayments :many
SELECT * FROM service_member_payments WHERE user_id=$1 ORDER BY service_id,month,member_id;
-- name: InsertMemberPayment :exec
INSERT INTO service_member_payments (id,service_id,user_id,member_id,month,paid_on,amount)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (member_id,month) DO NOTHING;
-- name: DeleteMemberPayment :exec
DELETE FROM service_member_payments WHERE user_id=$1 AND service_id=$2 AND member_id=$3 AND month=$4;
