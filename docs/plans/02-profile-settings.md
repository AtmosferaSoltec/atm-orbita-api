# 02 · Perfil, monedas y tipo de cambio

## Objetivo

Guardar las preferencias del usuario: sus dos monedas, en cuál ve el ahorro total, su zona horaria y el tipo de cambio manual entre las dos monedas, que **puede no existir**. Es la base de la regla "sin tipo de cambio no hay otra moneda" y de "hoy" según la zona del usuario.

## Pantallas de Android que cubre

- **Ajustes**: secciones Monedas y Tipo de cambio (`ORBITA_SPEC` §7.12).
- **Registro**: la moneda principal elegida (§7.1).
- El estado "Configura tu tipo de cambio" en Inicio, Cuentas, Ajustes, Nueva cuenta y Nueva/Editar tarjeta.
- Repositorio: `SettingsRepository` (`observeSettings`, `setFx`, `setDisplayCurrency`). Modelo `UserSettings(fx: FxPair, displayCurrency)`, con `FxPair.rate` nulo hasta que se configura.

## Módulo

`profile`. Expone a los demás módulos la interfaz `ProfileReader`:

```go
type ProfileReader interface {
    Get(ctx context.Context, userID uuid.UUID) (Profile, error)   // monedas, display, zona, tasa vigente o nil
    Today(ctx context.Context, userID uuid.UUID) (dates.LocalDate, error)
    RequireUsable(ctx context.Context, userID uuid.UUID, currency string) error // ErrFxNotConfigured
}
```

## Endpoints

| Método y ruta | Request | Respuesta |
|---|---|---|
| `GET /currencies` | — | `200 { data: [{ code, symbol, name }] }` (público; `Cache-Control: public, max-age=86400`) |
| `GET /settings` | — | `200 { data: settings }` |
| `PUT /settings/fx` | `{ "main_currency", "secondary_currency", "rate" }` | `200 { data: settings }` |
| `PATCH /settings` | `{ "display_currency"?, "timezone"? }` | `200 { data: settings }` |

```json
{
  "data": {
    "main_currency": "PEN",
    "secondary_currency": "USD",
    "display_currency": "PEN",
    "timezone": "America/Lima",
    "fx_mode": "manual",
    "fx_configured": true,
    "rate": "3.200000",
    "rate_updated_at": "2026-10-01T15:04:05Z"
  }
}
```

`rate` es unidades de la principal por 1 de la secundaria (1 US$ = S/ 3.20), igual que `FxPair.rate`. Es `null` mientras no se configura.

Catálogo de monedas (el de `core/Formatters.kt`): `PEN` S/ Soles · `USD` US$ Dólares · `EUR` € Euros · `MXN` MX$ Pesos mexicanos · `COP` COL$ Pesos colombianos · `CLP` CLP$ Pesos chilenos · `ARS` AR$ Pesos argentinos · `BOB` Bs Bolivianos · `BRL` R$ Reales.

### Errores

| Situación | HTTP | `code` |
|---|---|---|
| Moneda fuera del catálogo | 400 | `VALIDATION_FAILED` + `CURRENCY_NOT_SUPPORTED` |
| Las dos monedas iguales | 400 | `VALIDATION_FAILED` + `SAME_CURRENCY` |
| `rate` ausente, cero o negativo | 400 | `VALIDATION_FAILED` + `RATE_REQUIRED` ("Escribe un tipo de cambio mayor que cero.") |
| `display_currency` fuera del par | 400 | `VALIDATION_FAILED` + `CURRENCY_NOT_IN_PAIR` |
| Ver el total en la secundaria sin tipo de cambio | 422 | `FX_RATE_NOT_CONFIGURED` ("Configura tu tipo de cambio en Ajustes para usar otra moneda.") |
| Zona horaria inválida | 400 | `VALIDATION_FAILED` + `TIMEZONE_INVALID` |

## Tablas y migraciones

`00002_profile_create_profiles_and_exchange_rates.sql`:

