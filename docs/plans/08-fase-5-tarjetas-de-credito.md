# 08 · Fase 5 — Tarjetas de crédito

**Objetivo:** llevar la deuda de las tarjetas de crédito como recordatorio: registrar compras pendientes agrupadas por tarjeta y, al pagarlas, convertirlas en un egreso real de forma atómica.

**Tamaño:** mediano. **Depende de:** Fase 3.

Pantallas de Android cubiertas: Tarjetas de Crédito, Nueva/Editar tarjeta, Marcar como pagada, y el interruptor "Compra con tarjeta de crédito" de Nuevo movimiento (secciones 7.9, 7.10 y 7.5 de `ORBITA_SPEC.md`). Equivale a `CreditRepository`.

Esta fase cierra el pendiente de esquema que Android dejó anotado: la tabla de tarjetas y su relación con las compras (H3).

## 1. La regla central

Una compra con tarjeta **no es un movimiento**. Mientras está pendiente no afecta saldos, ahorro total ni reportes. Al marcarla como pagada, y solo entonces, nace un egreso en la cuenta elegida, con la categoría original y la fecha del pago.

Por eso las compras viven en su propia tabla (`credit_purchases`) y los reportes solo leen `transactions`.

## 2. Tarjetas

| Método y ruta | Descripción |
|---|---|
| `GET /credit-cards` | Tarjetas no archivadas con su resumen de deuda. `?includeArchived=true` |
| `POST /credit-cards` | Crea |
| `PATCH /credit-cards/{id}` | Cambia `name` y `currency` |
| `POST /credit-cards/{id}/archive` | Archiva |

```json
{
  "data": [
    { "id": "…", "name": "Visa Clásica", "currency": "PEN", "isArchived": false,
      "pendingCount": 2, "nextDueDate": "2026-10-05",
      "debt": [ { "currency": "PEN", "total": "336.00" } ] },
    { "id": "…", "name": "Mastercard Oro", "currency": "PEN", "isArchived": false,
      "pendingCount": 2, "nextDueDate": "2026-10-15",
      "debt": [ { "currency": "PEN", "total": "189.90" }, { "currency": "USD", "total": "12.00" } ] }
  ],
  "meta": {
    "cardCount": 2, "pendingCount": 4, "nextDueDate": "2026-10-05",
    "debt": [ { "currency": "PEN", "total": "525.90" }, { "currency": "USD", "total": "12.00" } ]
  }
}
```

Reglas:

- `name` 1–60; `currency` del catálogo, por defecto la moneda principal del usuario.
- **La deuda nunca mezcla monedas.** Cada lista `debt` va con la moneda principal primero (regla de `debtCurrencies`).
- Cambiar la moneda de una tarjeta afecta solo a las compras **nuevas**; las ya registradas conservan la suya. Por eso una tarjeta en soles puede deber también en dólares.
- Orden de las tarjetas: primero la de fecha límite más próxima; las que no deben nada, al final.
- **No se puede archivar una tarjeta con compras pendientes** (decisión del usuario, 7 oct 2026). Primero hay que pagarlas o eliminarlas; mientras tanto, `POST /credit-cards/{id}/archive` responde `409 CARD_HAS_PENDING_PURCHASES` con el número de compras pendientes. Así nunca hay deuda invisible: toda compra pendiente pertenece a una tarjeta que se ve en la pantalla. Cambia el comportamiento actual de Android, que sí lo permitía.
- La comprobación y el archivado van en una transacción con actualización condicionada, para que no se pueda registrar una compra en la tarjeta justo mientras se archiva.
- Una tarjeta archivada conserva sus compras **pagadas**, que siguen apareciendo en el historial con su tarjeta.

"Urgente" (vence en 3 días o menos, o ya venció) lo calcula el cliente con `nextDueDate` y su fecha de hoy.

## 3. Compras

