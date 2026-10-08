# 06 · Fase 3 — Movimientos y transferencias

**Objetivo:** registrar, editar y eliminar ingresos, egresos y transferencias, y listarlos mezclados por fecha. Es el corazón de la app: de aquí salen los saldos y los reportes.

**Tamaño:** grande. **Depende de:** Fase 2. **Bloquea:** Fases 4, 5 y 7.

Pantallas de Android cubiertas: Nuevo/Editar movimiento, Transferir/Editar transferencia, Lista de movimientos (secciones 7.5, 7.6 y 7.7 de `ORBITA_SPEC.md`). Equivale a `EntriesRepository`.

## 1. Movimientos

| Método y ruta | Descripción |
|---|---|
| `POST /transactions` | Crea un ingreso o un egreso |
| `GET /transactions/{id}` | Uno, con cuenta y categoría embebidas |
| `PATCH /transactions/{id}` | Edita |
| `DELETE /transactions/{id}` | Borrado lógico |

Cuerpo de creación:

```json
{
  "id": "0b7e…",
  "kind": "expense",
  "amount": "45.52",
  "accountId": "…",
  "categoryId": "…",
  "description": "Almuerzo con equipo",
  "occurredOn": "2026-10-02"
}
```

Respuesta:

```json
{
  "id": "0b7e…", "type": "transaction", "kind": "expense", "amount": "45.52", "currency": "PEN",
  "occurredOn": "2026-10-02", "description": "Almuerzo con equipo",
  "account":  { "id": "…", "name": "Débito principal", "type": "debit", "currency": "PEN", "isArchived": false },
  "category": { "id": "…", "name": "Alimentación", "kind": "expense", "color": "#0E7490", "isArchived": false },
  "createdAt": "2026-10-02T17:31:08.412Z", "updatedAt": "2026-10-02T17:31:08.412Z"
}
```

Reglas, en el orden de revisión de la especificación (monto, cuenta, categoría, descripción, fecha):

| Regla | Código si falla |
|---|---|
| `amount` con formato de monto y `> 0` | `AMOUNT_REQUIRED` |
| `accountId` presente, propia y no borrada | `ACCOUNT_REQUIRED` / `NOT_FOUND` |
| Cuenta no archivada (al crear, o al cambiar de cuenta) | `ACCOUNT_ARCHIVED` |
| `categoryId` presente, propia y no borrada | `CATEGORY_REQUIRED` / `NOT_FOUND` |
| Categoría no archivada (al crear, o al cambiar de categoría) | `CATEGORY_ARCHIVED` |
| Si se envía `kind`, coincide con el de la categoría | `CATEGORY_KIND_MISMATCH` |
| `description` de hasta 500 caracteres, recortada | `DESCRIPTION_TOO_LONG` |
| `occurredOn` no posterior a hoy (zona horaria del usuario) | `FUTURE_DATE` |

Detalles:

- **`kind` se toma de la categoría.** El cliente puede enviarlo como comprobación; el valor guardado es siempre el de la categoría. La clave foránea compuesta impide que se desalinee.
- **`occurredOn` es opcional al crear.** Android no muestra el campo de fecha en "Nuevo movimiento": el **servicio** calcula hoy en la zona horaria del perfil y lo guarda. La columna es `DATE` (`@db.Date`) y **no tiene valor por defecto en la base**: nunca se usa `current_date`, que en un servidor en UTC daría el día equivocado. `createdAt` guarda el instante exacto y desempata el orden dentro del día.
- **Al editar** se pueden cambiar todos los campos, incluido pasar de egreso a ingreso (cambiando a una categoría del otro tipo) y mover el movimiento a otra cuenta. La moneda pasa a ser la de la cuenta nueva.
- **Editar un movimiento antiguo** cuya cuenta o categoría está archivada está permitido mientras no se cambie esa cuenta o categoría.
- **No se valida saldo suficiente.** Un saldo puede quedar negativo; Android solo avisa.
- **Eliminar** marca `deleted_at`. Repetirlo devuelve `204`. Un movimiento eliminado deja de contar en saldos y reportes y responde `404` a `GET` y `PATCH`.
- **Egreso nacido de pagar una compra con tarjeta** (a partir de la Fase 5). La respuesta de todo movimiento incluye `creditPurchaseId`: el identificador de la compra si el egreso es un pago, o `null`.
  - **Eliminarlo deshace el pago** (decisión del usuario): la compra vuelve a pendiente, en la misma transacción. Desde Movimientos nunca se elimina la compra en sí; eso solo se hace desde la deuda de la tarjeta. Detalle en [08](08-fase-5-tarjetas-de-credito.md) §4.
  - **Editarlo** está por decidir (D18). Propuesta: se pueden cambiar la cuenta, el monto y la fecha, que son los datos del pago, y el cambio se copia a la compra (`paid_account_id`, `paid_amount`, `paid_on`) en la misma transacción. La categoría y la descripción son las de la compra: cambiarlas desde el movimiento responde `422`, porque el reporte dejaría de coincidir con la compra.

## 2. Transferencias

