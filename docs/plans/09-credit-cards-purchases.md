# 09 · Tarjetas de crédito y compras pendientes

## Objetivo

Que el usuario registre sus tarjetas de crédito y las compras hechas con ellas, que quedan **pendientes de pago** y no afectan saldos ni reportes. Las compras pendientes se editan y se eliminan; la deuda se calcula al leerla, por tarjeta y por moneda.

## Pantallas de Android que cubre

- **Tarjetas de Crédito**, **Nueva/Editar tarjeta** (`ui/feature/credit`; `ORBITA_SPEC` §7.9).
- **Nuevo movimiento** con el interruptor "Compra con tarjeta de crédito" y **Editar compra** (§7.5 y §7.9).
- Repositorio: `CreditRepository` (`observeCards`, `observePendingPurchases`, `createCard`, `updateCard`, `archiveCard`, `createPurchase`, `updatePurchase`, `deletePurchase`). Pagar es el plan 10.

## Módulo

`credit`. Usa `ledger` (`CategoryReader`) y `profile` (`ProfileReader`) por sus interfaces. Expone `CreditSummary` (para Inicio).

## Endpoints

| Método y ruta | Request | Respuesta |
|---|---|---|
| `GET /credit-cards` | — | `200 { data: [card] }` con su deuda por moneda |
| `POST /credit-cards` | `{ "id"?, "name", "currency" }` | `201 { data: card }` |
| `PATCH /credit-cards/{id}` | `{ "name"?, "currency"? }` | `200 { data: card }` |
| `POST /credit-cards/{id}/archive` | — | `204` |
| `GET /credit-purchases` | `status=pending` | `200 { data: [purchase] }` (sin paginar: son pocas; por `due_date` ascendente) |
| `POST /credit-purchases` | `{ "id"?, "card_id", "category_id", "amount", "description"?, "due_date" }` | `201 { data: purchase }` |
| `PATCH /credit-purchases/{id}` | `{ "card_id"?, "category_id"?, "amount"?, "description"?, "due_date"? }` | `200 { data: purchase }` |
| `DELETE /credit-purchases/{id}` | — | `204` (también si ya no existe) |

```json
{ "id": "…", "name": "Mastercard Oro", "currency": "PEN",
  "debt": [ { "currency": "PEN", "amount": "189.90" }, { "currency": "USD", "amount": "12.00" } ],
  "pending_count": 2, "next_due_date": "2026-10-15" }
```

```json
{ "id": "…", "card_id": "…", "description": "Pasajes", "amount": "240.00", "currency": "PEN",
  "purchase_date": "2026-09-10", "due_date": "2026-10-05", "status": "pending",
  "category": { "id": "…", "name": "Transporte", "kind": "expense", "color": "#B45309" } }
```

### Errores (orden de `ORBITA_SPEC` §4.1 para la compra: monto, tarjeta, categoría, descripción, fecha límite)

| Situación | HTTP | `code` |
|---|---|---|
| Nombre de tarjeta vacío o de más de 60 | 400 | `VALIDATION_FAILED` + `NAME_REQUIRED` / `NAME_TOO_LONG` |
| Monto ausente o ≤ 0 | 400 | `VALIDATION_FAILED` + `AMOUNT_REQUIRED` |
| Sin tarjeta | 400 | `VALIDATION_FAILED` + `CARD_REQUIRED` |
| Sin categoría, o categoría de ingreso | 400 | `VALIDATION_FAILED` + `CATEGORY_REQUIRED` / `CATEGORY_KIND_MISMATCH` |
| Descripción de más de 500 | 400 | `VALIDATION_FAILED` + `DESCRIPTION_TOO_LONG` |
| Al crear: fecha límite anterior a hoy. Al editar: anterior a la fecha de compra | 400 | `VALIDATION_FAILED` + `DUE_DATE_PAST` |
| Tarjeta en otra moneda sin tipo de cambio | 422 | `FX_RATE_NOT_CONFIGURED` |
| Editar o eliminar una compra ya pagada | 409 | `PURCHASE_ALREADY_PAID` ("Esta compra ya está pagada.") |
| Archivar una tarjeta con compras pendientes | 409 | `CARD_HAS_PENDING_PURCHASES` |
| Tarjeta archivada al registrar o mover una compra | 422 | `CARD_ARCHIVED` |
| Recurso de otro usuario | 404 | `NOT_FOUND` |

## Tablas y migraciones

