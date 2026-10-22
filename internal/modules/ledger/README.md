# Módulo ledger

El libro del usuario: sus **categorías** y sus **cuentas**; en los planes que siguen, movimientos y transferencias. Planes: [`03-ledger-categories.md`](../../../docs/plans/03-ledger-categories.md) y [`04-ledger-accounts.md`](../../../docs/plans/04-ledger-accounts.md).

## Tablas (dueño)

- `categories`, migración `00003`. `name` es lo que escribió la persona (recortado, con espacios simples); `name_normalized` (minúsculas, sin tildes, conserva la ñ) solo sirve para comparar y es nulo al archivar.
- `accounts`, migración `00004`. Guarda el saldo **inicial**; el saldo se calcula en `ListAccounts` (una sola consulta para todas las cuentas, a la que los planes 05 y 06 suman movimientos y transferencias).

Las dos migraciones dan lo inicial (las 9 categorías y "Efectivo") a los usuarios que ya existían.

## API pública (`module.go`)

| Qué | Para quién |
|---|---|
| `New(runner, clock, profile.Reader)`, `Routes(protected)` | `cmd/api/main.go`. Del perfil solo lee las monedas y el tipo de cambio |
| `OnUserRegistered(ctx, userID, mainCurrency)` | El gancho de alta de `identity` (lo cablea `main.go`, después del perfil): crea las 9 categorías y "Efectivo" en la misma transacción |

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

**Las dos**
- La app puede mandar el `id`: un reintento con el mismo contenido devuelve la existente (200); con otro, `ID_CONFLICT`.
- Archivar dos veces no es un error; editar una archivada es `404`.

## Endpoints

`GET/POST /categories`, `PATCH /categories/{id}`, `POST /categories/{id}/archive`; `GET/POST /accounts`, `PATCH /accounts/{id}`, `POST /accounts/{id}/archive`.

## Errores

| `code` | HTTP | Cuándo |
|---|---|---|
| `VALIDATION_FAILED` + `NAME_REQUIRED`, `NAME_TOO_LONG`, `KIND_INVALID`, `COLOR_INVALID`, `INVALID_FORMAT` | 400 | Categoría, en ese orden |
| `VALIDATION_FAILED` + `NAME_REQUIRED`, `NAME_TOO_LONG`, `TYPE_INVALID`, `CURRENCY_NOT_SUPPORTED`, `AMOUNT_INVALID`, `FIELD_NOT_EDITABLE` | 400 | Cuenta, en ese orden |
| `CATEGORY_NAME_TAKEN` (details: `name` / `NAME_TAKEN`) | 409 | Nombre de categoría repetido en ese tipo |
| `FX_RATE_NOT_CONFIGURED` | 422 | Cuenta en otra moneda sin tipo de cambio |
| `ID_CONFLICT` | 409 | El `id` ya existe con otro contenido o es de otro usuario |
| `NOT_FOUND` | 404 | No existe, está archivada (al editar) o es de otro usuario |