| Método y ruta | Descripción |
|---|---|
| `GET /credit-purchases` | Compras. `?status=pending` (por defecto) o `paid`; `?cardId=` |
| `POST /credit-purchases` | Registra una compra pendiente |
| `GET /credit-purchases/{id}` | Una |
| `PATCH /credit-purchases/{id}` | Edita una compra pendiente (sección 4) |
| `DELETE /credit-purchases/{id}` | Elimina una compra pendiente (sección 4) |
| `POST /credit-purchases/{id}/pay` | Marca como pagada y crea el egreso (sección 5) |
| `POST /credit-purchases/{id}/unpay` | Deshace el pago: la compra vuelve a pendiente. Es la misma operación que eliminar su egreso (sección 4) |

Editar y eliminar una compra pendiente quedaron **aprobados por el usuario** (7 oct 2026): sin ellos, una compra con el monto mal escrito no tenía arreglo. Android aún no tiene esas pantallas ([14](14-integracion-android.md)).

Cuerpo de creación:

```json
{ "id": "…", "cardId": "…", "amount": "240.00", "categoryId": "…",
  "description": "Pasajes", "dueDate": "2026-10-05" }
```

Respuesta:

```json
{ "id": "…", "status": "pending", "amount": "240.00", "currency": "PEN",
  "description": "Pasajes", "purchaseDate": "2026-09-10", "dueDate": "2026-10-05",
  "card": { "id": "…", "name": "Visa Clásica", "currency": "PEN", "isArchived": false },
  "category": { "id": "…", "name": "Transporte", "kind": "expense", "color": "#B45309" } }
```

Reglas de creación, en el orden de la especificación (monto, tarjeta, categoría, descripción, fecha límite):

| Regla | Código si falla |
|---|---|
| `amount` con formato de monto y `> 0` | `AMOUNT_REQUIRED` |
| `cardId` presente, propia, no borrada ni archivada | `CARD_REQUIRED` / `NOT_FOUND` / `CARD_ARCHIVED` |
| `categoryId` presente, propia, activa y **de egreso** | `CATEGORY_REQUIRED` / `NOT_FOUND` / `CATEGORY_KIND_MISMATCH` |
| `description` de hasta 500 caracteres | `DESCRIPTION_TOO_LONG` |
| `dueDate` no anterior a hoy | `DUE_DATE_PAST` |

Detalles:

- `currency` **no se envía**: se copia de la tarjeta.
- `purchaseDate` es hoy según la zona horaria del usuario; se admite enviarla si no es posterior a hoy.
- La fecha límite es la única fecha del sistema que puede estar en el futuro.
- Las compras pendientes se devuelven ordenadas por `dueDate` ascendente.
- La lista de pendientes no se pagina (son pocas por naturaleza); la de pagadas sí, por cursor.
- Registrar una compra en una tarjeta cuya moneda no es la principal exige tener tipo de cambio configurado ([05](05-fase-2-cuentas-categorias-ajustes.md) §3); en la práctica esa tarjeta no puede existir sin él.

## 4. Editar y eliminar una compra pendiente

### Editar — `PATCH /credit-purchases/{id}`

Campos que se pueden cambiar: `amount`, `categoryId`, `description`, `dueDate`, `purchaseDate` y `cardId`.

| Regla | Código si falla |
|---|---|
| La compra existe, es propia y no está eliminada | `NOT_FOUND` |
| La compra está **pendiente** | `PURCHASE_ALREADY_PAID` (409) |
| `amount` con formato de monto y `> 0` | `AMOUNT_REQUIRED` |
| `cardId`, si cambia: tarjeta propia y no archivada | `NOT_FOUND` / `CARD_ARCHIVED` |
| `categoryId`, si cambia: propia, activa y de egreso | `NOT_FOUND` / `CATEGORY_ARCHIVED` / `CATEGORY_KIND_MISMATCH` |
| `description` de hasta 500 caracteres | `DESCRIPTION_TOO_LONG` |
| `purchaseDate` no posterior a hoy | `FUTURE_DATE` |
| `dueDate` no anterior a `purchaseDate` | `DUE_DATE_PAST` |

Dos diferencias con la creación:

