# Módulo profile

Las preferencias del usuario: sus dos monedas, en cuál ve el ahorro total, su zona horaria y el tipo de cambio manual entre las dos, que **puede no existir**. Plan: [`docs/plans/02-profile-settings.md`](../../../docs/plans/02-profile-settings.md).

## Tablas (dueño)

`profiles` (una fila por usuario) y `exchange_rates` (historial; cada cambio es una fila nueva), migración `00002`. Un tipo de cambio que no existe es **ausencia de fila**, nunca un valor nulo.

## API pública (`module.go`)

| Qué | Para quién |
|---|---|
| `New(runner, clock)`, `Routes(public, protected)` | `cmd/api/main.go` |
| `OnUserRegistered(ctx, userID, mainCurrency, timezone)` | El gancho de alta de `identity` (lo cablea `main.go`): crea el perfil en la misma transacción |
| `Reader()`: `Settings`, `Today`, `RequireUsable` | Los módulos que necesiten las monedas, "hoy" en la zona del usuario, o saber si una moneda se puede usar |
| `ErrFxNotConfigured`, `IsFxNotConfigured` | Para traducir a `422 FX_RATE_NOT_CONFIGURED` |

## Reglas

- Alta: principal = la elegida (por defecto PEN); secundaria = USD, o PEN si la principal es USD; vista en la principal; sin tipo de cambio.
- El par y su tipo de cambio se guardan juntos y nunca sin un valor mayor que cero.
- El tipo de cambio vigente es la fila más reciente del par en cualquier sentido; guardada al revés, se usa su inverso (por eso intercambiar las monedas no lo pierde).
- Sin tipo de cambio, nada puede estar en una moneda distinta de la principal (`RequireUsable`), y no se ve el ahorro en la secundaria.

## Endpoints

`GET /currencies` (público), `GET /settings`, `PUT /settings/fx`, `PATCH /settings`.

## Errores

| `code` | HTTP | Cuándo |
|---|---|---|
| `VALIDATION_FAILED` + `CURRENCY_NOT_SUPPORTED`, `SAME_CURRENCY`, `RATE_REQUIRED`, `INVALID_FORMAT`, `CURRENCY_NOT_IN_PAIR`, `TIMEZONE_INVALID` | 400 | En ese orden por formulario |
| `FX_RATE_NOT_CONFIGURED` | 422 | Ver el ahorro en la secundaria sin tipo de cambio |
