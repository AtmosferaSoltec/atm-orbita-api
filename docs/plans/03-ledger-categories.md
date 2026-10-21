# 03 · Categorías

## Objetivo

Que el usuario tenga sus categorías de ingreso y de egreso (las 9 iniciales y las suyas), las cree, las renombre, les cambie el color y las archive, con nombres únicos por tipo sin distinguir mayúsculas ni tildes.

## Pantallas de Android que cubre

- **Categorías** y **Nueva/Editar categoría** (`ui/feature/categories`; `ORBITA_SPEC` §7.11).
- Los desplegables de categoría de Nuevo movimiento y de compra con tarjeta.
- Repositorio: `CategoriesRepository` (`observeCategories`, `create`, `update`, `archive`).

## Módulo

`ledger` (es dueño de categorías, cuentas, movimientos y transferencias). Expone `CategoryReader` (buscar una categoría activa del usuario, con su tipo) para `credit`.

## Endpoints

| Método y ruta | Request | Respuesta |
|---|---|---|
| `GET /categories` | — | `200 { data: [category] }` (no archivadas, por tipo y nombre) |
| `POST /categories` | `{ "id"?, "name", "kind", "color" }` | `201 { data: category }` (o `200` si es un reintento idéntico) |
| `PATCH /categories/{id}` | `{ "name"?, "color"? }` | `200 { data: category }` |
| `POST /categories/{id}/archive` | — | `204` |

```json
{ "id": "…", "name": "Alimentación", "kind": "expense", "color": "#0E7490" }
```

`kind`: `income` o `expense`. `name_normalized` nunca sale del API.

### Errores

| Situación | HTTP | `code` |
|---|---|---|
| Nombre vacío | 400 | `VALIDATION_FAILED` + `NAME_REQUIRED` |
| Nombre de más de 60 | 400 | `VALIDATION_FAILED` + `NAME_TOO_LONG` |
| Color que no es `#RRGGBB` | 400 | `VALIDATION_FAILED` + `COLOR_INVALID` |
| Nombre repetido en ese tipo | 409 | `CATEGORY_NAME_TAKEN` ("Ya tienes una categoría de este tipo con ese nombre.", bajo el campo) |
| Categoría de otro usuario o inexistente | 404 | `NOT_FOUND` |
| Mismo `id` con otro contenido | 409 | `ID_CONFLICT` |

## Tablas y migraciones

`00003_ledger_create_categories.sql`:

| Columna | Tipo |
|---|---|
| `id` | `UUID PK` |
| `user_id` | `UUID NOT NULL FK → users ON DELETE CASCADE` |
| `name` | `VARCHAR(60) NOT NULL` (como lo escribió el usuario, recortado y con espacios simples) |
| `name_normalized` | `VARCHAR(60) NULL` (lo calcula el backend; nulo al archivar) |
| `kind` | `VARCHAR(10) NOT NULL` (`income`, `expense`) |
| `color` | `CHAR(7) NOT NULL` |
| `archived_at` | `TIMESTAMPTZ NULL` |
| `created_at`, `updated_at` | `TIMESTAMPTZ` |

Restricciones: `UNIQUE (user_id, kind, name_normalized)` (los nulos no chocan, así que una archivada libera su nombre); `UNIQUE (id, user_id)` y `UNIQUE (id, user_id, kind)` para las claves foráneas compuestas de movimientos (plan 05); `CHECK color ~ '^#[0-9A-Fa-f]{6}$'`; `CHECK kind IN ('income','expense')`.

## Reglas

- **Alta** (gancho `OnUserRegistered`): egreso → Alimentación `#0E7490`, Transporte `#B45309`, Vivienda `#1D4ED8`, Salud `#7C3AED`, Ocio `#BE185D`, Otros `#64748B`; ingreso → Sueldo `#0B7A5A`, Freelance `#0E7490`, Otros ingresos `#64748B` (los de `SampleData.kt`).
- **Nombre único por usuario y tipo** (`ORBITA_SPEC` §7.11, `Names.kt`): se ignoran mayúsculas, tildes y diéresis, y espacios al inicio, al final o repetidos; la `ñ` se conserva ("Año" ≠ "Ano"); se muestra siempre el nombre original; "Otros" puede existir en ingresos y en egresos; renombrar "ocio" → "Ocio" no choca consigo misma. `text.NormalizeName` (plan 00) debe dar lo mismo que `normalizeName` de Kotlin.
- La base es quien decide: el `409` sale de la restricción única, no de una lectura previa (evita carreras).
- **El tipo no cambia** después de crear la categoría (`PATCH` no lo acepta).
- **Archivar** pone `archived_at` y `name_normalized = NULL`. La categoría deja de listarse y de poder usarse en movimientos y compras nuevos, pero los anteriores la conservan.
- **ID del cliente**: si viene `id` y ya existe una categoría propia con ese `id` y el mismo contenido, se devuelve con `200`; con otro contenido, `409 ID_CONFLICT`.

## Dependencias

01 y 02 (el alta corre después de crear el perfil).

## Pasos de implementación

1. Migración y consultas sqlc del módulo `ledger`.
2. Dominio: `Category`, `Kind`, `Color`, `Name` (limpia y normaliza), errores.
3. Casos de uso `List`, `Create`, `Rename`, `Archive`; gancho de alta; `CategoryReader`.
4. Adapters Postgres (traduce la violación de la restricción única a `ErrNameTaken`) y HTTP.
5. Android: `RemoteCategoriesRepository`; `CATEGORY_NAME_TAKEN` → `DataError.NAME_TAKEN` (mensaje bajo el campo, ya implementado).
6. OpenAPI y `ORBITA_SPEC.md`.

## Pruebas

Unitarias: vectores de normalización (los de `MoneyRulesTest.kt` y `DemoRepositoriesTest.kt`).

Integración:
- Un usuario nuevo tiene exactamente las 9 categorías.
- "Alimentación" existe: crear "alimentacion" o "ALIMENTACIÓN" de egreso → `409`; de ingreso → `201`.
- Archivar "Ocio" y crear "ocio" → `201`.
- Renombrar "Ocio" a "OCIO" → `200`.
- Dos `POST` simultáneos con el mismo nombre → uno `201`, otro `409`.
- A no ve ni archiva las categorías de B (`404`).

## Criterios de aceptación

- La pantalla Categorías muestra las del usuario, y un nombre repetido muestra el error bajo el campo.

## Variables nuevas para el servidor

Ninguna.

## Desviaciones

(Se completan al implementar.)
