# 05 · Fase 2 — Cuentas, categorías y ajustes

**Objetivo:** que el usuario pueda gestionar sus cuentas y categorías y fijar sus monedas y su tipo de cambio manual. Es todo lo que un movimiento necesita para existir.

**Tamaño:** mediano. **Depende de:** Fase 1. **Bloquea:** Fases 3 a 7.

Pantallas de Android cubiertas: Cuentas, Nueva/Editar cuenta, Categorías, Nueva/Editar categoría, Ajustes (secciones 7.3, 7.4, 7.11 y 7.12 de `ORBITA_SPEC.md`).

## 1. Catálogo de monedas

`GET /currencies` — público, cacheable 24 h.

```json
{ "data": [
  { "code": "PEN", "symbol": "S/",   "name": "Soles" },
  { "code": "USD", "symbol": "US$",  "name": "Dólares" },
  { "code": "EUR", "symbol": "€",    "name": "Euros" },
  { "code": "MXN", "symbol": "MX$",  "name": "Pesos mexicanos" },
  { "code": "COP", "symbol": "COL$", "name": "Pesos colombianos" },
  { "code": "CLP", "symbol": "CLP$", "name": "Pesos chilenos" },
  { "code": "ARS", "symbol": "AR$",  "name": "Pesos argentinos" },
  { "code": "BOB", "symbol": "Bs",   "name": "Bolivianos" },
  { "code": "BRL", "symbol": "R$",   "name": "Reales" }
] }
```

Es una constante en el código (igual a `supportedCurrencies` de Android), no una tabla. Toda moneda que entra al API se valida contra este catálogo; una que no esté devuelve `CURRENCY_NOT_SUPPORTED`. Agregar una moneda es un cambio de código con su prueba.

## 2. Ajustes

### `GET /settings`

```json
{
  "mainCurrency": "PEN",
  "secondaryCurrency": "USD",
  "displayCurrency": "PEN",
  "fxMode": "manual",
  "timezone": "America/Lima",
  "fxConfigured": true,
  "fx": { "from": "USD", "to": "PEN", "rate": "3.200000", "source": "manual", "rateDate": "2026-10-02" }
}
```

`fx.rate` es **unidades de la moneda principal por 1 unidad de la secundaria** (1 US$ = S/ 3.20), la misma convención de `FxPair`. Es `null`, con `fxConfigured: false`, si el usuario aún no tiene tipo de cambio para ese par (sección 3).

### `PUT /settings/fx`

Equivale a `SettingsRepository.setFx(FxPair)`. Cuerpo: `mainCurrency`, `secondaryCurrency`, `rate`.

En una transacción:

1. Valida que las dos monedas estén en el catálogo y sean distintas, y que `rate` **esté presente y sea mayor que cero**. Sin `rate`, con `null`, con `0` o con un negativo responde `422 RATE_REQUIRED` y no guarda nada: **no existe forma de guardar un par de monedas sin su tipo de cambio.**
2. Actualiza el perfil.
3. Si `displayCurrency` ya no es una de las dos, pasa a ser la principal (regla de `DemoSettingsRepository.setFx`).
4. Inserta una fila en `exchange_rates` (`from = secundaria`, `to = principal`, `source = manual`, `rate_date = hoy del usuario`). **No modifica la fila anterior**: queda el historial.

**Cambio de par (decisión del usuario, 7 oct 2026).** Al elegir en Ajustes una moneda que no estaba en el par, el campo del tipo de cambio queda **vacío**; antes Android lo ponía en `1.00`, que era un valor inventado. La app no envía nada hasta que el usuario escribe un valor mayor que cero, y entonces manda el par y el cambio juntos en esta misma petición. Si el usuario sale sin escribirlo, el par anterior sigue vigente. La regla se valida en los dos lados: la app no deja guardar, y el servidor rechaza con `RATE_REQUIRED` aunque un cliente lo intente.

Intercambiar las dos monedas del par ("elegir la otra") no vacía el campo: el cambio pasa a su inverso, que sí se conoce. Esa lógica vive en el cliente (`FxPair.withMain` / `withSecondary`), que envía el par resultante; el servidor solo valida y guarda.

### `PATCH /settings`