- **`dueDate` puede ser anterior a hoy.** Una compra ya vencida tiene que poder corregirse (su monto, su descripción) sin obligar a mover la fecha límite. Lo que se mantiene es que no sea anterior a la fecha de compra.
- **`currency` no se edita directamente.** Si se cambia `cardId`, la compra toma la moneda de la tarjeta nueva. Si la tarjeta no cambia, conserva la suya.

### Eliminar — `DELETE /credit-purchases/{id}`

Borrado lógico (`deleted_at`). Solo si está pendiente; si está pagada, `409 PURCHASE_ALREADY_PAID`. Repetirlo sobre una compra ya eliminada devuelve `204`.

### Una compra ya pagada

Decisión del usuario (7 oct 2026), para que la información no se altere:

- **Una compra solo se elimina desde la deuda de la tarjeta, y solo mientras está pendiente.** Una compra pagada no se edita ni se elimina (`409 PURCHASE_ALREADY_PAID`).
- **Desde Movimientos nunca se elimina una compra.** Lo que el usuario ve ahí es el egreso del pago. Si lo elimina, **la compra vuelve a pendiente**: reaparece en la deuda de su tarjeta, como si el pago no se hubiera hecho.
- Para borrar del todo una compra ya pagada hay dos pasos, en ese orden: eliminar el egreso (o deshacer el pago), y después eliminar la compra pendiente desde la tarjeta.

`DELETE /transactions/{id}` sobre el egreso de un pago y `POST /credit-purchases/{id}/unpay` hacen exactamente lo mismo, dentro de `prisma.$transaction`:

1. Borran lógicamente el egreso (`deleted_at`).
2. Devuelven la compra a `pending` y anulan `paid_on`, `paid_account_id`, `paid_amount` y `transaction_id`, con una actualización condicionada a que siga pagada con ese egreso.
3. Si la condición no se cumple (otra petición ya lo deshizo), no cambian nada y responden como una eliminación repetida.

Efecto en las cifras, todas calculadas al leer: el saldo de la cuenta de pago recupera el monto, el egreso sale de los reportes y la deuda de la tarjeta vuelve a incluir la compra.

Para que la app pueda avisar antes de eliminar, el movimiento trae `creditPurchaseId` cuando nació de un pago ([06](06-fase-3-movimientos-transferencias.md) §1). El aviso sugerido: "Este egreso es el pago de una compra con tarjeta. Al eliminarlo, la compra volverá a estar pendiente."

Si la tarjeta de esa compra fue archivada después del pago, deshacerlo la deja con una compra pendiente; en ese caso la tarjeta **se desarchiva** en la misma transacción, para mantener la regla de que no hay deuda en tarjetas archivadas.

Qué se puede **editar** en el egreso de un pago sigue abierto (D18, [06](06-fase-3-movimientos-transferencias.md) §1).

### Cómo se recalculan los saldos

**No hay nada que recalcular a mano, porque ningún total está guardado.** Cada cifra se obtiene sumando en el momento de leerla:

| Cifra | De dónde sale | Efecto de editar o eliminar una compra **pendiente** |
|---|---|---|
| Saldo de una cuenta | Saldo inicial + movimientos + transferencias | **Ninguno.** Una compra pendiente no es un movimiento y no toca ninguna cuenta |
| Ahorro total | Suma de saldos de cuentas | **Ninguno** |
| Reportes y totales del mes | Solo `transactions` | **Ninguno** |
| Deuda de la tarjeta | Suma de sus compras pendientes, por moneda | Cambia: la siguiente lectura ya suma el monto nuevo, o deja de sumar la compra eliminada |
| Deuda total | Suma de todas las compras pendientes, por moneda | Cambia igual |
| "N pagos pendientes" y "vence el…" | Conteo y fecha mínima de las pendientes | Cambian igual |

Si al editar se mueve la compra a otra tarjeta, la deuda baja en una y sube en la otra en la misma lectura, porque ambas se calculan de las mismas filas.

