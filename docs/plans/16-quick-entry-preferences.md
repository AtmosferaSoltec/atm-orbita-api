# 16 · Registro rápido y funciones opcionales

## Objetivo

Hoy registrar un gasto obliga a elegir de qué cuenta sale, y la app muestra siempre transferencias y tarjetas de crédito. Hay personas que quieren **registrar rápido** (monto, categoría y, si quieren, una descripción) y que **no usan** tarjetas, transferencias ni varias cuentas.

Este plan agrega un panel de **preferencias** en Ajustes con interruptores simples (activar/desactivar) para que cada persona vea solo lo que usa. **No se pierde ni se borra ningún dato**: apagar una función solo la oculta; volver a encenderla la devuelve tal cual.

Es una preferencia de **presentación**. El API sigue aceptando todas las operaciones sin importar las preferencias; lo único que valida es la cuenta predeterminada.

## Decisiones tomadas (21–24 oct 2026)

| # | Tema | Decisión |
|---|---|---|
| Q1 | Dónde se guardan | En el servidor, junto al perfil (`GET/PATCH /settings`): así siguen a la persona entre dispositivos y las usará también la futura web |
| Q2 | Valores iniciales | **Todo encendido**, como funciona hoy: nadie ve un cambio hasta que lo decide. Lo que se apaga es explícito |
| Q3 | Atajo | Un interruptor maestro **"Registro rápido"** que apaga de una vez todo lo opcional, para quien no quiere revisar uno por uno |
| Q4 | Categoría | Sigue siendo obligatoria (el registro siempre trae categorías). No cambia |
| Q5 | Monedas / tipo de cambio | Fuera de alcance. Sigue como está |

## Qué se puede activar o desactivar

Cuatro preferencias, todas en la sección nueva **"Cómo registras"** de Ajustes:

| Preferencia | Encendida (hoy) | Apagada |
|---|---|---|
| **Elegir la cuenta en cada movimiento** | El formulario pide "Desde la cuenta" / "A la cuenta" | No aparece el selector: el movimiento va a la **cuenta predeterminada**. Aparece en Ajustes "Cuenta predeterminada" para elegirla |
| **Descripción** | Campo siempre visible | El campo queda plegado tras un enlace "Agregar descripción" (sigue siendo opcional) |
| **Transferencias** | Pestaña "Transferencia" en el formulario y acceso desde Cuentas | Se ocultan la pestaña y los accesos |
| **Tarjetas de crédito** | Pestaña "Crédito" en la barra inferior, interruptor "Compra con tarjeta" en el formulario y recordatorios de Inicio | Se oculta la pestaña (la barra queda con Inicio · Cuentas · [+] · Reportes), el interruptor del formulario y el bloque de Inicio |

**Registro rápido (maestro):** al encenderlo se apagan las cuatro (cuenta, descripción visible, transferencias, tarjetas); al apagarlo se encienden las cuatro. Si la persona cambia una a mano después, el maestro refleja el estado real (encendido solo si las cuatro están apagadas).

### Con el formulario en modo rápido
Un gasto se registra con: teclado de monto → categoría → "Guardar". La descripción es un enlace opcional. La fecha sigue siendo el momento en que se guarda.

## Reglas

1. **Cuenta predeterminada.** Es una cuenta propia y activa (no archivada). Si no se eligió ninguna, es la primera cuenta activa (normalmente "Efectivo"). Si la elegida se archiva o se elimina, la app vuelve a la primera activa sin mostrar error.
2. **Con el selector de cuenta apagado**, el movimiento usa siempre la cuenta predeterminada, **no** "la de la última vez". Es predecible.
3. **Editar** un movimiento existente muestra su cuenta aunque el selector esté apagado (no se esconde dónde está registrado).
4. **Apagar tarjetas con compras pendientes** está permitido, pero la app avisa: "Tienes N compras pendientes. Seguirán ahí y podrás verlas al activar de nuevo las tarjetas." Los reportes no cambian (las compras pendientes no cuentan como movimientos, regla vigente).
5. **Apagar transferencias** no borra las transferencias hechas: siguen apareciendo en la lista de movimientos y cuentan en los saldos. Solo deja de poder crear nuevas desde la app.
6. Ninguna preferencia afecta a lo que el API calcula (saldos, reportes, Inicio).

