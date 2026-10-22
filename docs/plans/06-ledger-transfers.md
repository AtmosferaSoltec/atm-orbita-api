# 06 · Transferencias

## Objetivo

Mover dinero entre dos cuentas propias, en la misma moneda o entre monedas, guardando lo que sale, lo que entra y el tipo de cambio usado, para que los saldos cuadren con lo que hizo el banco. Una transferencia no es ingreso ni gasto.

## Pantallas de Android que cubre

- **Transferir / Editar transferencia** (`ui/feature/transfer/TransferScreen.kt`; `ORBITA_SPEC` §7.6).
- Las filas de transferencia de Inicio y de la Lista de movimientos.
- Repositorio: `EntriesRepository` (`createTransfer`, `updateTransfer`, `deleteTransfer`).

## Módulo

`ledger`.

## Endpoints

| Método y ruta | Request | Respuesta |
|---|---|---|
| `POST /transfers` | `{ "id"?, "from_account_id", "to_account_id", "from_amount", "to_amount", "exchange_rate"?, "occurred_on", "note"? }` | `201 { data: transfer }` |
| `PATCH /transfers/{id}` | mismos campos, opcionales | `200 { data: transfer }` |
| `DELETE /transfers/{id}` | — | `204` |

Y en `GET /entries` (plan 05) aparecen con `entry_type: "transfer"`:

```json
{
  "entry_type": "transfer",
  "id": "…", "occurred_on": "2026-10-01", "note": "Cambio de dólares",
  "from_account": { "id": "…", "name": "Cuenta Dólares", "currency": "USD" },
  "to_account": { "id": "…", "name": "Débito principal", "currency": "PEN" },
  "from_amount": "20.00", "to_amount": "64.00", "exchange_rate": "3.200000"
}
```

### Errores (orden de `ORBITA_SPEC` §4.1: cuentas distintas, montos, tipo de cambio, nota, fecha)

| Situación | HTTP | `code` |
|---|---|---|
| Misma cuenta en ambos lados | 400 | `VALIDATION_FAILED` + `SAME_ACCOUNT` |
| Algún monto ausente o ≤ 0 | 400 | `VALIDATION_FAILED` + `AMOUNT_REQUIRED` |
| Monedas distintas sin tipo de cambio > 0 | 400 | `VALIDATION_FAILED` + `RATE_REQUIRED` |
| Misma moneda con montos distintos, o con tipo de cambio | 400 | `VALIDATION_FAILED` + `AMOUNTS_MUST_MATCH` |
| Nota de más de 500 | 400 | `VALIDATION_FAILED` + `DESCRIPTION_TOO_LONG` |
| Fecha posterior a hoy | 400 | `VALIDATION_FAILED` + `FUTURE_DATE` |
| Cuenta archivada | 422 | `ACCOUNT_ARCHIVED` |
| Cuenta o transferencia de otro usuario | 404 | `NOT_FOUND` |

## Tablas y migraciones

`00006_ledger_create_transfers.sql`:

| Columna | Tipo |
|---|---|
| `id` | `UUID PK` |
| `user_id` | `UUID NOT NULL FK → users ON DELETE CASCADE` |
| `from_account_id`, `to_account_id` | `UUID NOT NULL`, FK compuestas `(…, user_id) → accounts (id, user_id)` |
| `from_amount`, `to_amount` | `NUMERIC(14,2) NOT NULL`, `> 0` |
| `exchange_rate` | `NUMERIC(18,6) NULL`, nulo o `> 0` |
| `occurred_on` | `DATE NOT NULL` |
| `note` | `VARCHAR(500) NOT NULL DEFAULT ''` |
| `created_at`, `updated_at`, `deleted_at` | `TIMESTAMPTZ` |

`CHECK (from_account_id <> to_account_id)`. Índices: `(user_id, occurred_on DESC, created_at DESC) WHERE deleted_at IS NULL`, `(from_account_id) INCLUDE (from_amount)` y `(to_account_id) INCLUDE (to_amount)`, ambos `WHERE deleted_at IS NULL`.

## Reglas

- **Lo que mueve los saldos son los dos montos**: `from_amount` sale del origen y `to_amount` entra al destino. El tipo de cambio guardado es informativo (`docs/03` §5).
- **Misma moneda**: `exchange_rate` nulo y `to_amount = from_amount`.
- **Monedas distintas**: `exchange_rate > 0` obligatorio. El API no recalcula: guarda lo que manda la app (que ya enlaza `out`, `rate` e `in`: `in = round(out × rate, 2)` o `rate = in ÷ out` a 6 decimales). Sin tipo de cambio configurado no pueden existir dos cuentas de monedas distintas (plan 04), así que esa transferencia no se puede dar.
- **Una cuenta en una tercera moneda** (fuera del par) también se puede transferir: la app deja el tipo de cambio vacío y el usuario lo escribe.
- **Saldo negativo**: se permite (Android avisa pero no bloquea).
- **Fecha** obligatoria (la app manda hoy por defecto, editable), nunca futura.
- **No cuenta** en reportes ni en las tarjetas del mes.
- **Eliminar** es borrado lógico.
- Este plan suma las transferencias a la consulta de saldos y a `GET /entries` (con `UNION ALL` y el mismo orden; `q` busca en la nota; `account_id` coincide con origen o destino).

