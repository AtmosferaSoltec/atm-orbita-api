# Módulo ledger

El libro del usuario: sus **categorías**, sus **cuentas**, sus **movimientos** (ingresos y egresos) y sus **transferencias**. Planes: [`03`](../../../docs/plans/03-ledger-categories.md), [`04`](../../../docs/plans/04-ledger-accounts.md), [`05`](../../../docs/plans/05-ledger-transactions.md) y [`06`](../../../docs/plans/06-ledger-transfers.md).

## Tablas (dueño)

- `categories`, migración `00003`. `name` es lo que escribió la persona (recortado, con espacios simples); `name_normalized` (minúsculas, sin tildes, conserva la ñ) solo sirve para comparar y es nulo al archivar.
- `accounts`, migración `00004`. Guarda el saldo **inicial**; el saldo se calcula en `ListAccounts` (una sola consulta para todas las cuentas: saldo inicial + ingresos − egresos + transferencias que entran − transferencias que salen).
- `transactions`, migración `00005`. Claves foráneas compuestas `(account_id, user_id)` y `(category_id, user_id, kind)`: la base rechaza un movimiento que apunte a la cuenta o categoría de otra persona, o a una categoría de otro tipo. Borrado lógico (`deleted_at`).
- `transfers`, migración `00006`. Las dos cuentas con claves foráneas compuestas, nunca la misma; borrado lógico.

Las migraciones `00003` y `00004` dan lo inicial (las 9 categorías y "Efectivo") a los usuarios que ya existían.

## API pública (`module.go`)

| Qué | Para quién |
|---|---|
| `New(runner, clock, profile.Reader)`, `Routes(protected)` | `cmd/api/main.go`. Del perfil lee las monedas, el tipo de cambio y "hoy" en su zona |
| `OnUserRegistered(ctx, userID, mainCurrency)` | El gancho de alta de `identity` (lo cablea `main.go`, después del perfil): crea las 9 categorías y "Efectivo" en la misma transacción |
| `CategoryReader()`: `Categories(ctx, userID, ids)`, también las archivadas | El módulo `credit` (plan 09), para validar y mostrar la categoría de una compra |
| `ReportQuery()`: `TotalsByCategory(ctx, userID, from, to)` | El módulo `reports` (plan 07): sumas de los movimientos por moneda de la cuenta, tipo y categoría, hechas en la base |

## Reglas

**Categorías**
- Iniciales: egreso → Alimentación, Transporte, Vivienda, Salud, Ocio, Otros; ingreso → Sueldo, Freelance, Otros ingresos (`domain.InitialCategories`, los colores de `SampleData.kt`).
- El nombre no se repite dentro del mismo tipo, sin distinguir mayúsculas ni tildes. Lo decide el índice único de la base (no una lectura previa), así dos altas simultáneas no pasan las dos.
- El tipo no cambia. Archivar la oculta y libera su nombre; los movimientos que ya la usan la conservan.
- La lista sale en el orden en que se crearon.

**Cuentas**
- Al registrarse: "Efectivo", tipo `cash`, en la moneda principal, saldo 0, en ahorros.
- La moneda y el saldo inicial no cambian después de crearla (`PATCH` los rechaza con `FIELD_NOT_EDITABLE`). El saldo inicial no puede ser negativo al crear.
- Sin tipo de cambio, solo se crean cuentas en la moneda principal.
- Ahorro total: suma de las cuentas activas con `include_in_savings`, convertidas sin redondear y redondeadas una sola vez al final (HALF_UP), como `FxPair.savingsTotal` de Android. Con tipo de cambio sale en las dos monedas del par; sin él, solo en la principal. Una cuenta fuera del par no suma.
- Orden: `sort_order`, luego `created_at`. La primera es la cuenta por defecto en Android.

