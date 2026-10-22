# 04 · Cuentas

## Objetivo

Que el usuario tenga sus cuentas (efectivo, débito, ahorro, otra), cada una con una sola moneda y su saldo **calculado**, las cree, las edite, decida cuáles cuentan en el ahorro total y las archive.

## Pantallas de Android que cubre

- **Cuentas** y **Nueva/Editar cuenta** (`ui/feature/accounts`; `ORBITA_SPEC` §7.3 y §7.4).
- Los desplegables de cuenta de Nuevo movimiento, Transferir y Marcar como pagada (muestran el saldo).
- Repositorio: `AccountsRepository` (`observeAccounts`, `create`, `update`, `setIncludeInSavings`, `archive`).

## Módulo

`ledger`. Expone `AccountReader` (buscar una cuenta activa del usuario con su moneda y saldo) para `credit` y `home`.

## Endpoints

| Método y ruta | Request | Respuesta |
|---|---|---|
| `GET /accounts` | — | `200 { data: { accounts: [account], savings } }` |
| `POST /accounts` | `{ "id"?, "name", "type", "currency", "initial_balance"?, "include_in_savings"? }` | `201 { data: account }` |
| `PATCH /accounts/{id}` | `{ "name"?, "type"?, "include_in_savings"? }` | `200 { data: account }` |
| `POST /accounts/{id}/archive` | — | `204` |

```json
{
  "data": {
    "accounts": [
      { "id": "…", "name": "Cuenta Dólares", "type": "savings", "currency": "USD",
        "balance": "250.00", "include_in_savings": true }
    ],
    "savings": {
      "included_count": 4, "active_count": 5,
      "totals": [ { "currency": "PEN", "amount": "7166.30" }, { "currency": "USD", "amount": "2239.47" } ]
    }
  }
}
```

`type`: `cash`, `debit`, `savings`, `other`. `savings.totals` trae el ahorro en las dos monedas del par cuando hay tipo de cambio, y solo en la principal cuando no. Android puede seguir calculándolo con `FxPair.savingsTotal` (da lo mismo); el API lo entrega para Inicio y para iOS.

### Errores

| Situación | HTTP | `code` |
|---|---|---|
| Nombre vacío o de más de 60 | 400 | `VALIDATION_FAILED` + `NAME_REQUIRED` / `NAME_TOO_LONG` |
| Tipo o moneda inválidos | 400 | `VALIDATION_FAILED` + `TYPE_INVALID` / `CURRENCY_NOT_SUPPORTED` |
| Saldo inicial negativo o mal formado | 400 | `VALIDATION_FAILED` + `AMOUNT_INVALID` |
| Moneda distinta de la principal sin tipo de cambio | 422 | `FX_RATE_NOT_CONFIGURED` |
| Cuenta de otro usuario o inexistente | 404 | `NOT_FOUND` |
| Mismo `id` con otro contenido | 409 | `ID_CONFLICT` |

## Tablas y migraciones

`00004_ledger_create_accounts.sql`:

| Columna | Tipo |
|---|---|
| `id` | `UUID PK` |
| `user_id` | `UUID NOT NULL FK → users ON DELETE CASCADE` |
| `name` | `VARCHAR(60) NOT NULL` |
| `type` | `VARCHAR(10) NOT NULL` |
| `currency` | `CHAR(3) NOT NULL` |
| `initial_balance` | `NUMERIC(14,2) NOT NULL DEFAULT 0` |
| `include_in_savings` | `BOOLEAN NOT NULL DEFAULT true` |
| `sort_order` | `INT NOT NULL DEFAULT 0` |
| `archived_at` | `TIMESTAMPTZ NULL` |
| `created_at`, `updated_at` | `TIMESTAMPTZ` |

`UNIQUE (id, user_id)` para las claves foráneas compuestas (planes 05, 06 y 10). **Sin `CHECK` de signo en `initial_balance`:** que sea ≥ 0 lo valida el API al crear; los datos de ejemplo de Android necesitan uno negativo ("Débito principal" nace en −4,065.80 para terminar en 1,245.80).

## Reglas

- **Alta** (gancho `OnUserRegistered`): cuenta "Efectivo", tipo `cash`, en la moneda principal, saldo inicial 0, en ahorros.
- **Saldo** = saldo inicial + ingresos − egresos + transferencias que entran − transferencias que salen, sin lo borrado. **No se guarda**: se calcula en SQL, en una sola consulta agrupada para todas las cuentas (sin una consulta por cuenta). En este plan solo existe el saldo inicial; los planes 05 y 06 agregan sus sumas a la misma consulta.
- **Ahorro total** (en moneda `D`): suma de los saldos de las cuentas no archivadas con `include_in_savings`, convertidos a `D` con el tipo de cambio vigente (`ProfileReader`), redondeando una sola vez al final (HALF_UP). Una cuenta en una moneda fuera del par no suma (como `FxPair.savingsTotal`).
- **La moneda y el saldo inicial no cambian después de crear la cuenta** (`AccountsRepository.update` los ignora). `PATCH` no los acepta.
- **Sin tipo de cambio**, solo se crean cuentas en la moneda principal (`RequireUsable`).
- **Archivar** pone `archived_at`: la cuenta deja de listarse y no se usa en movimientos nuevos, pero sus movimientos y transferencias siguen y se muestran con ella.
- Orden: `sort_order`, luego `created_at`. La primera de la lista es la cuenta por defecto en Android.

## Dependencias

02 (moneda principal y tipo de cambio) y 03 (el alta corre en orden: perfil, categorías, cuenta).

## Pasos de implementación