## Endpoints

Se extienden los de ajustes; no hay rutas nuevas.

**`GET /api/v1/settings`** — la respuesta agrega `preferences`:

```json
{
  "data": {
    "main_currency": "PEN",
    "…": "…",
    "preferences": {
      "pick_account": true,
      "show_description": true,
      "transfers_enabled": true,
      "credit_cards_enabled": true,
      "default_account_id": null
    }
  }
}
```

**`PATCH /api/v1/settings`** — acepta `preferences` parcial (solo los campos enviados cambian):

```json
{ "preferences": { "pick_account": false, "default_account_id": "<uuid>" } }
```

- `default_account_id` acepta `null` para volver a "la primera activa".
- Los demás valores son booleanos; otro tipo → `400 VALIDATION_FAILED`.
- La respuesta es el mismo objeto de `GET /settings`.

### Errores
| Situación | HTTP | `code` |
|---|---|---|
| `default_account_id` no existe, es de otra persona o está archivada | 404 | `ACCOUNT_NOT_FOUND` ("Esa cuenta ya no existe.") |
| Un valor de `preferences` no es booleano | 400 | `VALIDATION_FAILED` |

Revisar también **`GET /home`**: si ya incluye `settings`, debe incluir `preferences` (Android lee una sola vez al abrir Inicio). Documentar todo en `docs/openapi.yaml`.

## Tablas y migraciones

Una migración nueva, **no se edita ninguna aplicada**: `db/migrations/00011_profile_add_preferences.sql`.

```sql
-- +goose Up
ALTER TABLE profiles
    ADD COLUMN pick_account         BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN show_description     BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN transfers_enabled    BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN credit_cards_enabled BOOLEAN NOT NULL DEFAULT true,
    -- Sin clave foránea: la cuenta es del módulo ledger. El módulo profile la
    -- valida por una interfaz de ledger y la app tolera que ya no exista.
    ADD COLUMN default_account_id   UUID;

-- +goose Down
ALTER TABLE profiles
    DROP COLUMN default_account_id,
    DROP COLUMN credit_cards_enabled,
    DROP COLUMN transfers_enabled,
    DROP COLUMN show_description,
    DROP COLUMN pick_account;
```

Es segura con la versión anterior corriendo (columnas con valor por defecto). Los perfiles existentes quedan todo encendido (Q2).

## Módulo(s)

- **`profile`** (API): dominio con las preferencias, caso de uso en el servicio de ajustes, adapter Postgres (consultas sqlc) y handler. Mantener las reglas de dependencia: `profile` **no** consulta la tabla de cuentas; pide a `ledger` una interfaz pequeña (por ejemplo `AccountChecker.IsActiveOwnedBy(ctx, userID, accountID)`) que se conecta en `main.go`/`module.go`, igual que ya se hace con otros ganchos entre módulos.
- **Android**: ver siguiente sección.

## Android

Pantallas y archivos que toca (nombres del código actual):