**Movimientos**
- La moneda es la de su cuenta; el tipo (`kind`), el de su categoría. Cambiar a una categoría del otro tipo convierte el egreso en ingreso.
- Sin `occurred_on`, la fecha es hoy en la zona del perfil. Nunca posterior a hoy; hacia atrás no hay tope.
- Una cuenta o categoría archivada no se usa en movimientos nuevos ni al cambiar la de uno existente (sí se conserva si ya era la suya).
- Eliminar es lógico y deja de contar en saldos y listas. Eliminar dos veces no es un error.
- La lista (`GET /entries`): `occurred_on DESC`, luego `created_at DESC`; `q` busca en la descripción sin distinguir mayúsculas (escapa `%`, `_` y `\`); `account_id` filtra; páginas de hasta 100; periodo de hasta 10 años.

**Transferencias**
- No son ingreso ni gasto. Lo que mueve los saldos son los dos montos; el tipo de cambio es informativo y el API no lo recalcula.
- Misma moneda: sin tipo de cambio y con montos iguales. Monedas distintas: tipo de cambio obligatorio y mayor que cero.
- Sin `occurred_on` la fecha es hoy (Android siempre la manda). En un `PATCH`, `exchange_rate: null` lo borra y no mandarlo lo deja como estaba.
- En la lista se mezclan con los movimientos (`entry_type: "transfer"`); `q` busca en la nota y `account_id` coincide con el origen o el destino.

**Todo**
- La app puede mandar el `id`: un reintento con el mismo contenido devuelve el existente (200); con otro, `ID_CONFLICT`. En un movimiento sin fecha, la fecha no cuenta para comparar.
- Archivar dos veces no es un error; editar algo archivado o eliminado es `404`.

## Endpoints

`GET/POST /categories`, `PATCH /categories/{id}`, `POST /categories/{id}/archive`; `GET/POST /accounts`, `PATCH /accounts/{id}`, `POST /accounts/{id}/archive`; `POST /transactions`, `PATCH`/`DELETE /transactions/{id}`; `POST /transfers`, `PATCH`/`DELETE /transfers/{id}`; `GET /entries`.

## Errores

| `code` | HTTP | Cuándo |
|---|---|---|
| `VALIDATION_FAILED` + `NAME_REQUIRED`, `NAME_TOO_LONG`, `KIND_INVALID`, `COLOR_INVALID`, `INVALID_FORMAT` | 400 | Categoría, en ese orden |
| `VALIDATION_FAILED` + `NAME_REQUIRED`, `NAME_TOO_LONG`, `TYPE_INVALID`, `CURRENCY_NOT_SUPPORTED`, `AMOUNT_INVALID`, `FIELD_NOT_EDITABLE` | 400 | Cuenta, en ese orden |
| `VALIDATION_FAILED` + `AMOUNT_REQUIRED`, `ACCOUNT_REQUIRED`, `CATEGORY_REQUIRED`, `DESCRIPTION_TOO_LONG`, `FUTURE_DATE` (o `INVALID_FORMAT`) | 400 | Movimiento, en ese orden |
| `VALIDATION_FAILED` + `INVALID_FORMAT` / `RANGE_INVALID` | 400 | `GET /entries` con fechas, página o `account_id` mal formados, o un periodo invertido o de 10 años o más |
| `VALIDATION_FAILED` + `ACCOUNT_REQUIRED`, `SAME_ACCOUNT`, `AMOUNT_REQUIRED`, `RATE_REQUIRED` / `AMOUNTS_MUST_MATCH`, `DESCRIPTION_TOO_LONG`, `FUTURE_DATE` | 400 | Transferencia, en ese orden |
| `ACCOUNT_ARCHIVED` / `CATEGORY_ARCHIVED` | 422 | Movimiento o transferencia con una cuenta (o categoría) archivada |
| `CATEGORY_NAME_TAKEN` (details: `name` / `NAME_TAKEN`) | 409 | Nombre de categoría repetido en ese tipo |
| `FX_RATE_NOT_CONFIGURED` | 422 | Cuenta en otra moneda sin tipo de cambio |
| `ID_CONFLICT` | 409 | El `id` ya existe con otro contenido o es de otro usuario |
| `NOT_FOUND` | 404 | No existe, está archivada (al editar) o es de otro usuario |