| Tabla | Columnas | Reglas en la base |
|---|---|---|
| `profiles` | `user_id UUID PK FK → users ON DELETE CASCADE`, `main_currency CHAR(3)`, `secondary_currency CHAR(3)`, `display_currency CHAR(3)`, `timezone VARCHAR(64)`, `fx_mode VARCHAR(10) DEFAULT 'manual'`, `created_at`, `updated_at` | `main <> secondary`; `display IN (main, secondary)`; monedas `^[A-Z]{3}$` |
| `exchange_rates` | `id UUID PK`, `user_id UUID NULL FK → users ON DELETE CASCADE` (nulo = global, para el plan 14), `from_currency CHAR(3)`, `to_currency CHAR(3)`, `rate NUMERIC(18,6)`, `source VARCHAR(10)` (`manual`, `api`), `created_at` | `rate > 0`; `from <> to` |

Índice `exchange_rates (user_id, from_currency, to_currency, created_at DESC)`.

**El tipo de cambio ausente es ausencia de fila**, no una columna nula (guía §11). Cada `PUT /settings/fx` agrega una fila nueva y conserva el historial.

## Reglas

- **Alta** (gancho `OnUserRegistered`): perfil con `main_currency` = la elegida (por defecto `PEN`); `secondary_currency` = `USD`, o `PEN` si la principal es `USD`; `display_currency` = la principal; `fx_mode = manual`; `timezone` la del registro o `America/Lima`. **Sin fila de tipo de cambio.**
- **Tipo de cambio vigente** = la fila más reciente del usuario para el par (`from = secondary`, `to = main`). Si existe la del par al revés, se usa su inverso (como `FxPair.raw` en Android): por eso intercambiar las monedas no pierde el valor.
- **`PUT /settings/fx`** guarda el par y su tipo de cambio juntos, y nunca sin un valor mayor que cero (como `DemoSettingsRepository.setFx`). Si la moneda de visualización deja de estar en el par, pasa a la principal.
- **Sin tipo de cambio** (`RequireUsable`): nada puede estar en una moneda distinta de la principal. Lo usan cuentas (04), tarjetas (09) y `PATCH /settings` con `display_currency`. Una vez configurado no se puede quitar; solo cambiar.
- **"Hoy"** = `dates.Today(clock, profile.timezone)`. Todo módulo que valide fechas lo pide aquí.
- **Cambiar la moneda principal** no toca las cuentas ni las tarjetas existentes: conservan la suya.

## Dependencias

01 (usuario y gancho de alta).

## Pasos de implementación

1. Migración y consultas sqlc.
2. Dominio: `Currency` (catálogo), `FxPair` con `Convert`, `RateBetween` y `Inverse` (mismo redondeo que Android: división intermedia a 10 decimales, HALF_UP una sola vez al final), `Profile`.
3. Casos de uso `GetSettings`, `SetFx`, `UpdateSettings`; gancho de alta; implementación de `ProfileReader`.
4. Adapters Postgres y HTTP; `GET /currencies`.
5. Android: `RemoteSettingsRepository` (mapea `rate` nulo a `FxPair.rate = null`), y el registro ya envía `main_currency` y `timezone` (`TimeZone.getDefault().id`).
6. OpenAPI y `ORBITA_SPEC.md` §7.12 y §10.

## Pruebas

Unitarias (vectores de `docs/06` y `MoneyRulesTest.kt`):
- US$ 20.00 → S/ 64.00 con 3.20; S/ 64.00 → US$ 20.00 con el inverso.
- Una moneda fuera del par no tiene conversión.
- Sin tipo de cambio, convertir entre monedas distintas no da resultado.
- Par nuevo con otra moneda: el tipo de cambio queda vacío; intercambiar el par pasa al inverso.

Integración:
- Registro sin moneda → perfil PEN/USD sin tipo de cambio; con `MXN` → MXN/USD; con `USD` → USD/PEN.
- `GET /settings` de un usuario nuevo → `fx_configured: false`, `rate: null`.
- `PUT /settings/fx` con `rate` vacío o `"0"` → `400 RATE_REQUIRED` y no se guarda nada.
- Dos `PUT` seguidos → dos filas; `GET` devuelve la última.
- `PATCH /settings` con la secundaria antes de configurar el tipo de cambio → `422 FX_RATE_NOT_CONFIGURED`.
- Aislamiento: el tipo de cambio de A no aparece en B.

## Criterios de aceptación

- Un usuario nuevo ve "Configura tu tipo de cambio" en Ajustes; tras escribir 3.20, Inicio puede mostrar el total en dólares.
- Cambiar de par sin escribir un valor no guarda nada.

## Variables nuevas para el servidor

Ninguna.

## Desviaciones

(Se completan al implementar.)