Campos opcionales: `displayCurrency` (debe ser una de las dos del par), `timezone` (identificador IANA válido), `fxMode` (`auto` solo cuando exista la Fase 6).

### Tipo de cambio vigente

`ExchangeRatesService.effectiveRate(userId, from, to)` es el único lugar que decide qué cambio aplica. Lo usan ajustes, cuentas, inicio y transferencias.

1. `from == to` → `1`.
2. Modo manual → la fila manual más reciente del usuario para el par (`rate_date DESC, created_at DESC`). Si solo existe el par inverso, `1 ÷ rate`.
3. Modo automático (Fase 6) → la fila global más reciente, directa o cruzada.
4. Sin datos → `null`. Quien llama decide: una cuenta en esa moneda no entra en el ahorro total ni muestra equivalente, igual que hoy en Android.

`GET /exchange-rates/latest?from=USD&to=PEN` expone ese resultado, para el cambio sugerido en Transferir.

## 3. Usuario sin tipo de cambio configurado

Decisión del usuario (7 oct 2026): el tipo de cambio **puede no existir**, la interfaz lo dice con el estado "Configura tu tipo de cambio" y **no se permiten operaciones en otra moneda hasta que exista**.

### Cuándo ocurre

Al registrarse. El alta crea el perfil con su par de monedas (por defecto PEN/USD) pero ninguna fila en `exchange_rates`: el servidor no conoce el cambio y no inventa uno. (Cuando exista la Fase 6, el alta sembrará el valor global del día y este estado casi no se verá.)

### Cómo se representa

| Capa | Representación |
|---|---|
| Base de datos | **No hay fila** en `exchange_rates` para el par del usuario. No existe ninguna columna de tipo de cambio que admita nulo en el perfil (confirmado por el usuario) |
| API | `GET /settings` y `GET /home` devuelven `"fxConfigured": false` y `"fx": { "from": "USD", "to": "PEN", "rate": null }` |
| Android | `FxPair.rate` pasa de `BigDecimal` a `BigDecimal?`; `convert`, `rateBetween` y `savingsTotal` devuelven "sin resultado" en lugar de fallar |

`fxConfigured` es verdadero cuando existe tipo de cambio entre la moneda principal y la secundaria del usuario.

### Qué se bloquea

**Aprobado por el usuario: se bloquea desde el origen.** No se crean cuentas ni tarjetas en una moneda distinta de la base (la moneda principal) sin tipo de cambio, y el mensaje lleva a "Configura tu tipo de cambio".

Mientras `fxConfigured` sea falso, el servidor rechaza con `422 FX_RATE_NOT_CONFIGURED` toda escritura que **introduzca una moneda distinta de la principal**:

| Operación | Sin tipo de cambio |
|---|---|
| Crear una cuenta o una tarjeta en la moneda principal | Permitido |
| Crear una cuenta en otra moneda | **Bloqueado** |
| Crear una tarjeta en otra moneda, o cambiar la moneda de una tarjeta a otra | **Bloqueado** |
| Movimientos, compras y pagos | Permitidos: solo pueden ser en la moneda principal, porque no existen cuentas ni tarjetas en otra |
| Transferir entre cuentas de monedas distintas | **Bloqueado** (no puede darse, pero el servidor lo comprueba igual) |
| Ver el ahorro total en la moneda secundaria (`displayCurrency`) | **Bloqueado** |
| Fijar el tipo de cambio (`PUT /settings/fx`) | Permitido: es la salida del estado |

El criterio se resume en una regla fácil de probar: **sin tipo de cambio, todos los datos del usuario están en su moneda principal.** Así ninguna pantalla necesita convertir nada y ningún total queda a medias.

Se propone bloquear en el origen (crear la cuenta) y no solo al transferir porque, si se permitiera tener una cuenta en dólares sin tipo de cambio, el ahorro total, el equivalente de esa cuenta y el Inicio tendrían que mostrar datos incompletos.

### Qué muestra la app

