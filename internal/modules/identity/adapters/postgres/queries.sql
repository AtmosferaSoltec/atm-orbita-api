-- name: CreateUser :exec
INSERT INTO users (id, email, password_hash)
VALUES ($1, $2, $3);

-- name: GetUserByEmail :one
SELECT id, email, password_hash, created_at
FROM users
WHERE email = $1;

-- name: GetUserByID :one
SELECT id, email, password_hash, created_at
FROM users
WHERE id = $1;

-- name: TouchLastLogin :exec
UPDATE users SET last_login_at = now(), updated_at = now() WHERE id = $1;

-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, user_agent)
VALUES ($1, $2, $3);

-- name: IsSessionActive :one
SELECT EXISTS (
    SELECT 1 FROM sessions WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
);

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = now(), revoked_reason = $2
WHERE id = $1 AND revoked_at IS NULL;

-- name: TouchSession :exec
UPDATE sessions SET last_used_at = now() WHERE id = $1;

-- name: CreateRefreshToken :exec
INSERT INTO refresh_tokens (id, session_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4);

-- Bloquea la fila: dos renovaciones con el mismo token no pueden pasar a la vez.
-- name: LockRefreshToken :one
SELECT r.id, r.session_id, r.expires_at, r.used_at, s.user_id, s.revoked_at AS session_revoked_at
FROM refresh_tokens r
JOIN sessions s ON s.id = r.session_id
WHERE r.token_hash = $1
FOR UPDATE OF r;

-- name: MarkRefreshTokenUsed :exec
UPDATE refresh_tokens SET used_at = now() WHERE id = $1;

-- Plan 11: recuperar la contraseña.

-- name: CountResetCodesSince :one
SELECT count(*) FROM password_reset_codes WHERE user_id = $1 AND created_at > $2;

-- name: DiscardResetCodes :exec
-- Un código nuevo deja sin efecto los anteriores.
UPDATE password_reset_codes SET used_at = $2 WHERE user_id = $1 AND used_at IS NULL;

-- name: CreateResetCode :exec
INSERT INTO password_reset_codes (id, user_id, code_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5);

-- name: LockActiveResetCode :one
-- El código vigente del usuario, bloqueado hasta el fin de la transacción: de
-- dos cambios de contraseña simultáneos, el segundo lo encuentra usado.
SELECT id, code_hash, expires_at, attempts
FROM password_reset_codes
WHERE user_id = $1 AND used_at IS NULL
ORDER BY created_at DESC
LIMIT 1
FOR UPDATE;

-- name: AddResetCodeAttempt :exec
UPDATE password_reset_codes SET attempts = attempts + 1 WHERE id = $1;

-- name: UseResetCode :execrows
UPDATE password_reset_codes SET used_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND used_at IS NULL AND expires_at > sqlc.arg(now) AND attempts < 5;

-- name: UpdatePassword :exec
UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1;

-- name: RevokeUserSessions :exec
UPDATE sessions SET revoked_at = now(), revoked_reason = $2
WHERE user_id = $1 AND revoked_at IS NULL;

-- La cascada se lleva todo lo del usuario (plan 12).
-- name: DeleteUser :execrows
DELETE FROM users WHERE id = $1;