| Método y ruta | Descripción |
|---|---|
| `POST /transfers` | Crea |
| `GET /transfers/{id}` | Una, con ambas cuentas embebidas |
| `PATCH /transfers/{id}` | Edita |
| `DELETE /transfers/{id}` | Borrado lógico |

Cuerpo:

```json
{
  "id": "…",
  "fromAccountId": "…", "toAccountId": "…",
  "fromAmount": "20.00", "toAmount": "63.40",
  "exchangeRate": "3.170000",
  "occurredOn": "2026-10-01",
  "note": "Cambio de dólares"
}
```

Reglas, en el orden de la especificación (cuentas, montos, cambio, nota, fecha):

| Regla | Código si falla |
|---|---|
| Cuentas distintas | `SAME_ACCOUNT` |
| Ambas cuentas propias, no borradas; no archivadas al crear o al cambiarlas | `NOT_FOUND` / `ACCOUNT_ARCHIVED` |
| `fromAmount` y `toAmount` con formato de monto y `> 0` | `AMOUNT_REQUIRED` |
| Misma moneda: `toAmount == fromAmount` | `AMOUNTS_MUST_MATCH` |
| Monedas distintas: `exchangeRate` presente y `> 0` | `RATE_REQUIRED` |
| Monedas distintas: el usuario tiene tipo de cambio configurado | `FX_RATE_NOT_CONFIGURED` |
| `note` de hasta 500 caracteres | `DESCRIPTION_TOO_LONG` |
| `occurredOn` no posterior a hoy | `FUTURE_DATE` |

Detalles:

- **Sin tipo de cambio configurado** no se puede operar en otra moneda ([05](05-fase-2-cuentas-categorias-ajustes.md) §3). En la práctica el usuario no puede llegar a tener dos cuentas de monedas distintas en ese estado; la comprobación en transferencias es una segunda barrera.
- **`occurredOn` es obligatoria** y se guarda tal cual en una columna `DATE`. La base no pone ninguna fecha por defecto.
- **Misma moneda:** `exchangeRate` se guarda como `null`, se envíe o no.
- **Los montos mandan; el cambio es informativo.** El servidor **no** exige que `fromAmount × exchangeRate` sea igual a `toAmount`. Con el cambio guardado a 6 decimales y montos grandes, esa igualdad no siempre se cumple aunque la operación sea correcta (H14). Lo que mueve los saldos son `fromAmount` y `toAmount`.
- El cambio se guarda tal como lo envió el cliente, con hasta 6 decimales, para que al editar se vea "el cambio con que se guardó".
- La regla "nunca queda la misma cuenta en ambos lados" (intercambiar al elegir la del otro lado) es de la interfaz. El servidor solo rechaza cuentas iguales.
- Como la moneda de una cuenta no cambia después de crearla, la comprobación de monedas no tiene condición de carrera.
- "Saldos después de transferir" lo calcula el cliente con los saldos de `GET /accounts`.

## 3. Lista mezclada

`GET /entries` devuelve movimientos y transferencias juntos, del más reciente al más antiguo. Reemplaza a `observeEntries(period)` y `observeRecentEntries(limit)`.

| Parámetro | Uso |
|---|---|
| `from`, `to` | Rango de fechas inclusivo. Opcionales |
| `accountId` | Solo esa cuenta. Una transferencia aparece si la cuenta es origen **o** destino |
| `q` | Busca en la descripción (movimientos) o la nota (transferencias), sin distinguir mayúsculas |
| `type` | `transaction` o `transfer`. Opcional |
| `limit` | Por defecto 50, máximo 100 |
| `cursor` | Página siguiente |

```json
{
  "data": [
    { "type": "transaction", "id": "…", "kind": "expense", "amount": "18.50", "currency": "PEN",
      "occurredOn": "2026-10-02", "description": "Almuerzo",
      "account": { "id": "…", "name": "Efectivo", "type": "cash", "currency": "PEN" },
      "category": { "id": "…", "name": "Alimentación", "kind": "expense", "color": "#0E7490" } },
    { "type": "transfer", "id": "…", "occurredOn": "2026-10-01", "note": "Cambio de dólares",
      "fromAmount": "20.00", "toAmount": "64.00", "exchangeRate": "3.200000",
      "from": { "id": "…", "name": "Cuenta Dólares", "type": "savings", "currency": "USD" },
      "to":   { "id": "…", "name": "Débito principal", "type": "debit", "currency": "PEN" } }
  ],
  "meta": { "nextCursor": "eyJ…" }
}
```

Implementación:

- Una sola consulta con `UNION ALL` de las dos tablas, ordenada por `occurred_on DESC, created_at DESC, id DESC`, con el cursor como condición de fila. Mezclar en el cliente o en memoria rompería la paginación.
- Las cuentas y categorías embebidas se cargan en una segunda consulta por lote (por los identificadores de la página), no una por fila.
- Las cuentas y categorías archivadas se incluyen embebidas: los movimientos antiguos siguen mostrándolas.
- La búsqueda `q` usa `ILIKE` con los comodines escapados. Como siempre va acotada por usuario y normalmente por rango de fechas, el conjunto a revisar es pequeño. Si la búsqueda sin rango se vuelve lenta, se agrega un índice de trigramas ([13](13-escalabilidad-y-operacion.md)).
- `q` tiene un máximo de 100 caracteres.