1. Migración y consultas sqlc (incluida la de saldos agrupados).
2. Dominio: `Account`, `AccountType`, `Savings` (cálculo con el `FxPair` del plan 02).
3. Casos de uso `List`, `Create`, `Update`, `Archive`; gancho de alta; `AccountReader`.
4. Adapters Postgres y HTTP.
5. Android: `RemoteAccountsRepository`; `setIncludeInSavings` es un `PATCH` con solo ese campo.
6. OpenAPI y `ORBITA_SPEC.md`.

## Pruebas

Unitarias: ahorro total de los datos de ejemplo = S/ 7,166.30 y US$ 2,239.47; "Billetera digital" (fuera de ahorros) no suma; sin tipo de cambio solo suma la moneda principal.

Integración:
- Un usuario nuevo tiene "Efectivo" en su moneda principal con saldo 0.
- Crear una cuenta en USD sin tipo de cambio → `422`; tras `PUT /settings/fx` → `201`.
- `PATCH` con `currency` o `initial_balance` → `400` (campo no permitido).
- Archivar la saca de `GET /accounts` y del ahorro total.
- A no ve ni modifica las cuentas de B.

## Criterios de aceptación

- Cuentas muestra los saldos y el total que entrega el API; el interruptor de ahorros persiste.
- Un usuario nuevo ve "Configura tu tipo de cambio" y, tras escribirlo, puede crear una cuenta en dólares.

## Variables nuevas para el servidor

Ninguna.

## Estado (22 oct 2026)

Hecho: implementado en API y Android; desviaciones aprobadas el 22 oct 2026.

| Comprobación | Resultado |
|---|---|
| `gofmt`, `go vet`, `golangci-lint run`, `go build` | Sin problemas (`0 issues`) |
| `go test ./...` sin base / contra el Postgres local | Pasa. Dominio: ahorro de los datos de ejemplo = S/ 7,166.30 y US$ 2,239.47; sin tipo de cambio, S/ 6,366.30; "Billetera digital" y una cuenta en EUR no suman; validaciones. Servicio: 6 pruebas. Integración por HTTP (6): "Efectivo" en MXN con 0.00 al registrarse; USD sin tipo de cambio → 422 y tras `PUT /settings/fx` → 201; total S/ 5,600.00 = US$ 1,750.00; validaciones en orden; `PATCH` con `currency` o `initial_balance` → 400; archivar la saca de la lista y del total; reintento con el mismo `id`; aislamiento entre usuarios → 404 |
| Migración `00004`: `down` y `up` | Funciona; un usuario anterior recibe "Efectivo" en su moneda principal |
| OpenAPI | Válido (Redocly) |
| Android: pruebas, `lintDebug`, `installDebug` | 67 pruebas pasan (6 de `RemoteAccountsRepository`, 1 de `DemoAccountMirror`); ningún aviso de lint en los archivos nuevos |

**En el teléfono** (API real + Postgres local, con una cuenta creada antes de esta migración):

| Prueba | Resultado |
|---|---|
| Abrir Inicio y Cuentas | "Efectivo · soles, S/ 0.00", "1 de 1 cuentas" (la dio la migración) |
| Nueva cuenta sin tipo de cambio | Solo deja soles ("Configura tu tipo de cambio en Ajustes para usar otras monedas.") |
| Crear "Ahorros" con 4800 | S/ 4,800.00; total 2 de 2 |
| Tipo de cambio 3.20 y crear "Cuenta Dolares" con US$ 250 | "≈ S/ 800.00"; total S/ 5,600.00 o US$ 1,750.00 |
| Apagar "Contar en el total" en Ahorros | Total S/ 800.00, 2 de 3; queda guardado en la base |
| Cerrar y abrir la app | Todo vuelve del API igual |
| Gasto (demo) de S/ 20 desde "Efectivo" | Se guarda; Efectivo muestra S/ -20.00 y el total baja |
| Archivar "Cuenta Dolares" | Sale de la lista y del total; en la base queda archivada |

## Desviaciones

- **La migración da "Efectivo" a los usuarios que ya existían**, en su moneda principal (la del perfil), como hizo la `00003` con las categorías.
- **`FIELD_NOT_EDITABLE`** es el código para `currency` o `initial_balance` en un `PATCH` (el plan pedía un 400 sin nombrar el código).
- **El ahorro total se calcula en Go, no en SQL**: SQL entrega el saldo de cada cuenta en una sola consulta y Go suma con la misma regla que `FxPair.savingsTotal` (convierte sin redondear y redondea una vez al final). `ledger` tiene su propia copia del par (`domain.Fx`) que llena desde `profile.Reader`, para que su dominio no dependa de otro módulo.
- **`ledger.New` recibe `profile.Reader`** y `OnUserRegistered` recibe la moneda principal (crea las categorías y "Efectivo").
- **`AccountReader` se deja para el plan 08 o 09** (Inicio y `credit` son quienes lo usan), como `CategoryReader`.
- **Android no lee `savings`** de la respuesta: sigue calculando el total con `FxPair.savingsTotal` (da lo mismo; el plan lo permitía).
- **Android: `DemoAccountMirror`** (temporal, hasta el plan 05), como el de categorías. Copia las cuentas del API al almacén demo con el saldo del API como inicial; los movimientos de ejemplo de la sesión mueven el saldo **en pantalla**, no en el API, y se pierden al cerrar la app.
- **Texto de Android corregido** (aprobado el 22 oct 2026): al editar una cuenta decía "La moneda no se puede cambiar porque la cuenta ya tiene movimientos.", aunque no tuviera ninguno. Ahora dice "La moneda de una cuenta no se puede cambiar.", que es la regla.
