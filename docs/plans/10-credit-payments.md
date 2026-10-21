# 10 · Pagar una compra con tarjeta

## Objetivo

Marcar una compra como pagada: en **una sola transacción** se crea el egreso en la cuenta con que se paga (con la categoría y la descripción de la compra) y la compra sale de pendientes. Además, las dos reglas del egreso de un pago: solo se edita su cuenta, monto y fecha, y eliminarlo devuelve la compra a pendiente.

## Pantallas de Android que cubre

- **Marcar como pagada** (`ui/feature/credit/PayCreditScreen.kt`; `ORBITA_SPEC` §7.10).
- **Editar/Eliminar movimiento** cuando el egreso es el pago de una compra (§7.5): nota fija, categoría y descripción bloqueadas, y el aviso al eliminar.
- Repositorios: `CreditRepository.pay`; `EntriesRepository.updateMovement` y `deleteMovement` (con `Movement.creditPurchaseId`).

## Módulos

`credit` y `ledger`, sin importarse:

- `ledger` expone `PaymentLedger` (crear el egreso de un pago dentro de la transacción en curso) y **declara** el puerto `PaymentReverter`, que llama al eliminar un egreso de pago.
- `credit` usa `PaymentLedger` e **implementa** `PaymentReverter`.
- `main.go` cablea los dos. Ninguno conoce el paquete interno del otro.

```go
// ledger (público)
type PaymentLedger interface {
    CreatePaymentExpense(ctx context.Context, in PaymentExpense) (transactionID uuid.UUID, err error)
}
// PaymentExpense: user_id, purchase_id, account_id, category_id, description, amount, occurred_on

// ledger (puerto que necesita; lo implementa credit)
type PaymentReverter interface {
    RevertPayment(ctx context.Context, userID, purchaseID uuid.UUID) error
}
```

Ambos corren con la `pgx.Tx` que lleva el contexto (`tx.Runner`), así que el egreso y la compra cambian juntos o nada.

## Endpoints

| Método y ruta | Request | Respuesta |
|---|---|---|
| `POST /credit-purchases/{id}/pay` | `{ "account_id", "paid_amount", "paid_on" }` | `200 { data: { purchase, transaction } }` |
| `PATCH /transactions/{id}` (plan 05) | Si es el egreso de un pago: solo `account_id`, `amount`, `occurred_on` | `200` |
| `DELETE /transactions/{id}` (plan 05) | — | `204`; si es el egreso de un pago, la compra vuelve a pendiente |

En `GET /entries` y en la respuesta de la transacción, `credit_purchase_id` deja de ser siempre `null`: trae la compra pagada con ese egreso (Android lo usa para bloquear la edición y cambiar el texto de la confirmación).

### Errores (orden de `ORBITA_SPEC` §4.1 para el pago: cuenta, monto, fecha)

| Situación | HTTP | `code` |
|---|---|---|
| Sin cuenta | 400 | `VALIDATION_FAILED` + `ACCOUNT_REQUIRED` |
| Monto ausente o ≤ 0 | 400 | `VALIDATION_FAILED` + `AMOUNT_REQUIRED` |
| Fecha de pago posterior a hoy | 400 | `VALIDATION_FAILED` + `FUTURE_DATE` |
| Cuenta archivada | 422 | `ACCOUNT_ARCHIVED` |
| La compra ya está pagada (también el segundo de dos pagos simultáneos) | 409 | `PURCHASE_ALREADY_PAID` |
| Cambiar la categoría o la descripción del egreso de un pago | 422 | `PAYMENT_EXPENSE_LOCKED` |
| Compra o cuenta de otro usuario | 404 | `NOT_FOUND` |

## Tablas y migraciones

Dos migraciones, cada una de su módulo:

