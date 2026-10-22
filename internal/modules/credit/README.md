# Módulo credit

Las tarjetas de crédito y las compras hechas con ellas, que quedan **pendientes de pago** y no afectan saldos ni reportes. Pagar una compra es el plan 10. Plan: [`docs/plans/09-credit-cards-purchases.md`](../../../docs/plans/09-credit-cards-purchases.md).

## Tablas (dueño)

`credit_cards` y `credit_purchases`, migración `00007`. La compra apunta a su tarjeta con una clave foránea compuesta `(card_id, user_id)`; a su categoría (de `ledger`) **sin** clave foránea: se valida con `ledger.CategoryReader` al crear y al editar, y las categorías nunca se borran.

## API pública (`module.go`)

| Qué | Para quién |
|---|---|
| `New(runner, clock, ledger.CategoryReader, profile.Reader)`, `Routes(protected)` | `cmd/api/main.go` |
| `CreditSummary()`: `Summary(ctx, userID)` → pendientes, próxima fecha límite y si alguna es urgente (`ok=false` sin pendientes) | Inicio (plan 08) |

## Reglas

- Una compra pendiente no es un movimiento: no cambia saldos, ahorro ni reportes; solo la deuda de su tarjeta, que se suma al leer.
- Fecha de compra = hoy (zona del perfil). Moneda = la de la tarjeta al comprar.
- Editar y eliminar: solo pendientes (pagada → `PURCHASE_ALREADY_PAID`). Movida a otra tarjeta toma su moneda; si no, conserva la suya. La fecha límite puede quedar vencida al editar, pero no antes de la fecha de compra; al crear, no antes de hoy.
- La categoría de una compra es propia, activa y de egreso.
- Tarjetas: nombre y moneda editables (sin tipo de cambio, solo la principal); cambiar la moneda no toca las compras existentes. No se archiva una tarjeta con compras pendientes.
- Deuda por moneda, sin mezclar; la principal primero. Tarjetas: la de fecha límite más próxima primero; las que no deben nada al final.
- Urgente = vence en 3 días o menos, o ya venció.

## Endpoints

`GET/POST /credit-cards`, `PATCH /credit-cards/{id}`, `POST /credit-cards/{id}/archive`; `GET /credit-purchases?status=pending`, `POST /credit-purchases`, `PATCH`/`DELETE /credit-purchases/{id}`.

## Errores

| `code` | HTTP | Cuándo |
|---|---|---|
| `VALIDATION_FAILED` + `NAME_REQUIRED`, `NAME_TOO_LONG`, `CURRENCY_NOT_SUPPORTED` | 400 | Tarjeta |
| `VALIDATION_FAILED` + `AMOUNT_REQUIRED`, `CARD_REQUIRED`, `CATEGORY_REQUIRED`, `DESCRIPTION_TOO_LONG`, `DUE_DATE_PAST` (o `INVALID_FORMAT`) | 400 | Compra, en ese orden |
| `VALIDATION_FAILED` + `CATEGORY_KIND_MISMATCH` | 400 | Compra con una categoría de ingreso |
| `FX_RATE_NOT_CONFIGURED` | 422 | Tarjeta (o compra en una tarjeta) en otra moneda sin tipo de cambio |
| `CARD_ARCHIVED` / `CATEGORY_ARCHIVED` | 422 | Registrar o mover una compra a una tarjeta o categoría archivada |
| `CARD_HAS_PENDING_PURCHASES` | 409 | Archivar una tarjeta que debe |
| `PURCHASE_ALREADY_PAID` | 409 | Editar o eliminar una compra pagada |
| `ID_CONFLICT` | 409 | El `id` ya existe con otro contenido |
| `NOT_FOUND` | 404 | No existe o es de otro usuario |