| Pantalla | Con tipo de cambio | Sin tipo de cambio |
|---|---|---|
| Inicio y Cuentas, tarjeta de ahorro | Total, selector `S/ \| US$` y la línea "o US$ 2,239.47 · cambio 3.20" | Total en la moneda principal; selector desactivado; en lugar de la línea, el enlace **"Configura tu tipo de cambio ›"**, que lleva a Ajustes |
| Nueva cuenta, Nueva/Editar tarjeta | Desplegable de moneda con las 9 | Desplegable fijo en la moneda principal, con la ayuda "Configura tu tipo de cambio en Ajustes para usar otras monedas." |
| Transferir | Normal | Normal: todas las cuentas están en la misma moneda |
| Ajustes, sección Tipo de cambio | `1 US$ = S/ [3.20]` y su inverso | Campo vacío y resaltado, con el aviso "Configura tu tipo de cambio"; el inverso muestra "—" |

Si el servidor devuelve `FX_RATE_NOT_CONFIGURED` (un cliente desactualizado, por ejemplo), la app muestra "Configura tu tipo de cambio en Ajustes para usar otra moneda.", con una acción **"Configurar"** que abre Ajustes en la sección Tipo de cambio. El mensaje nunca es un callejón sin salida: siempre lleva al lugar donde se resuelve.

### Cómo se sale

`PUT /settings/fx` con un `rate > 0`. Inserta la primera fila de `exchange_rates` y `fxConfigured` pasa a verdadero.

**Es un estado de una sola dirección.** Una vez configurado no se puede volver atrás: las filas de `exchange_rates` no se borran, y cambiar el par de monedas exige enviar un `rate`. Por eso el servidor nunca tiene que resolver el caso "hay cuentas en dólares y el tipo de cambio desapareció".

Cambiar de par tampoco puede devolver al usuario a este estado: el par nuevo solo se guarda junto con su tipo de cambio (sección 2).

### Terceras monedas

Con el tipo de cambio configurado, una cuenta en una moneda fuera del par (por ejemplo EUR con par PEN/USD) sigue permitida, como hoy: no entra en el ahorro total ni muestra equivalente. Eso no cambia.

## 4. Cuentas

| Método y ruta | Descripción |
|---|---|
| `GET /accounts` | Cuentas no archivadas con su saldo calculado. `?includeArchived=true` las incluye |
| `GET /accounts/{id}` | Una cuenta |
| `POST /accounts` | Crea |
| `PATCH /accounts/{id}` | Cambia `name`, `type`, `includeInSavings` |
| `POST /accounts/{id}/archive` | Archiva |

Respuesta de `GET /accounts`:

```json
{
  "data": [
    { "id": "…", "name": "Cuenta Dólares", "type": "savings", "currency": "USD",
      "balance": "250.00", "includeInSavings": true, "isArchived": false,
      "equivalent": { "currency": "PEN", "amount": "800.00" } }
  ],
  "meta": {
    "savings": {
      "includedCount": 4, "totalCount": 5,
      "main":      { "currency": "PEN", "total": "7166.30" },
      "secondary": { "currency": "USD", "total": "2239.47" },
      "rate": "3.200000"
    }
  }
}
```

Reglas:

- **Saldo** calculado con la consulta de [02](02-modelo-de-datos.md), sección 6. Nunca se guarda.
- **Ahorro total** = suma de los saldos de las cuentas no archivadas con `includeInSavings`, convertidos a cada moneda del par y redondeados **una sola vez al final**. Las cuentas en una moneda fuera del par no suman. Es la regla de `FxPair.savingsTotal`.
- `equivalent` aparece solo si la cuenta no está en la moneda principal y hay cambio para convertirla.
- Orden: `sort_order`, luego `created_at`.
- Al crear: `name` (1–60 tras recortar espacios), `type` (por defecto `debit`), `currency` (por defecto la principal del usuario), `initialBalance` (opcional, `>= 0`), `includeInSavings` (por defecto `true`), `id` opcional.
- Crear una cuenta en una moneda distinta de la principal exige tener tipo de cambio configurado; si no, `422 FX_RATE_NOT_CONFIGURED` (sección 3).
- Sin tipo de cambio, `meta.savings.secondary` es `null` y ninguna cuenta lleva `equivalent`.
- Al editar: **`currency` e `initialBalance` no se aceptan**. Enviarlos devuelve `400` (campo desconocido), no se ignoran en silencio.
- Archivar no borra nada: los movimientos antiguos siguen apuntando a la cuenta y siguen contando en los reportes. Una cuenta archivada no admite movimientos ni transferencias nuevos.
- Archivar dos veces devuelve `204` las dos.

