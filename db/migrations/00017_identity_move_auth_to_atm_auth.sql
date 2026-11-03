-- +goose Up
-- Orbita ya no autentica: cuentas, sesiones, contraseñas y Google viven en atm-auth-api.
-- Aún no había usuarios reales (decisión del dueño, 2 nov 2026), así que se borran los
-- datos de prueba y se quitan las tablas de autenticación. `users` queda como la fila
-- local de cada persona (mismo id que en atm-auth-api), para que la cascada borre sus datos.
TRUNCATE TABLE users CASCADE;
DROP TABLE IF EXISTS refresh_tokens, sessions, password_reset_codes, external_identities, google_login_challenges;
ALTER TABLE users DROP COLUMN password_hash, DROP COLUMN last_login_at;

-- +goose Down
-- No se puede deshacer: los datos y las tablas de autenticación ya no existen. Se deja
-- vacío a propósito; volver atrás es restaurar el respaldo.
SELECT 1;