- **Dominio:** `UserSettings` (`domain/model/Models.kt`) agrega `preferences: Preferences` (cuatro booleanos y `defaultAccountId: String?`). Datos de ejemplo y modo demo (`data/demo`) con los mismos valores iniciales.
- **Remoto:** `SettingsDtos.kt` y `RemoteSettingsRepository` leen y envían `preferences`; `SettingsRepository` gana `setPreferences(...)`.
- **Ajustes** (`SettingsScreen.kt`, `SettingsViewModel.kt`): sección nueva **"Cómo registras"** con el interruptor maestro "Registro rápido", los cuatro interruptores con una línea de ayuda cada uno, y el selector "Cuenta predeterminada" visible solo cuando "Elegir la cuenta" está apagado. Textos en `strings.xml` (es-PE).
- **Formulario de movimiento** (`MovementFormScreen.kt`): oculta el selector de cuenta, plegado de descripción, pestaña "Transferencia" e interruptor "Compra con tarjeta" según las preferencias. Hoy la cuenta inicial es "la del último movimiento o la primera" (`MovementsUiState.defaultAccount`): con el selector apagado pasa a ser la **cuenta predeterminada** (regla 1 y 2). Al editar, se mantiene visible la cuenta del movimiento (regla 3).
- **Navegación** (`OrbitaApp.kt`, `OrbitaBottomBar` en `AppComponents.kt`): `BottomTab.CREDIT` solo si las tarjetas están activas; accesos a transferencias solo si están activas. Si la pestaña activa se oculta, volver a Inicio.
- **Inicio** (`HomeScreen.kt`): ocultar el bloque de crédito con tarjetas apagadas.
- **Sesión nueva o cambio de dispositivo:** las preferencias llegan con los ajustes; no hay estado local aparte.

## Dependencias

02 (ajustes), 04 (cuentas), 09 (tarjetas), 13 (integración con Android).

## Pasos de implementación

1. API: migración `00011`, dominio, puerto hacia `ledger`, servicio, adapter, handler, OpenAPI. Pruebas.
2. Despliegue: avisar al usuario antes de promover; la migración se aplica sola en producción.
3. Android: dominio y datos (demo y remoto), luego Ajustes, luego formulario, navegación e Inicio.
4. Actualizar `docs/ORBITA_SPEC.md` de Android (pantalla Ajustes, formulario y reglas nuevas) en el mismo cambio.

## Pruebas

- **API:**
  - `GET /settings` de un perfil existente devuelve todo en `true` y `default_account_id: null`.
  - `PATCH` parcial cambia solo lo enviado.
  - `default_account_id` de otra persona, archivada o inexistente → `404 ACCOUNT_NOT_FOUND`; con `null` vuelve a la primera activa.
  - Un valor no booleano → `400`.
  - Una persona no puede leer ni cambiar las preferencias de otra.
  - La migración sube y baja (`Up` y `Down`) sobre una base con datos.
  - Test de arquitectura: `profile` no importa paquetes internos de `ledger`.
- **Android:**
  - Unitarias: maestro enciende/apaga las cuatro y refleja el estado real; cuenta predeterminada con fallback (archivada, ausente, lista vacía); la barra inferior sin la pestaña Crédito.
  - Formulario: con el selector apagado un gasto se guarda en la cuenta predeterminada; editar muestra la cuenta; compra con tarjeta oculta; descripción plegada pero se guarda si se escribe.
  - Contra el API real (servidor): apagar y encender cada preferencia, cerrar sesión y volver a entrar (deben persistir), y probar desde un segundo dispositivo o instalación.

## Criterios de aceptación

- Con "Registro rápido" encendido, registrar un gasto es: monto → categoría → guardar, sin elegir cuenta.
- Apagar tarjetas oculta su pestaña, el interruptor del formulario y el bloque de Inicio; encenderlas devuelve todo, con los datos intactos.
- Apagar transferencias oculta sus accesos; las transferencias hechas siguen en la lista.
- Las preferencias persisten tras cerrar sesión y en otro dispositivo.
- Ningún usuario existente ve cambios hasta que apague algo.
- Pruebas del API y de Android pasan; lint y `golangci-lint` sin errores.

## Variables nuevas para el servidor

Ninguna.

## Fuera de alcance (posibles siguientes pasos)

- Preguntar "¿cómo quieres registrar?" al crear la cuenta, en vez de solo en Ajustes.
- Recordar la última categoría usada como sugerida.
- Ocultar la moneda secundaria y el tipo de cambio para quien solo usa una moneda.
