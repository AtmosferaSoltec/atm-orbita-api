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

## Estado (22 oct 2026)

Implementado en API y Android, en revisión.

| Comprobación | Resultado |
|---|---|
| `gofmt`, `go vet`, `golangci-lint run`, `go build` | Sin problemas (`0 issues`) |
| `go test ./...` sin base / contra el Postgres local | Pasa. Dominio (3): deuda de los datos de ejemplo S/ 525.90 + US$ 12.00 (Visa S/ 336.00, Mastercard S/ 189.90 + US$ 12.00), orden de tarjetas, resumen y urgencia (3 días, vencida), validaciones. Integración por HTTP (4), con "hoy" = 2 oct en Lima: deuda y orden de la lista; una compra no cambia saldos, reportes ni la lista de movimientos; Pasajes a 250.00 → Visa S/ 346.00 y nada más; la Suscripción conserva USD en su tarjeta (ya en soles) y pasa a PEN al moverla; fecha límite vencida al editar sí, antes de la compra no; archivar Visa con pendientes → 409 y tras eliminarlas → 204; tarjeta archivada → `CARD_ARCHIVED`; orden de validación; categoría de ingreso → `CATEGORY_KIND_MISMATCH`; tarjeta en USD sin tipo de cambio → 422; aislamiento entre usuarios |
| Migración `00007`: `down` y `up` | Funciona |
| OpenAPI | Válido (Redocly) |
| Android: pruebas, `lintDebug`, `installDebug` | 78 pruebas pasan (5 de `RemoteCreditRepository`); ningún aviso nuevo de lint |

**En el teléfono** (API real + Postgres local):

| Prueba | Resultado |
|---|---|
| Abrir Crédito | "Aún no tienes tarjetas de crédito" (ya no salen las de ejemplo) |
| Nueva tarjeta "Visa" y compra "Zapatillas" de S/ 200 | Deuda S/ 200.00, vence 4 nov; en la base, pendiente con fecha de compra de hoy |
| Ahorro y Reportes tras la compra | No cambian (S/ 1,533.60; gastos del mes S/ 35.00) |
| Editar la compra a S/ 250 | Deuda S/ 250.00 |
| Archivar Visa con la compra pendiente | La app lo impide con su aviso |
| Eliminar la compra y archivar | Deuda S/ 0.00; Visa deja de listarse y queda archivada en la base |
| Tarjeta "Mastercard" y compra "Audifonos" de S/ 120 | Se guarda; Inicio muestra "1 pago pendiente · vence 4 nov" |
| Marcar como pagada | "Algo salió mal. Inténtalo de nuevo.": pagar es el plan 10 |

## Desviaciones

- **La compra trae también su `card`** (`id`, `name`, `currency`) además de `card_id`, para que la app la muestre sin pedir las tarjetas aparte.
- **`POST` y `PATCH /credit-cards` responden la tarjeta con su deuda**, igual que la lista.
- **`ledger` expone `CategoryReader.Categories(ids)`, que devuelve también las archivadas**, y `credit` decide: una compra nueva (o movida a otra categoría) no usa una archivada (`422 CATEGORY_ARCHIVED`, código nuevo), pero una que ya la tenía la conserva y la lista la muestra.
- **`CATEGORY_KIND_MISMATCH` se informa solo**, después de que el resto del formulario es válido (hay que leer la categoría para saber su tipo).
- **`due_date` ausente o mal escrita** → `INVALID_FORMAT`.
- **`DELETE /credit-purchases/{id}`**: `204` si ya estaba eliminada; si nunca existió o es de otro usuario, `404` (como el resto del API).
- **`GET /credit-purchases` solo admite `status=pending`** (es el valor por defecto); otro valor → `400`.
- **`CreditSummary` queda listo** (pendientes, próxima fecha límite y urgencia) para Inicio, plan 08.
- **Las pruebas de integración de `credit` viven con las de `ledger`**, que ya arman el sistema completo.
- **Android quita los tres puentes temporales** (`DemoCategoryMirror`, `DemoAccountMirror`, `DemoEntriesMix`): con las compras en el API ya nada del modo demo escribe sobre cuentas o categorías reales.
- **Android: "Marcar como pagada" da un error genérico hasta el plan 10.** Antes funcionaba solo en el modo demo; ya no puede, porque las compras están en el API.
- **Android, corrección en Nuevo movimiento**: la tarjeta por defecto se elegía una sola vez al abrir el formulario, y con el API la lista llega un instante después; en el teléfono quedó elegida una tarjeta recién archivada y el API la rechazó (`CARD_ARCHIVED`). Ahora, si la elegida ya no está en la lista, se usa la primera.
- **Android sigue sumando la deuda** a partir de las compras pendientes (`debtIn`); no lee `debt` de las tarjetas (da lo mismo).
