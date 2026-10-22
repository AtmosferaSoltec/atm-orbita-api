-- name: CreateProfile :exec
INSERT INTO profiles (user_id, main_currency, secondary_currency, display_currency, timezone)
VALUES ($1, $2, $3, $4, $5);

-- name: GetProfile :one
SELECT user_id, main_currency, secondary_currency, display_currency, timezone, fx_mode, updated_at
FROM profiles
WHERE user_id = $1;

-- Bloquea el perfil: dos cambios de monedas a la vez no se mezclan.
-- name: LockProfile :one
SELECT user_id, main_currency, secondary_currency, display_currency, timezone, fx_mode, updated_at
FROM profiles
WHERE user_id = $1
FOR UPDATE;

-- name: UpdateProfile :exec
UPDATE profiles
SET main_currency = $2, secondary_currency = $3, display_currency = $4, timezone = $5, updated_at = now()
WHERE user_id = $1;

-- name: InsertExchangeRate :exec
INSERT INTO exchange_rates (id, user_id, from_currency, to_currency, rate, source)
VALUES ($1, $2, $3, $4, $5, $6);

-- El más reciente del usuario para el par, en cualquiera de los dos sentidos.
-- name: LatestUserRate :one
SELECT from_currency, to_currency, rate, created_at
FROM exchange_rates
WHERE user_id = $1
  AND ((from_currency = $2 AND to_currency = $3) OR (from_currency = $3 AND to_currency = $2))
ORDER BY created_at DESC, id DESC
LIMIT 1;
