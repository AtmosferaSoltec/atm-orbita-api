-- Crea el rol, la base y el esquema de Orbita en el PostgreSQL compartido.
-- Se ejecuta UNA vez, como superusuario, y no es idempotente a proposito:
-- si el rol o la base ya existen, falla en vez de pisarlos.
--
--   docker exec -i atm-postgres psql -v ON_ERROR_STOP=1 -U postgres \
--     -v app_password="'<clave>'" -d postgres < scripts/provision-db.sql

CREATE ROLE orbita_app WITH LOGIN PASSWORD :app_password;
CREATE DATABASE orbita OWNER orbita_app;

REVOKE CONNECT ON DATABASE orbita FROM PUBLIC;
GRANT CONNECT ON DATABASE orbita TO orbita_app;

\connect orbita

CREATE SCHEMA IF NOT EXISTS orbita AUTHORIZATION orbita_app;

ALTER SCHEMA public OWNER TO postgres;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON SCHEMA public FROM orbita_app;

ALTER ROLE orbita_app IN DATABASE orbita SET search_path TO orbita;