`00007_credit_create_cards_and_purchases.sql`:

| Tabla | Columnas |
|---|---|
| `credit_cards` | `id UUID PK`, `user_id FK → users ON DELETE CASCADE`, `name VARCHAR(60)`, `currency CHAR(3)`, `archived_at NULL`, `created_at`, `updated_at`; `UNIQUE (id, user_id)` |
| `credit_purchases` | `id UUID PK`, `user_id FK → users ON DELETE CASCADE`, `card_id` (FK compuesta `(card_id, user_id) → credit_cards`), `category_id UUID NOT NULL`, `description VARCHAR(500) DEFAULT ''`, `amount NUMERIC(14,2) > 0`, `currency CHAR(3)`, `purchase_date DATE`, `due_date DATE`, `status VARCHAR(10)` (`pending`, `paid`), `created_at`, `updated_at`, `deleted_at NULL` |

`CHECK (due_date >= purchase_date)`. Índice `(user_id, due_date) WHERE status = 'pending' AND deleted_at IS NULL`.

**Sin clave foránea hacia `categories`** (tabla de `ledger`): la guía limita las FK entre módulos a lo indispensable (`user_id`). `credit` valida la categoría por `CategoryReader` al crear y al editar. Las categorías nunca se borran (solo se archivan), así que la referencia no queda colgando.

Los campos del pago (`paid_at` y `payment_transaction_id`) los agrega el plan 10 con su propia migración; la cuenta, el monto y la fecha del pago viven en el egreso.

## Reglas

- **Una compra pendiente no es un movimiento**: no cambia saldos, ahorro ni reportes. Editarla o eliminarla solo cambia la deuda de su tarjeta, que se suma al leer (nada se guarda).
- **Fecha de compra** = hoy (zona del perfil), la pone el API. **Moneda** = la de la tarjeta al momento de la compra (copia).
- **Editar**: solo pendientes. Si cambia de tarjeta, toma la moneda de la tarjeta nueva; si no, conserva la suya aunque la tarjeta haya cambiado de moneda después (como `DemoCreditRepository.updatePurchase`). La fecha límite puede quedar en el pasado (una compra vencida debe poder corregirse), pero no antes de la fecha de compra.
- **Eliminar**: solo pendientes; borrado lógico; eliminar una ya eliminada → `204`.
- **Tarjetas**: nombre y moneda editables (sin tipo de cambio, solo la principal); cambiar la moneda no toca las compras existentes. **No se archiva una tarjeta con compras pendientes.**
- **Deuda** por tarjeta y total: suma de las compras pendientes **por moneda, sin mezclar**; la principal primero. Orden de las tarjetas en `GET /credit-cards`: la de fecha límite más próxima primero; las que no deben nada al final.
- "Urgente" = vence en 3 días o menos, o ya venció (`isUrgent`). El texto del chip lo arma la app.

## Dependencias

03 (categorías) y 02 (moneda principal y tipo de cambio).

## Pasos de implementación

1. Migración y consultas sqlc del módulo `credit`.
2. Dominio: `Card`, `Purchase` (validación al crear y al editar), deuda por moneda, errores.
3. Casos de uso de tarjetas y compras; `CreditSummary`.
4. Adapters Postgres y HTTP.
5. Android: `RemoteCreditRepository` (todo menos `pay`).
6. OpenAPI y `ORBITA_SPEC.md`.

## Pruebas

Integración (datos de ejemplo):
- Deuda total S/ 525.90 + US$ 12.00; Visa Clásica S/ 336.00; Mastercard Oro S/ 189.90 + US$ 12.00.
- Registrar una compra no cambia ningún saldo ni el reporte del mes.
- Editar el monto de Pasajes a 250.00 cambia la deuda de Visa a S/ 346.00 y nada más.
- Mover la "Suscripción" (USD) a otra tarjeta en PEN → pasa a PEN; dejarla en su tarjeta tras cambiar la moneda de la tarjeta → conserva USD.
- Archivar Visa con pendientes → `409`; tras eliminar sus compras → `204`.
- Una categoría de ingreso → `400 CATEGORY_KIND_MISMATCH`.
- A no ve ni edita las compras de B.

## Criterios de aceptación

- En la app, una compra con tarjeta queda pendiente sin tocar saldos; editarla y eliminarla solo cambian la deuda.

## Variables nuevas para el servidor

Ninguna.

## Desviaciones

(Se completan al implementar.)