Los saldos de las cuentas **solo** cambian al pagar (nace un egreso) y al deshacer el pago (el egreso se elimina lógicamente). En ambos casos tampoco se actualiza ningún saldo: el egreso entra o sale de la suma.

Esto es consecuencia directa de la regla "el saldo no se guarda": no existe un contador que pueda quedar desfasado si una operación falla a medias.

### Transacción

Editar y eliminar corren dentro de `prisma.$transaction`, aunque escriban una sola fila, por dos razones:

1. **Las comprobaciones y la escritura deben ver el mismo estado.** Validar que la tarjeta y la categoría nuevas son propias y están activas, y escribir, ocurre en una sola unidad.
2. **No puede cruzarse con un pago.** Si el usuario edita una compra en un dispositivo mientras la paga en otro, sin protección podría nacer un egreso con el monto viejo sobre una compra con el monto nuevo.

Pasos de la edición:

```
prisma.$transaction(async (tx) => {
  1. Validar referencias: tarjeta y categoría nuevas (propias, activas, categoría de egreso).
  2. Actualizar con condición:
       UPDATE credit_purchases SET …
        WHERE id = ? AND user_id = ? AND status = 'pending' AND deleted_at IS NULL
  3. Si no se actualizó ninguna fila: leer la compra para distinguir
       no existe / es de otro usuario / eliminada → 404
       está pagada                                → 409
     y lanzar el error, lo que revierte la transacción.
  4. Devolver la compra actualizada.
})
```

La eliminación es igual, con `SET deleted_at = now()` en el paso 2.

La **actualización condicionada** es lo que resuelve la concurrencia. Pagar (sección 5) usa la misma condición sobre la misma fila, y PostgreSQL hace que una de las dos operaciones espere a la otra:

| Orden real | Resultado |
|---|---|
| Edita primero, paga después | El pago lee los datos ya editados y el egreso nace con el monto nuevo |
| Paga primero, edita después | La edición no encuentra la fila en `pending` → `409 PURCHASE_ALREADY_PAID`, sin cambios |
| Elimina primero, paga después | El pago no encuentra la fila → `404` |
| Paga primero, elimina después | `409`, sin cambios |

En ningún orden queda una compra pagada sin egreso, ni un egreso que no coincida con su compra.

## 5. Pagar

`POST /credit-purchases/{id}/pay`

```json
{ "accountId": "…", "paidAmount": "240.00", "paidOn": "2026-10-02" }
```

Reglas, en el orden de la especificación (cuenta, monto, fecha):

| Regla | Código si falla |
|---|---|
| `accountId` presente, propia, no archivada | `ACCOUNT_REQUIRED` / `NOT_FOUND` / `ACCOUNT_ARCHIVED` |
| `paidAmount` con formato de monto y `> 0` | `AMOUNT_REQUIRED` |
| `paidOn` no posterior a hoy (por defecto, hoy) | `FUTURE_DATE` |
| La compra existe, es propia y está pendiente | `NOT_FOUND` / `PURCHASE_ALREADY_PAID` |

`paidAmount` es **lo que el banco realmente descontó, en la moneda de la cuenta de pago**. Si la compra fue en dólares y se paga desde una cuenta en soles, es el monto en soles. El servidor no convierte ni compara con el monto de la compra: la sugerencia del monto es cosa del cliente.

**Atomicidad.** Dentro de `prisma.$transaction`:

1. Se marca la compra como pagada con una actualización condicionada: `… WHERE id = ? AND user_id = ? AND status = 'pending' AND deleted_at IS NULL`.
2. Si esa actualización no tocó ninguna fila, la compra no existe o ya estaba pagada: se cancela todo y se responde `404` o `409`.
3. Se lee la compra **después** de esa actualización, ya con la fila bloqueada, para tomar su categoría y descripción vigentes (pudo editarse un instante antes; sección 4).
4. Se inserta el egreso en `transactions`: cuenta de pago, categoría de la compra, `paidAmount`, `paidOn`, descripción de la compra.
5. Se guarda en la compra el `transaction_id`, `paid_on`, `paid_account_id` y `paid_amount`.

