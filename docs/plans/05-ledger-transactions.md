# 05 · Movimientos (ingresos y egresos) y lista de movimientos

## Objetivo

Registrar, editar y eliminar ingresos y egresos, y listar los movimientos de un periodo (con las transferencias del plan 06 mezcladas cuando existan), con búsqueda y filtro por cuenta.

## Pantallas de Android que cubre

- **Nuevo/Editar movimiento** (`ui/feature/movement/MovementFormScreen.kt`; `ORBITA_SPEC` §7.5), sin el interruptor de tarjeta de crédito (plan 09).
- **Lista de movimientos** (§7.7) y **Últimos movimientos** de Inicio.
- Repositorio: `EntriesRepository` (`observeEntries(period)`, `observeRecentEntries(limit)`, `createMovement`, `updateMovement`, `deleteMovement`).

## Módulo

`ledger`. Expone `EntriesReader` (recientes, para `home`) y `ReportQuery` (sumas por moneda y categoría, para `reports`, plan 07).

## Endpoints

| Método y ruta | Request | Respuesta |
|---|---|---|
| `POST /transactions` | `{ "id"?, "account_id", "category_id", "amount", "description"?, "occurred_on"? }` | `201 { data: transaction }` |
| `PATCH /transactions/{id}` | `{ "account_id"?, "category_id"?, "amount"?, "description"?, "occurred_on"? }` | `200 { data: transaction }` |
| `DELETE /transactions/{id}` | — | `204` (también si ya estaba eliminado) |
| `GET /entries` | query: `from`, `to` (obligatorios, `YYYY-MM-DD`), `account_id`?, `q`?, `page`?, `page_size`? (máx. 100) | `200 { data: [entry], meta: { page, page_size, total } }` |

Un `entry` es un movimiento o una transferencia, con `entry_type`:

```json
{
  "entry_type": "transaction",
  "id": "…", "kind": "expense", "occurred_on": "2026-10-02",
  "amount": "18.50", "currency": "PEN", "description": "Almuerzo",
  "account": { "id": "…", "name": "Efectivo", "type": "cash", "currency": "PEN" },
  "category": { "id": "…", "name": "Alimentación", "kind": "expense", "color": "#0E7490" },
  "credit_purchase_id": null
}
```

`kind` sale de la categoría (no se envía al crear). `credit_purchase_id` existe desde el plan 10; aquí siempre es `null`.

### Errores (en el orden de revisión de `ORBITA_SPEC` §4.1: monto, cuenta, categoría, descripción, fecha)

| Situación | HTTP | `code` |
|---|---|---|
| Monto ausente, cero, negativo o con más de 2 decimales | 400 | `VALIDATION_FAILED` + `AMOUNT_REQUIRED` |
| Sin cuenta | 400 | `VALIDATION_FAILED` + `ACCOUNT_REQUIRED` |
| Sin categoría | 400 | `VALIDATION_FAILED` + `CATEGORY_REQUIRED` |
| Descripción de más de 500 | 400 | `VALIDATION_FAILED` + `DESCRIPTION_TOO_LONG` |
| Fecha posterior a hoy (zona del perfil) | 400 | `VALIDATION_FAILED` + `FUTURE_DATE` |
| Cuenta o categoría archivada | 422 | `ACCOUNT_ARCHIVED` / `CATEGORY_ARCHIVED` |
| Cuenta, categoría o movimiento de otro usuario o inexistente | 404 | `NOT_FOUND` |
| `from` > `to`, o rango de más de 10 años | 400 | `VALIDATION_FAILED` + `RANGE_INVALID` |
| Mismo `id` con otro contenido | 409 | `ID_CONFLICT` |

## Tablas y migraciones

`00005_ledger_create_transactions.sql`:

| Columna | Tipo |
|---|---|
| `id` | `UUID PK` |
| `user_id` | `UUID NOT NULL FK → users ON DELETE CASCADE` |
| `account_id` | `UUID NOT NULL` — FK compuesta `(account_id, user_id) → accounts (id, user_id)` |
| `category_id` | `UUID NOT NULL` — FK compuesta `(category_id, user_id, kind) → categories (id, user_id, kind)` |
| `kind` | `VARCHAR(10) NOT NULL` (se guarda para sumar sin unir con categorías; la FK impide que se desalinee) |
| `amount` | `NUMERIC(14,2) NOT NULL CHECK (amount > 0)` |
| `occurred_on` | `DATE NOT NULL` (sin valor por defecto) |
| `description` | `VARCHAR(500) NOT NULL DEFAULT ''` |
| `created_at`, `updated_at` | `TIMESTAMPTZ` |
| `deleted_at` | `TIMESTAMPTZ NULL` |