El cliente puede seguir calculando el total localmente para que el interruptor "Contar en el total de ahorros" responda al instante; el valor del servidor es el de referencia y ambos deben coincidir (mismos vectores de prueba).

## 5. Categorías

| Método y ruta | Descripción |
|---|---|
| `GET /categories` | No archivadas. Filtros `?kind=expense` y `?includeArchived=true` |
| `POST /categories` | Crea |
| `PATCH /categories/{id}` | Cambia `name` y `color` |
| `POST /categories/{id}/archive` | Archiva |

Reglas:

- `name` 1–60; `kind` `income` o `expense`; `color` con formato `#RRGGBB`.
- **`kind` no cambia** después de crear. Enviarlo en `PATCH` devuelve `400`.
- Una categoría archivada deja de ofrecerse, pero los movimientos y reportes anteriores la conservan con su nombre y color.
- El API no limita el color a la paleta de 7 muestras de Android: la paleta es una decisión de interfaz.

### Nombre repetido

Decisión del usuario (7 oct 2026): el nombre de una categoría es **único por usuario y por tipo**, y la comparación **no distingue mayúsculas ni tildes**. "Alimentación", "alimentacion" y "ALIMENTACIÓN" son la misma categoría.

**Dos columnas, dos papeles**

| Columna | Contenido | Quién la escribe | Para qué |
|---|---|---|---|
| `name` | El nombre **tal como lo escribió el usuario**, recortado | El usuario | Es lo único que se muestra, siempre |
| `name_normalized` (`nameNormalized` en Prisma) | El nombre en minúsculas y sin tildes | **Solo el backend**, al crear y al renombrar | Es la clave de unicidad. Nunca se muestra, no se acepta en ningún DTO de entrada y no sale en ninguna respuesta |

**Normalización** (`common/text/normalizeName`, una sola función para todo el API):

1. Recortar espacios al inicio y al final.
2. Reducir los espacios repetidos a uno.
3. Pasar a minúsculas.
4. Quitar las tildes y la diéresis: `á é í ó ú ü` → `a e i o u u`.
5. **La `ñ` se conserva** (confirmado por el usuario): en español es una letra distinta, no una `n` con tilde. "Año" y "Ano" no deben chocar.

| Nombre escrito | `name` guardado | `name_normalized` |
|---|---|---|
| `Alimentación` | `Alimentación` | `alimentacion` |
| `alimentacion` | `alimentacion` | `alimentacion` |
| `ALIMENTACIÓN` | `ALIMENTACIÓN` | `alimentacion` |
| `  Otros   gastos ` | `Otros gastos` | `otros gastos` |
| `Pingüinos` | `Pingüinos` | `pinguinos` |
| `Año nuevo` | `Año nuevo` | `año nuevo` |

Implementación: descomponer en Unicode (NFD), eliminar las marcas combinantes salvo la de la `ñ`, recomponer (NFC). Se hace en el backend y no en la base para no depender de extensiones de PostgreSQL y para que la regla tenga una sola implementación con sus pruebas.

**Restricción única, declarada en Prisma**

```prisma
model Category {
  // …
  name           String    @db.VarChar(60)
  nameNormalized String?   @map("name_normalized") @db.VarChar(60)
  kind           EntryKind

  @@unique([userId, kind, nameNormalized])
}
```

Con la columna normalizada ya no hace falta un índice escrito a mano en SQL: la regla queda en el esquema de Prisma, que es la única fuente del esquema.

Incluye `kind` porque el usuario eligió que la unicidad sea por tipo: se puede tener "Otros" en egresos y "Otros" en ingresos, como dice `ORBITA_SPEC.md` 7.11. (El texto con las decisiones decía `@@unique([userId, nameNormalized])`; al confirmarlas, el usuario mantuvo el tipo.)

**Categorías archivadas** (confirmado por el usuario). `nameNormalized` admite nulo y **se pone en nulo al archivar**. PostgreSQL no considera iguales dos valores nulos, así que una categoría archivada deja libre su nombre: se puede crear "Mascotas" otra vez después de archivar la anterior. Sin esto, archivar una categoría inutilizaría su nombre para siempre, porque la app no tiene "desarchivar". La alternativa es que las archivadas sigan ocupando el nombre; basta con no anularlo.