- `00008_ledger_add_transaction_source.sql`: en `transactions`, `source VARCHAR(20) NOT NULL DEFAULT 'manual'` (`manual`, `credit_payment`) y `source_id UUID NULL` (la compra). Índice único `(source_id) WHERE source = 'credit_payment' AND deleted_at IS NULL`: una compra tiene a lo sumo un egreso de pago vivo. Compatible con la versión anterior (columna con valor por defecto).
- `00009_credit_add_purchase_payment.sql`: en `credit_purchases`, `paid_at TIMESTAMPTZ NULL` y `payment_transaction_id UUID NULL`. `CHECK ((status = 'paid') = (payment_transaction_id IS NOT NULL))`.

**La cuenta, el monto y la fecha del pago no se copian a la compra**: viven en el egreso, que es la fuente de verdad. Así editar el egreso no deja datos viejos en la compra.

## Reglas

- **Pagar** (`credit`, en `tx.Runner`): bloquea la compra (`SELECT … FOR UPDATE`) y exige `status = pending`; valida la cuenta por `AccountReader`; crea el egreso por `PaymentLedger` (categoría y descripción de la compra, `occurred_on = paid_on`, `amount = paid_amount`, `source = credit_payment`); marca la compra `paid` con el id del egreso. Todo o nada.
- **El monto realmente descontado** está en la moneda de la cuenta que paga. Si coincide con la de la compra, la app sugiere el monto de la compra; si no, lo escribe el usuario (el banco aplicó su propio cambio). El API no convierte ni compara.
- **Editar el egreso de un pago** (`ledger`): si `source = credit_payment`, solo cambian `account_id`, `amount` y `occurred_on`; un cambio de categoría o descripción → `422 PAYMENT_EXPENSE_LOCKED` (como `DemoEntriesRepository.updateMovement`). Para corregirlas: eliminar el egreso, editar la compra y pagarla de nuevo.
- **Eliminar el egreso de un pago** (`ledger`, en una transacción): borrado lógico del egreso y `PaymentReverter.RevertPayment`, que vuelve la compra a `pending`, limpia `paid_at` y `payment_transaction_id`, y **desarchiva su tarjeta** si estaba archivada, para que ninguna deuda quede oculta (regla del código de Android). Desde Movimientos nunca se elimina la compra.
- Una compra pagada no se edita ni se elimina (`409`, plan 09).
- El egreso del pago cuenta en saldos y reportes con la fecha de pago, como cualquier egreso.

## Dependencias

05 (movimientos) y 09 (compras).

## Pasos de implementación

1. Migraciones de `ledger` y `credit`.
2. `ledger`: `PaymentLedger`, puerto `PaymentReverter`, bloqueo de edición y reversión al eliminar; `credit_purchase_id` en las respuestas.
3. `credit`: caso de uso `Pay` e implementación de `PaymentReverter`.
4. Cableado en `main.go`.
5. Android: `RemoteCreditRepository.pay`; `PAYMENT_EXPENSE_LOCKED` → `DataError.PAYMENT_LOCKED`; `creditPurchaseId` desde el DTO.
6. OpenAPI y `ORBITA_SPEC.md`.

## Pruebas

Integración (datos de ejemplo):
- Pagar Pasajes desde "Débito principal" por 240.00: su saldo pasa de 1,245.80 a 1,005.80; la compra sale de pendientes; el egreso aparece en `/entries` con categoría Transporte y descripción "Pasajes", y en el reporte del mes con la fecha de pago.
- Pagar dos veces → el segundo `409`. Dos pagos simultáneos → uno `200`, otro `409`, y un solo egreso.
- Un fallo forzado al crear el egreso → la compra sigue pendiente (prueba de atomicidad).
- Editar el egreso cambiando cuenta, monto y fecha → `200`; cambiando la categoría o la descripción → `422`.
- Eliminar el egreso → la compra vuelve a pendiente y a la deuda de su tarjeta; el saldo vuelve a 1,245.80.
- Archivar una tarjeta sin pendientes, y luego eliminar el egreso de un pago de esa tarjeta → la tarjeta vuelve a aparecer.

## Criterios de aceptación

- Desde la app: pagar una compra crea el egreso y la saca de pendientes; eliminar ese egreso la devuelve, sin descuadrar ningún saldo.

## Variables nuevas para el servidor

Ninguna.

## Desviaciones

(Se completan al implementar.)
