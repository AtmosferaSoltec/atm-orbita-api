-- Plan 17: nombre de la cuenta. Lo pide la web al registrarse; Android todavía
-- no lo envía, por eso es opcional. Solo agrega una columna nullable: la versión
-- anterior del API sigue funcionando contra la base ya migrada.

-- +goose Up
ALTER TABLE users ADD COLUMN name VARCHAR(60);

-- +goose Down
ALTER TABLE users DROP COLUMN name;
