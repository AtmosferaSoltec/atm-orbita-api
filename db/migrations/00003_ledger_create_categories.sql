-- Plan 03: categorías de ingreso y de egreso.

-- +goose Up
CREATE TABLE categories (
    id              UUID        PRIMARY KEY,
    user_id         UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Tal como lo escribió la persona (recortado y con espacios simples): es lo que se muestra.
    name            VARCHAR(60) NOT NULL,
    -- Minúsculas y sin tildes (conserva la ñ), solo para comparar. Nulo al archivar:
    -- así una categoría archivada deja libre su nombre.
    name_normalized VARCHAR(60),
    kind            VARCHAR(10) NOT NULL,
    color           CHAR(7)     NOT NULL,
    archived_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT categories_kind CHECK (kind IN ('income', 'expense')),
    CONSTRAINT categories_color_format CHECK (color ~ '^#[0-9A-Fa-f]{6}$'),
    CONSTRAINT categories_name_not_blank CHECK (length(btrim(name)) BETWEEN 1 AND 60),
    -- Para las claves foráneas compuestas de los movimientos (plan 05): un
    -- movimiento no puede apuntar a la categoría de otra persona ni de otro tipo.
    CONSTRAINT categories_id_user_key UNIQUE (id, user_id),
    CONSTRAINT categories_id_user_kind_key UNIQUE (id, user_id, kind)
);

-- El nombre no se repite dentro del mismo tipo. Los nulos (archivadas) no chocan.
CREATE UNIQUE INDEX categories_user_kind_name_key ON categories (user_id, kind, name_normalized);
CREATE INDEX categories_user_idx ON categories (user_id, created_at) WHERE archived_at IS NULL;

-- Los usuarios que ya existían reciben las 9 iniciales, como si acabaran de
-- registrarse (domain.InitialCategories). Un microsegundo entre una y otra
-- mantiene su orden en la lista.
INSERT INTO categories (id, user_id, name, name_normalized, kind, color, created_at, updated_at)
SELECT gen_random_uuid(), u.id, i.name, i.name_normalized, i.kind, i.color,
       now() + i.position * interval '1 microsecond', now()
FROM users u
CROSS JOIN (VALUES
    (1, 'Alimentación',   'alimentacion',   'expense', '#0E7490'),
    (2, 'Transporte',     'transporte',     'expense', '#B45309'),
    (3, 'Vivienda',       'vivienda',       'expense', '#1D4ED8'),
    (4, 'Salud',          'salud',          'expense', '#7C3AED'),
    (5, 'Ocio',           'ocio',           'expense', '#BE185D'),
    (6, 'Otros',          'otros',          'expense', '#64748B'),
    (7, 'Sueldo',         'sueldo',         'income',  '#0B7A5A'),
    (8, 'Freelance',      'freelance',      'income',  '#0E7490'),
    (9, 'Otros ingresos', 'otros ingresos', 'income',  '#64748B')
) AS i (position, name, name_normalized, kind, color);

-- +goose Down
DROP TABLE categories;