**La regla la impone la base**, no una consulta previa del servicio: comprobar primero y luego insertar deja una ventana en la que dos peticiones simultáneas pasan la comprobación. El servicio calcula `nameNormalized`, inserta, y si la base rechaza por esa restricción (error `P2002` de Prisma) lo traduce.

**Respuesta ante una colisión**

`409 Conflict`, con un código estable y el campo afectado:

```json
{
  "type": "https://orbita.app/problems/category-name-taken",
  "title": "Category name already in use",
  "status": 409,
  "code": "CATEGORY_NAME_TAKEN",
  "requestId": "…",
  "errors": [ { "field": "name", "code": "NAME_TAKEN" } ]
}
```

- Aplica igual al crear y al renombrar.
- Cambiar solo las mayúsculas o las tildes de una categoría ("ocio" → "Ocio", "Alimentacion" → "Alimentación") está permitido: es la misma fila, no choca consigo misma, y `name` pasa a mostrarse como el usuario lo escribió.
- La respuesta no incluye el nombre de la categoría con la que choca; la app ya tiene la lista.

**En Android**, el mensaje se muestra **bajo el campo Nombre**, en rojo, y no en el aviso flotante que usan las demás validaciones: "Ya tienes una categoría de este tipo con ese nombre." El campo queda marcado en rojo hasta que el usuario cambia el texto. La app siempre muestra `name`; nunca recibe `nameNormalized`. Si quiere avisar antes de enviar, debe aplicar la misma normalización: los casos de la tabla de arriba van en los vectores compartidos ([12](12-pruebas.md) §3).

## 6. Tareas

- [ ] Revisar que la migración `init_finance` (creada en la Fase 1) tenga todos los `CHECK` e índices de [02](02-modelo-de-datos.md)
- [ ] Módulo `currencies` y validador `@IsSupportedCurrency()`
- [ ] Módulo `exchange-rates`: repositorio, `effectiveRate`, `GET /exchange-rates/latest`
- [ ] Módulo `settings`: `GET`, `PUT /settings/fx`, `PATCH`
- [ ] Módulo `accounts`: CRUD, archivar, consulta de saldos
- [ ] Cálculo del ahorro total y de `equivalent` en `common/money`
- [ ] `common/text/normalizeName` (minúsculas, sin tildes, `ñ` conservada) con sus pruebas unitarias
- [ ] Columna `name_normalized` y `@@unique([userId, kind, nameNormalized])`; calcularla al crear, al renombrar y en el alta inicial; anularla al archivar
- [ ] Módulo `categories`: CRUD, archivar, traducción de la restricción única (`P2002`) a `409 CATEGORY_NAME_TAKEN` con `field: name`; `nameNormalized` fuera de todos los DTO
- [ ] `fxConfigured` en `GET /settings`; bloqueo `FX_RATE_NOT_CONFIGURED` en cuentas, tarjetas, transferencias y `displayCurrency`
- [ ] Mover el alta inicial de la Fase 1 a los servicios de `settings`, `categories` y `accounts`, para que haya una sola forma de crear cada cosa
- [ ] Sembrar el tipo de cambio del usuario nuevo desde el global, cuando exista
- [ ] DTO de salida con serialización de `Decimal`

## 7. Pruebas

Sin tipo de cambio configurado:

| Caso | Esperado |
|---|---|
| `GET /settings` de un usuario recién registrado | `fxConfigured: false`, `fx.rate: null` |
| `GET /accounts` | `savings.main` con el total, `savings.secondary: null`, ningún `equivalent` |
| Crear una cuenta en PEN | `201` |
| Crear una cuenta en USD | `422 FX_RATE_NOT_CONFIGURED` |
| `PATCH /settings` con `displayCurrency: "USD"` | `422 FX_RATE_NOT_CONFIGURED` |
| `PUT /settings/fx` con `rate: "3.20"` | `200`; `fxConfigured: true` |
| Después, crear una cuenta en USD | `201` |
| `PUT /settings/fx` sin `rate`, con `0` o negativo | `422 RATE_REQUIRED` |

