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

## Estado (22 oct 2026)

Implementado en API y Android, en revisión.

| Comprobación | Resultado |
|---|---|
| `gofmt`, `go vet`, `golangci-lint run`, `go build` | Sin problemas (`0 issues`) |
| `go test ./...` sin base / contra el Postgres local | Pasa: 4 del dominio (nombre limpio y largo en caracteres, color, tipo, clave sin tildes que conserva la ñ, las 9 iniciales válidas y sin repetir), 5 del servicio y 6 de integración por HTTP (las 9 al registrarse y en orden; "alimentacion" y "ALIMENTACIÓN" de egreso → 409, de ingreso → 201; archivar "Ocio" y crear "ocio" → 201; "OCIO" → 200; errores en el orden de la app; reintento con el mismo `id` → 200, con otro contenido → `ID_CONFLICT`; dos altas simultáneas → una 201 y otra 409; aislamiento entre usuarios → 404) |
| Migración `00003`: `down` y `up` | Funciona; un usuario anterior a la migración recibe las 9 en orden |
| OpenAPI | Válido (Redocly) |
| Android: pruebas, `lintDebug`, `installDebug` | 60 pruebas pasan (6 de `RemoteCategoriesRepository`, 1 de `DemoCategoryMirror`); ningún aviso de lint en los archivos nuevos |

**En el teléfono** (API real + Postgres local):

| Prueba | Resultado |
|---|---|
| Registro | La base tiene las 9; Categorías muestra las 6 de egreso y las 3 de ingreso, en el orden de la app |
| Crear "alimentacion" de egreso | "Ya tienes una categoría de este tipo con ese nombre." bajo el campo |
| Crear "Mascotas" | 201; aparece al final de Egresos |
| Renombrar "Ocio" a "OCIO" | Se guarda |
| Movimiento (demo) con "Mascotas" | Se guarda y se ve en Inicio |
| Archivar "Mascotas" | Deja de listarse; el movimiento la conserva; en la base queda archivada y sin `name_normalized` |
| Cerrar y abrir la app | Las categorías vuelven del API |

## Desviaciones

- **Orden de la lista**: en el orden en que se crearon (`created_at`, `id`), no "por tipo y nombre". Así las 9 iniciales salen como en la app (Alimentación primero) y las nuevas, al final, como en el modo demo.
- **La migración da las 9 iniciales a los usuarios que ya existían** (con un microsegundo entre una y otra para conservar el orden). Sin esto, una cuenta creada antes del plan 03 quedaría sin categorías.
- **`CATEGORY_NAME_TAKEN` trae `details`** con `name` / `NAME_TAKEN`, para que cualquier cliente sepa bajo qué campo mostrarlo.
- **`KIND_INVALID`** es un código nuevo (Android no lo necesita: el tipo se elige con dos botones). Un `id` que no es UUID en el cuerpo responde `INVALID_FORMAT`; en la ruta, `404`.
- **Archivar dos veces** responde 204 (idempotente); editar una archivada, 404.
- **`CategoryReader` se deja para el plan 09** (`credit`), que es quien lo usa; el plan 05 lo necesitará también para los movimientos y lo agregará si llega antes.
- **Android: `DemoCategoryMirror`** (temporal, hasta el plan 05). Los movimientos y compras siguen en datos de ejemplo y el modo demo falla si un movimiento apunta a una categoría que no conoce. El puente copia cada lista leída del API al almacén demo; las categorías de ejemplo quedan ahí archivadas para que los movimientos de ejemplo conserven las suyas.
- **Android valida el nombre repetido antes de enviar** con la lista que ya tiene (como en el modo demo); el 409 del API es el respaldo (otro dispositivo, carreras) y se muestra con el mismo texto.
