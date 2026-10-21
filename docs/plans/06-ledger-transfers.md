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

## Desviaciones

(Se completan al implementar.)