Cómo lo usa Android:

| Pantalla | Petición |
|---|---|
| Inicio, "Últimos movimientos" | Incluidos en `GET /home` ([07](07-fase-4-reportes-e-inicio.md)) |
| Lista de movimientos, un mes | `GET /entries?from=2026-10-01&to=2026-10-31&accountId=…&q=…` |
| Cuenta inicial de un movimiento nuevo | El primer movimiento de la lista reciente, como hoy |

El límite de "5 meses" de la lista es una decisión de interfaz; el API no lo impone.

## 4. Tareas

- [ ] Migración `entries`: `transactions`, `transfers`, con `CHECK`, claves foráneas compuestas e índices
- [ ] Módulo `transactions`: crear, leer, editar, eliminar
- [ ] Validación de cuenta y categoría propias y activas, y de `kind`
- [ ] Fecha por defecto con la zona horaria del usuario
- [ ] Módulo `transfers`: crear, leer, editar, eliminar, con las reglas de moneda
- [ ] Idempotencia de creación con `id` del cliente
- [ ] Módulo `entries`: consulta `UNION ALL`, cursor, filtros, carga por lote de embebidos
- [ ] Verificar con `EXPLAIN` que la lista y los saldos usan los índices previstos
- [ ] Actualizar la consulta de saldos de cuentas para incluir movimientos y transferencias
- [ ] Semilla de ejemplo completa (las 21 entradas de `SampleData.kt`)

## 5. Pruebas

Reglas (las mismas de `DemoRepositoriesTest` en Android, contra la semilla de ejemplo):

| Caso | Esperado |
|---|---|
| Egreso de 45.52 desde Débito principal | Saldo 1,245.80 → **1,200.28**; gastos de octubre 30.50 → **76.02** |
| El movimiento nuevo sin fecha | Toma la fecha de hoy y aparece primero |
| Eliminar "Almuerzo" (18.50, Efectivo) | Saldo 320.50 → **339.00**; gastos de octubre **12.00** |
| Transferencia US$ 20.00 → S/ 63.40 con cambio 3.17 | Cuenta Dólares **230.00**; Débito principal **1,309.20**; gastos de octubre siguen en **30.50** |
| Transferencia en la misma moneda con montos distintos | `422 AMOUNTS_MUST_MATCH` |
| Transferencia entre monedas sin cambio | `422 RATE_REQUIRED` |
| Transferencia a la misma cuenta | `422 SAME_ACCOUNT` |
| Movimiento con fecha de mañana | `422 FUTURE_DATE` |
| Categoría de ingreso en un egreso | `422 CATEGORY_KIND_MISMATCH` |
| Monto `0`, `-5`, `45.523`, `45,52`, `1e3` | `400` o `422` con `AMOUNT_REQUIRED` / `INVALID_FORMAT` |

Fechas y zona horaria:

- Usuario en `America/Lima`, reloj en `2026-10-03T01:00:00Z` (las 20:00 del 2 oct en Lima): un movimiento sin fecha queda en **2 oct**, y uno con fecha 3 oct se rechaza.
- Usuario en `Europe/Madrid`, reloj en `2026-10-02T22:30:00Z` (las 00:30 del 3 oct en Madrid): un movimiento con fecha 3 oct se acepta.

Lista:

- El orden mezcla movimientos y transferencias por fecha y, dentro del día, por creación.
- Recorrer todas las páginas con `limit=5` devuelve cada entrada exactamente una vez, también si se crean entradas nuevas a mitad del recorrido.
- `accountId` incluye las transferencias donde la cuenta es origen o destino.
- `q=super` encuentra "Supermercado"; `q=%` no devuelve todo.
- Un cursor manipulado devuelve `400`, no un error interno.

Seguridad:

- El usuario B no puede leer, editar ni eliminar entradas de A (`404`).
- B no puede crear un movimiento en una cuenta de A, ni con una categoría de A (`404`, y además la base lo rechaza si se salta el servicio).
- B no puede transferir desde o hacia una cuenta de A.
- Crear dos veces con el mismo `id` y el mismo cuerpo crea una sola fila.

## 6. Criterios de aceptación

- Con la semilla, crear un egreso de 45.52 cambia el saldo en 45.52 exactos (criterio de la Fase 3 de Android).
- Los totales del mes excluyen las transferencias.
- La lista de un mes responde en una sola petición, paginada.
- Todas las pruebas de la sección 5 pasan.

## 7. Riesgos

| Riesgo | Mitigación |
|---|---|
| Errores de un día por zona horaria | Reloj inyectado y pruebas explícitas con dos zonas |
| La consulta `UNION ALL` con cursor es la más compleja del proyecto | Prueba de recorrido completo con datos generados al azar, comparada con un orden calculado en memoria |
| Una cadena de monto se convierte a `number` por descuido | Tipo `Decimal` de extremo a extremo y prueba con `0.1 + 0.2` |