Nombre de categoría:

| Caso | Esperado |
|---|---|
| Crear "alimentación" de egreso cuando existe "Alimentación" de egreso | `409 CATEGORY_NAME_TAKEN`, con `errors[0].field = "name"` |
| Crear "alimentacion" (sin tilde) | `409` |
| Crear "ALIMENTACIÓN" | `409` |
| Crear " Alimentación " (con espacios) | `409` |
| Crear "Otros" de ingreso cuando existe "Otros" de egreso | `201` |
| Crear "Ano" cuando existe "Año" | `201`: la `ñ` no es una tilde |
| Crear "Ocio" tras archivar la categoría "Ocio" | `201` |
| Renombrar "Salud" a "ocio" existiendo "Ocio" | `409` |
| Renombrar "Ocio" a "OCIO", o "Alimentacion" a "Alimentación" | `200`, y `GET /categories` devuelve el nombre nuevo tal cual |
| `GET /categories` tras crear "ALIMENTACIÓN" en una cuenta sin esa categoría | Devuelve `name: "ALIMENTACIÓN"`; ninguna respuesta contiene `nameNormalized` |
| Enviar `nameNormalized` en el cuerpo | `400` (campo no admitido) |
| Dos peticiones simultáneas que crean "Mascotas" y "mascotas" | Una `201`, otra `409`; una sola fila |
| El usuario B crea "Alimentación" teniendo A la suya | `201`: la unicidad es por usuario |
| Las 9 categorías del alta inicial | Tienen `name_normalized` calculado |

Unitarias:

- Ahorro total de los datos de ejemplo: `7166.30` en PEN y `2239.47` en USD (7,166.30 ÷ 3.20 = 2,239.46875, *half up*).
- Una cuenta con `includeInSavings = false` no suma.
- Una cuenta en EUR con par PEN/USD no suma ni tiene `equivalent`.
- `effectiveRate` con par directo, con par inverso, y sin datos.

e2e:

- Crear una cuenta con saldo inicial `50` devuelve `balance: "50.00"`.
- `PATCH` con `currency` devuelve `400`.
- Archivar una cuenta la quita de `GET /accounts` y la conserva con `includeArchived`.
- `PUT /settings/fx` inserta una fila y conserva la anterior; `GET /settings` devuelve la nueva.
- Cambiar el par a EUR/USD con visualización en PEN deja la visualización en EUR.
- `displayCurrency` fuera del par da `422`.
- Aislamiento: el usuario B no puede leer, editar ni archivar cuentas y categorías de A (`404`).
- Nombre de 61 caracteres, nombre solo con espacios, color `rojo`, moneda `XXX`: cada uno con su código.

## 8. Criterios de aceptación

- Un usuario recién registrado ve "Configura tu tipo de cambio", no puede crear nada en otra moneda, y tras escribir el cambio todo queda habilitado.
- No se pueden tener dos categorías activas del mismo tipo con el mismo nombre, se escriba como se escriba.
- Con la semilla de ejemplo, `GET /accounts` devuelve los cinco saldos de la sección 11 de `ORBITA_SPEC.md` y el ahorro total S/ 7,166.30 = US$ 2,239.47.
- Apagar "Contar en el total de ahorros" en una cuenta cambia el total en la siguiente lectura.
- Todas las rutas pasan la prueba de aislamiento entre usuarios.

## 9. Riesgos

| Riesgo | Mitigación |
|---|---|
| El cliente y el servidor calculan el ahorro total por separado y divergen | Vectores de prueba compartidos; el servidor es la referencia |
| `FxPair.rate` opcional toca muchas pantallas de Android | Cambio acotado y listado en [14](14-integracion-android.md) §5; el compilador de Kotlin señala cada uso |
| La app y el backend normalizan los nombres de forma distinta y el aviso previo de la app no coincide con el `409` | Los casos de normalización van en los vectores compartidos; la decisión final siempre es del backend |
| Una normalización demasiado agresiva une nombres que el usuario ve distintos | Solo se quitan mayúsculas, tildes y diéresis; la `ñ` se conserva |
| El usuario cambia de par y pierde el cambio anterior | El historial se conserva en `exchange_rates`; al volver a un par ya usado, `effectiveRate` lo recupera |