## Dependencias

05 (lista de movimientos y saldos).

## Pasos de implementación

1. Migración y consultas sqlc; ampliar saldos y la consulta de `/entries`.
2. Dominio: `Transfer` con su validación en el orden de Android.
3. Casos de uso `Create`, `Update`, `Delete`.
4. Adapters Postgres y HTTP.
5. Android: `RemoteEntriesRepository` (transferencias).
6. OpenAPI y `ORBITA_SPEC.md`.

## Pruebas

Unitarias: orden de validación; misma moneda con montos distintos → `AMOUNTS_MUST_MATCH`.

Integración (datos de ejemplo):
- US$ 20.00 de "Cuenta Dólares" a "Débito principal" con 3.20 y S/ 64.00: los saldos pasan de 250.00/1,245.80 a 230.00/1,309.80.
- Con S/ 63.40 y cambio 3.17: entran exactamente 63.40.
- El reporte de octubre no cambia.
- Eliminarla devuelve los dos saldos.
- No se puede apuntar a una cuenta de otro usuario (`404`).

## Criterios de aceptación

- Una transferencia entre monedas desde la app deja los dos saldos como los muestra "Saldos después de transferir".

## Variables nuevas para el servidor

Ninguna.

## Estado (22 oct 2026)

Hecho: implementado en API y Android; desviaciones aprobadas el 22 oct 2026.

| Comprobación | Resultado |
|---|---|
| `gofmt`, `go vet`, `golangci-lint run`, `go build` | Sin problemas (`0 issues`) |
| `go test ./...` sin base / contra el Postgres local | Pasa. Dominio: reglas de moneda (misma moneda con montos distintos o con tipo de cambio → `AMOUNTS_MUST_MATCH`; monedas distintas sin tipo de cambio, con cero o con texto → `RATE_REQUIRED`; 3.17 se guarda tal cual) y comparación de reintentos. Servicio (4): orden de validación; entre monedas guarda lo que manda la app; editar a la misma moneda con `exchange_rate: null`; no tocar el tipo de cambio lo conserva; cuentas archivadas y ajenas; reintento y eliminar dos veces. Integración por HTTP (3): US$ 20 → S/ 64 con 3.20 deja 230.00 / 1,309.80; con 63.40 y 3.17 entran exactamente 63.40; eliminarla devuelve los dos saldos; no es movimiento; validación y cuentas de otro usuario → 404; la lista mezcla y pagina las dos tablas en el orden de `SampleData.kt`, `q` busca en la nota y `account_id` coincide con origen o destino |
| Migración `00006`: `down` y `up` | Funciona |
| OpenAPI | Válido (Redocly) |
| Android: pruebas, `lintDebug`, `installDebug` | 75 pruebas pasan; ningún aviso de lint en los archivos nuevos |

**En el teléfono** (API real + Postgres local):

| Prueba | Resultado |
|---|---|
| Nueva cuenta "Dolares" en USD y transferir S/ 320 de Ahorros | El formulario propone 0.3125 y US$ 100.00; tras guardar, Ahorros S/ 4,480.00 y Dolares US$ 100.00, como mostraba "Saldos después de transferir"; en la base, 320.00 → 100.00 con 0.312500 |
| Editarla: destino Efectivo (misma moneda) | En la base queda sin tipo de cambio (`NULL`) y 320.00 → 320.00; Efectivo S/ 310.00, Dolares US$ 0.00 |
| Eliminarla | Los saldos vuelven a como estaban |
| Transferir S/ 50, cerrar y abrir la app | Sigue en Inicio ("Ahorros → Efectivo") |

## Desviaciones

- **La lista se arma en dos pasos**: una consulta ordena y pagina las claves de las dos tablas (`UNION ALL` de `entry_type`, `id`, fecha y creación) y luego se leen los detalles de cada tipo con `id = ANY(…)`. Mismo orden y páginas que un solo `UNION ALL`, pero cada tipo se lee con sus propias columnas y sqlc genera tipos simples.
- **Al editar, `exchange_rate: null` borra el tipo de cambio y no mandarlo lo deja como estaba.** Android manda siempre el campo, también como `null` explícito.
- **Si no viene `occurred_on`, la fecha es hoy** (como en los movimientos); Android la manda siempre.
- **`ACCOUNT_REQUIRED`** si falta alguna de las dos cuentas; `SAME_ACCOUNT` va en el campo `to_account_id` y `AMOUNTS_MUST_MATCH` en `to_amount`.
- **`POST` y `PATCH /transfers` responden la fila de la lista** (`entry_type: "transfer"`), como los movimientos. Las cuentas traen también su `type`.
- **`AMOUNTS_MUST_MATCH` no existe en Android** (su formulario iguala los montos solo); si llegara, se vería el mensaje genérico.
- **"El reporte de octubre no cambia"** se comprobará en el plan 07, que es el de reportes; hoy las transferencias no tocan ninguna suma de movimientos.
- **Android: `DemoEntriesMix` sigue, más chico, hasta el plan 10**: ya solo mezcla el gasto de un pago de tarjeta (que aún es demo) hecho desde una cuenta del API. El puente de cuentas también sigue por eso.