Si dos peticiones llegan a la vez, solo una gana la actualización condicionada; la otra recibe `409 PURCHASE_ALREADY_PAID`. No puede nacer un egreso duplicado. Reemplaza a la función `pay_credit_purchase` del esquema original.

Respuesta: `200` con la compra ya pagada y el movimiento creado.

**Deshacer** (`unpay`, o eliminar el egreso del pago): descrito en la sección 4, "Una compra ya pagada". `unpay` responde `409` si la compra no está pagada.

## 6. Tareas

- [ ] Migración `credit`: `credit_cards`, `credit_purchases`, con `CHECK`, claves foráneas compuestas e índices
- [ ] Módulo `credit`, parte de tarjetas: CRUD, archivar, resumen de deuda por moneda
- [ ] Parte de compras: crear, listar, leer
- [ ] Editar una compra pendiente, dentro de `prisma.$transaction` con actualización condicionada
- [ ] Eliminar una compra pendiente, igual
- [ ] `pay` transaccional con actualización condicionada y lectura posterior
- [ ] Deshacer el pago como una sola operación compartida por `unpay` y por `DELETE /transactions/{id}` del egreso de un pago
- [ ] `creditPurchaseId` en la respuesta de los movimientos
- [ ] Bloqueo de archivado de tarjetas con compras pendientes (`409 CARD_HAS_PENDING_PURCHASES`) y desarchivado al deshacer un pago
- [ ] Completar `credit` en `GET /home`
- [ ] Agregar tarjetas y compras a la semilla de ejemplo
- [ ] Incluir tarjetas y compras en la cascada de `DELETE /me`

## 7. Pruebas

Editar y eliminar compras pendientes, con la semilla de ejemplo:

| Caso | Esperado |
|---|---|
| Cambiar "Pasajes" de S/ 240.00 a S/ 250.00 | Visa Clásica debe **S/ 346.00**; deuda total **S/ 535.90 + US$ 12.00** |
| Tras esa edición, saldos de cuentas, ahorro total y reporte de octubre | **Sin cambios**: Débito principal 1,245.80; ahorro 7,166.30; gastos 30.50 |
| Eliminar "Cena en restaurante" (S/ 96.00) | Visa Clásica debe S/ 240.00; deuda total S/ 429.90 + US$ 12.00; 3 pagos pendientes |
| Tras eliminar, saldos y reportes | Sin cambios |
| Mover "Audífonos" de Mastercard Oro a Visa Clásica | Mastercard baja S/ 189.90 y Visa sube S/ 189.90; la deuda total no cambia |
| Editar una compra ya pagada | `409 PURCHASE_ALREADY_PAID`, sin cambios |
| Eliminar una compra ya pagada | `409`, y el egreso sigue existiendo |
| Editar una compra vencida sin tocar su fecha límite | `200` |
| `dueDate` anterior a `purchaseDate` | `422 DUE_DATE_PAST` |
| Eliminar dos veces | `204` las dos |
| Editar y pagar la misma compra a la vez (repetido muchas veces) | Siempre uno de los dos resultados válidos de la sección 4; nunca un egreso con un monto distinto del de su compra |
| Eliminar y pagar la misma compra a la vez | Nunca queda un egreso de una compra eliminada |
| Fallo forzado después de la actualización, dentro de la transacción | La compra queda exactamente como estaba |
| El usuario B edita o elimina una compra de A | `404`, sin cambios |
| B mueve una compra propia a una tarjeta de A | `404` |

Con la semilla de ejemplo (las mismas reglas de `DemoRepositoriesTest`):