Índices: `(user_id, occurred_on DESC, created_at DESC) WHERE deleted_at IS NULL` (lista); `(account_id) INCLUDE (kind, amount) WHERE deleted_at IS NULL` (saldos); `(user_id, category_id, occurred_on) WHERE deleted_at IS NULL` (reportes).

Las claves foráneas compuestas son la segunda barrera: aunque el código olvidara comprobar a quién pertenece una cuenta, la base rechaza un movimiento que apunte a la de otra persona.

## Reglas

- **La moneda del movimiento es la de su cuenta.** No se guarda aparte.
- **Fecha.** Si `occurred_on` no viene (Android no la manda al crear), el API usa "hoy" según la zona del perfil. Nunca posterior a hoy; hacia atrás no hay tope. Se guarda como `DATE`, ya calculada.
- **Categoría del tipo correcto.** El `kind` sale de la categoría elegida; al editar, cambiar a una categoría del otro tipo convierte el egreso en ingreso (como en Android). Una categoría archivada no se usa en movimientos nuevos ni al cambiar la de uno existente.
- **Eliminar** es borrado lógico (`deleted_at`): deja de contar en saldos, listas y reportes. Eliminar uno ya eliminado devuelve `204`.
- **Saldo de la cuenta**: este plan suma `transactions` a la consulta de saldos del plan 04.
- **Orden de la lista**: `occurred_on DESC`, luego `created_at DESC` (desempata dentro del día, como `order` en el modo demo).
- **`GET /entries`**: Android pide un mes (`from` = día 1, `to` = último día o hoy) y lee todas sus páginas; la lista muestra 5 meses (el actual y 4 anteriores), pero eso lo limita la app, no el API. `q` busca sin distinguir mayúsculas en la descripción (y en la nota de las transferencias, plan 06), escapando `%`, `_` y `\`. `account_id` filtra por esa cuenta (en transferencias, como origen o destino).
- **Los recientes de Inicio** = `GET /entries` de los últimos movimientos con `page_size=4` sobre un rango amplio, o `EntriesReader.Recent` desde `home` (plan 08).
- Toda consulta lleva `user_id` (sale del token) como primer parámetro del repositorio.

## Dependencias

04 (cuentas y saldos) y 03 (categorías).

## Pasos de implementación

1. Migración y consultas sqlc; ampliar la consulta de saldos.
2. Dominio: `Transaction`, validación en el orden de Android, errores.
3. Casos de uso `Create`, `Update`, `Delete`, `ListEntries`; `EntriesReader`.
4. Adapters Postgres y HTTP (`/transactions`, `/entries`).
5. Android: `RemoteEntriesRepository` (movimientos y lista); `observeEntries(period)` pide todas las páginas del periodo y vuelve a leer tras cada escritura; mapeo de `entry` a `Movement`/`Transfer`.
6. OpenAPI y `ORBITA_SPEC.md`.

## Pruebas

Unitarias: orden de validación; "hoy" por zona horaria.

Integración:
- Un egreso de 45.52 baja el saldo de su cuenta exactamente en 45.52; eliminarlo lo devuelve.
- Sin `occurred_on`, creado a las 20:00 de Lima (01:00 UTC del día siguiente): queda con la fecha de Lima.
- Fecha de mañana → `400 FUTURE_DATE`.
- Una categoría de ingreso con una cuenta de otro usuario → `404`.
- Lista de septiembre con los datos de ejemplo: 16 movimientos (más la transferencia del plan 06), en el orden de `SampleData.kt`.
- `q=super` encuentra los dos "Supermercado"; `account_id` filtra.
- Reintentar un `POST` con el mismo `id` y cuerpo → `200` sin duplicar.

## Criterios de aceptación

- Crear, editar y eliminar un movimiento desde la app cambia el saldo y la lista al instante, y persiste.

## Variables nuevas para el servidor

Ninguna.

## Desviaciones

(Se completan al implementar.)