| Caso | Esperado |
|---|---|
| Deuda total | **S/ 525.90 + US$ 12.00**, 4 pagos, 2 tarjetas, vence 5 oct |
| Visa Clásica | S/ 336.00 (Pasajes 240.00 + Cena 96.00) |
| Mastercard Oro | S/ 189.90 + US$ 12.00 |
| Registrar una compra de S/ 80.00 | 5 pendientes; Débito principal sigue en **1,245.80**; gastos de octubre siguen en **30.50** |
| Pagar "Pasajes" con S/ 240.00 desde Débito principal | Saldo **1,005.80**; gastos de octubre **270.50**; "Pasajes" sale de pendientes; el egreso nuevo tiene descripción "Pasajes" y categoría "Transporte" |
| Pagar dos veces la misma compra | La segunda da `409` y existe un solo egreso |
| Dos pagos simultáneos de la misma compra | Uno `200`, otro `409`, un solo egreso |
| Pagar "Suscripción" (US$ 12.00) desde una cuenta en soles con S/ 39.00 | Egreso de S/ 39.00; la deuda en USD baja a cero |
| Fallo forzado al insertar el egreso | La compra sigue pendiente |
| `paidOn` de mañana | `422 FUTURE_DATE` |
| `dueDate` de ayer al crear | `422 DUE_DATE_PAST` |
| Categoría de ingreso | `422 CATEGORY_KIND_MISMATCH` |
| Cambiar la moneda de una tarjeta a USD | Las compras anteriores siguen en PEN; las nuevas nacen en USD |
| Deshacer un pago | El egreso desaparece de saldos y reportes; la compra vuelve a pendientes |
| Pagar "Pasajes" y luego eliminar ese egreso con `DELETE /transactions/{id}` | Débito principal vuelve a **1,245.80**; gastos de octubre vuelven a **30.50**; "Pasajes" reaparece como pendiente y Visa Clásica vuelve a deber S/ 336.00 |
| El egreso de un pago en `GET /entries` | Trae `creditPurchaseId`; un movimiento normal lo trae en `null` |
| Eliminar dos veces el egreso de un pago | `204` las dos; la compra queda pendiente una sola vez |
| Eliminar el egreso y pagar otra vez la compra | Funciona: nace un egreso nuevo |
| `DELETE /credit-purchases/{id}` de una compra pagada | `409`; el egreso y la compra siguen intactos |
| Archivar Visa Clásica con sus 2 compras pendientes | `409 CARD_HAS_PENDING_PURCHASES`; la tarjeta sigue activa |
| Pagar o eliminar sus 2 compras y archivarla | `204` |
| Archivar una tarjeta y registrar una compra en ella a la vez | Nunca queda una compra pendiente en una tarjeta archivada |
| Deshacer el pago de una compra cuya tarjeta se archivó después | La compra vuelve a pendiente y la tarjeta vuelve a estar activa |

Seguridad:

- El usuario B no puede ver, pagar, editar ni eliminar compras de A.
- B no puede pagar una compra propia desde una cuenta de A.
- B no puede registrar una compra en una tarjeta de A.

## 8. Criterios de aceptación

- Una compra pendiente no cambia saldos ni reportes (criterio de la Fase 6 de Android), tampoco al editarla o eliminarla.
- Una compra pendiente mal escrita se puede corregir o eliminar, y la deuda de su tarjeta lo refleja en la siguiente lectura.
- Al pagarla se crea el egreso en la cuenta elegida, con la fecha de pago y la categoría original, y recién entonces cuenta en los reportes.
- No existe ninguna secuencia de peticiones, ni simultáneas, que deje una compra pagada sin egreso o con dos.

## 9. Riesgos

| Riesgo | Mitigación |
|---|---|
| Una edición se cruza con un pago y el egreso nace con datos viejos | Misma actualización condicionada en ambas operaciones, dentro de `prisma.$transaction`, con prueba de concurrencia |
| Doble pago por reintento de red | Actualización condicionada dentro de la transacción, con prueba de concurrencia |
| El usuario elimina el egreso de un pago y la compra queda "pagada" sin egreso | No puede ocurrir: eliminar ese egreso devuelve la compra a pendiente en la misma transacción |
| Tarjeta archivada con deuda invisible | No puede ocurrir: no se archiva con compras pendientes, y deshacer un pago la desarchiva |
| El usuario edita el egreso de un pago y deja de coincidir con la compra | Pendiente de decidir qué campos se pueden editar (D18) |
