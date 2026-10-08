# 01 · Arquitectura y convenciones del API

Reglas comunes a todas las fases. Si un plan de fase contradice este documento, manda este documento.

## 1. Vista general

```
Android · Web · iOS
        │  HTTPS · JSON · JWT de acceso
        ▼
┌──────────────────────────────┐
│ Proxy inverso (TLS, límites) │
└──────────────┬───────────────┘
               ▼
┌──────────────────────────────┐      ┌───────────────────────┐
│ API NestJS (sin estado)      │─────▶│ PostgreSQL (Docker)    │
│ 1..N réplicas                │      │ volumen + respaldos    │
└──────────────┬───────────────┘      └───────────────────────┘
               │ tarea diaria
               ▼
     Proveedor de tipo de cambio
```

Es un **monolito modular**: un solo despliegue, con módulos de negocio aislados entre sí. No se parte en microservicios; el dominio es pequeño y las operaciones críticas (pagar una compra) necesitan transacciones de base de datos.

El API **no guarda estado en memoria** entre peticiones (ni sesiones, ni cachés obligatorias). Eso permite levantar más réplicas sin cambios ([13](13-escalabilidad-y-operacion.md)).

## 2. Stack

Stack confirmado por el usuario: **NestJS + PostgreSQL + Prisma**, sin Supabase ni ningún servicio de backend gestionado, con autenticación propia.

Versiones verificadas con `npm view` el 7 oct 2026 (detalle y evidencia en la sección 2.1).

| Área | Elección | Versión | Nota |
|---|---|---|---|
| Runtime | Node.js | 24 | El proyecto ya es ESM (`"type": "module"`) |
| Framework | NestJS + Express | 12.1.2 | Ya en el `pnpm-lock.yaml` |
| Gestor de paquetes | pnpm | 11.20.0 | Fijado en `packageManager`; se usa con Corepack |
| Base de datos | PostgreSQL en Docker | 17 | `postgres:17-alpine`, la que ya usa el `docker-compose.yml` |
| ORM | `prisma`, `@prisma/client`, `@prisma/adapter-pg` | **7.10.0 exacta** | Sin `^` ni `latest` (H17) |
| Validación | `class-validator` + `class-transformer` | 0.15.1 / 0.5.1 | En lugar de `nestjs-zod` (H16) |
| Correo | `nodemailer` por SMTP, con **Resend** como proveedor inicial | 10.0.16 | Detrás de la interfaz `MailService`; detalle en [04](04-fase-1-autenticacion.md) §7 |
| Configuración | `@nestjs/config` | 12.0 | Con validación del entorno al arrancar |
| Contraseñas | `@node-rs/argon2` (Argon2id) | 2.2 | Binarios precompilados, sin `node-gyp` |
| Tokens | `jose` (ES256) | 6.2 | Firma asimétrica con rotación de claves |
| Límite de peticiones | `@nestjs/throttler` | 6.7 | Almacén en Redis al pasar a varias réplicas |
| Documentación | `@nestjs/swagger` (OpenAPI) | 12.0 | Solo expuesta fuera de producción |
| Tareas programadas | `@nestjs/schedule` | 12.0 | Con candado en Postgres |
| Salud | `@nestjs/terminus` | 12.1 | |
| Registros | `nestjs-pino` + `pino` | 5.3 / 10.4 | JSON estructurado |
| Cabeceras | `helmet` | 8.3 | |
| PDF | `pdfkit` | 0.20 | Sin navegador sin interfaz |
| Pruebas | Vitest, Supertest, `@testcontainers/postgresql` | 4.1 / 7 / 12.2 | Vitest ya está configurado |
| Lint y formato | oxlint, Prettier | — | Ya configurados |

Lo que **no** se agrega todavía: Redis, colas, caché distribuida, CQRS, GraphQL. Cada uno tiene un disparador concreto en [13](13-escalabilidad-y-operacion.md).

### 2.1 Versiones verificadas y por qué

Consultado con `npm view <paquete> version|dist-tags|peerDependencies|engines` el 7 oct 2026.

| Paquete | Versión | Lo que declara | Conclusión |
|---|---|---|---|
| `@nestjs/common` | 12.1.2 | `peerDependencies`: `class-validator >=0.13.2`, `class-transformer >=0.4.1`, `rxjs ^7.1.0`, `reflect-metadata ^0.1.12 \|\| ^0.2.0` | `class-validator` es la validación que Nest espera |
| `@nestjs/core`, `@nestjs/platform-express`, `@nestjs/testing` | 12.1.2 | `@nestjs/common ^12.0.0` | Alineados entre sí |
| `class-validator` | 0.15.1 | Sin `peerDependencies` | Cumple `>=0.13.2` de Nest |
| `class-transformer` | 0.5.1 | Sin `peerDependencies` | Cumple `>=0.4.1` de Nest |
| `@nestjs/mapped-types` | 12.0.0 | `class-validator ^0.13 \|\| ^0.14 \|\| ^0.15`, `class-transformer ^0.4 \|\| ^0.5`, Nest `^10 \|\| ^11 \|\| ^12` | Compatible con las versiones elegidas |
| `@nestjs/swagger` | 12.0.2 | Nest `^12.0.0`, `typescript ^5.5 \|\| ^6.0`, `class-validator *`, `class-transformer *` | Compatible; el proyecto usa TypeScript 6.0.3 |
| `nestjs-zod` | 5.5.0 | Nest `^10.0.0 \|\| ^11.0.0` | **No declara Nest 12.** Descartado |
| `prisma` (CLI) | 7.10.0 | `typescript >=5.4.0`; `engines.node`: `^20.19 \|\| ^22.12 \|\| >=24.0` | Compatible con Node 24 y TypeScript 6 |
| `@prisma/client` | 7.10.0 | `prisma *`, `typescript >=5.4.0` | Misma versión que el CLI |
| `@prisma/adapter-pg` | 7.10.0 | Depende de `pg ^8.16.3` y de `@prisma/driver-adapter-utils 7.10.0` | Va atado a la misma versión de Prisma |

**Prisma: versión elegida `7.10.0`, fijada exacta en los tres paquetes.**

- Es la **última versión estable**: publicada el 25 ago 2026, es el tag `latest` de `@prisma/client` y el tag `prev` del CLI.
- El tag `latest` del paquete `prisma` apunta hoy a **`8.0.0-rc.21`**, una versión candidata. Con `pnpm add prisma` o con un rango `^` se instalaría un CLI de la serie 8 junto a un cliente de la serie 7.
- `@prisma/adapter-pg` depende de la versión exacta de las utilidades internas de Prisma: mezclar versiones entre los tres paquetes no está soportado.
- En `package.json` van como `"prisma": "7.10.0"`, `"@prisma/client": "7.10.0"` y `"@prisma/adapter-pg": "7.10.0"`, **sin `^`, sin `~` y sin `latest`**. El `pnpm-lock.yaml` y `--frozen-lockfile` impiden que cambien solas.
- Subir de versión es un cambio deliberado: los tres a la vez, con las pruebas de integración en verde. Pasar a Prisma 8 se evalúa cuando sea estable, como tarea propia.

`class-validator` y `class-transformer` también se fijan exactos (`0.15.1` y `0.5.1`): son versiones `0.x`, donde un cambio menor puede romper.

Antes de ejecutar `pnpm add` en la Fase 0 hay que repetir esta consulta: si ha salido una `7.10.x` de corrección, se toma esa y se actualiza esta tabla.

## 3. Estructura de carpetas

```
prisma/
  schema.prisma
  migrations/
  seed.ts
src/
  main.ts
  app.module.ts
  config/                 # esquema y carga del entorno
  common/
    errors/               # AppError, catálogo de códigos, filtro global
    money/                # Decimal, redondeo, conversión, formato
    text/                 # normalizeName: minúsculas y sin tildes, para comparar nombres
    dates/                # LocalDate, "hoy" por zona horaria
    pagination/           # cursores
    auth/                 # guard global, @CurrentUser(), @Public()
    http/                 # interceptores, id de petición, ETag
  prisma/                 # PrismaModule, PrismaService
  modules/
    auth/
    users/
    settings/
    currencies/
    exchange-rates/
    accounts/
    categories/
    transactions/
    transfers/
    entries/
    reports/
    credit/
    exports/
    home/
    health/
test/
  e2e/
  fixtures/               # datos de ejemplo y vectores de prueba
docs/plans/
```

Cada módulo de negocio sigue la misma forma:

```
modules/accounts/
  accounts.module.ts
  accounts.controller.ts      # HTTP: DTO de entrada y de salida, nada de reglas
  accounts.service.ts         # reglas de negocio y transacciones
  accounts.repository.ts      # único lugar que toca Prisma
  dto/
  accounts.service.spec.ts
```

Reglas de dependencia:

- `controller → service → repository → Prisma`. Un controlador nunca usa Prisma.
- Un módulo usa a otro **solo a través de su servicio exportado**, nunca de su repositorio.
- `common/` no importa nada de `modules/`.
- **Todo método de repositorio recibe `userId` como primer parámetro** y lo incluye en el `where`. No existe ningún método que busque por `id` solamente ([11](11-seguridad.md), sección 2).

## 4. Convenciones HTTP

| Tema | Regla |
|---|---|
| Prefijo y versión | `/api/v1`. Un cambio incompatible crea `/api/v2`; `v1` sigue vivo mientras haya apps publicadas que lo usen |
| Formato | JSON, propiedades en `camelCase` |
| Identificadores | UUID. El cliente puede enviar `id` al crear; si no lo envía, lo genera el servidor |
| Montos | **Texto** con punto decimal y 2 decimales: `"1245.80"`. Nunca número JSON |
| Tipos de cambio | Texto, hasta 6 decimales: `"3.200000"` |
| Fechas | `YYYY-MM-DD` (`occurredOn`, `dueDate`). Sin hora ni zona |
| Instantes | ISO 8601 en UTC (`createdAt`, `updatedAt`) |
| Enumerados | Minúsculas, iguales a la base: `income`, `expense`, `cash`, `debit`, `savings`, `other`, `pending`, `paid`, `manual`, `auto` |
| Monedas | ISO 4217 en mayúsculas, del catálogo `GET /currencies` |
| Autenticación | `Authorization: Bearer <token de acceso>`. Todo endpoint la exige salvo los marcados públicos |
| Listas | `{ "data": [...], "meta": { ... } }` |
| Recurso único | El objeto directamente |
| Sin contenido | `204` |

Entrada de montos: se aceptan solo cadenas que cumplan `^\d{1,12}(\.\d{1,2})?$`. Se rechaza la coma decimal, la notación científica, los signos y más de 2 decimales. El cliente normaliza antes de enviar.

### Códigos de estado

| Código | Cuándo |
|---|---|
| `200` / `201` / `204` | Éxito, creado, sin contenido |
| `400` | Cuerpo o parámetros mal formados (tipo, formato, campo desconocido) |
| `401` | Sin token, token inválido o vencido, credenciales incorrectas |
| `403` | Autenticado pero no permitido (correo sin confirmar) |
| `404` | No existe **o pertenece a otro usuario** (no se distingue) |
| `409` | Conflicto de estado (correo ya registrado, nombre repetido, compra ya pagada) |
| `422` | Datos bien formados que violan una regla de negocio |
| `429` | Límite de peticiones, con cabecera `Retry-After` |
| `500` / `503` | Fallo interno, dependencia caída |

### Formato de error

Sigue RFC 9457 (`application/problem+json`) con un `code` estable que el cliente usa para elegir el mensaje en su idioma. **El API no envía textos de interfaz**; `detail` es solo para depurar.

```json
{
  "type": "https://orbita.app/problems/validation-failed",
  "title": "Validation failed",
  "status": 422,
  "code": "VALIDATION_FAILED",
  "detail": "One or more fields are invalid.",
  "requestId": "01JC…",
  "errors": [
    { "field": "amount", "code": "AMOUNT_REQUIRED" },
    { "field": "occurredOn", "code": "FUTURE_DATE" }
  ]
}
```

Los códigos de `errors[]` son los mismos del enumerado `ValidationError` de Android, y se devuelven **en el orden de revisión** de la sección 4.1 de `ORBITA_SPEC.md`, de modo que el cliente puede mostrar el primero.

| `code` | HTTP | Equivalente en Android |
|---|---|---|
| `VALIDATION_FAILED` | 400 / 422 | `ValidationError.*` según `errors[].code` |
| `INVALID_CREDENTIALS` | 401 | `DataError.INVALID_CREDENTIALS` |
| `UNAUTHENTICATED` | 401 | Sesión vencida: renovar o volver a iniciar sesión |
| `EMAIL_NOT_CONFIRMED` | 403 | `DataError.EMAIL_NOT_CONFIRMED` |
| `NOT_FOUND` | 404 | `DataError.NOT_FOUND` |
| `EMAIL_ALREADY_REGISTERED` | 409 | `DataError.EMAIL_ALREADY_REGISTERED` |
| `CATEGORY_NAME_TAKEN` | 409 | Nuevo en Android: mensaje bajo el campo Nombre (H5) |
| `FX_RATE_NOT_CONFIGURED` | 422 | Nuevo en Android: "Configura tu tipo de cambio" (H4) |
| `PURCHASE_ALREADY_PAID` | 409 | Nuevo en Android |
| `CARD_HAS_PENDING_PURCHASES` | 409 | Nuevo en Android: no se archiva una tarjeta con deuda |
| `PAYMENT_EXPENSE_LOCKED` | 422 | `DataError.PAYMENT_LOCKED`: el egreso de un pago solo deja cambiar cuenta, monto y fecha |
| `ID_CONFLICT` | 409 | `DataError.UNKNOWN` |
| `RATE_LIMITED` | 429 | Nuevo en Android |
| `INTERNAL_ERROR` | 500 | `DataError.UNKNOWN` |

Códigos de campo propios del servidor, además de los de Android: `INVALID_FORMAT`, `CURRENCY_NOT_SUPPORTED`, `CATEGORY_KIND_MISMATCH`, `ACCOUNT_ARCHIVED`, `CATEGORY_ARCHIVED`, `CARD_ARCHIVED`, `AMOUNTS_MUST_MATCH`, `RANGE_TOO_LARGE`.

## 5. Dinero

- Tipo en el servidor: `Prisma.Decimal`. Prohibido `number` para montos y tipos de cambio; una regla de lint y la revisión de código lo vigilan.
- Todas las operaciones pasan por `common/money`: `add`, `subtract`, `convert`, `round2`, `percentOf`. Redondeo `ROUND_HALF_UP`, una sola vez al final.
- Conversión: `x × cambio(A→B)` si existe ese par; `x ÷ cambio(B→A)` si solo existe el inverso. La división intermedia usa 10 decimales, igual que `FxPair` en Android.
- Porcentaje: `total × 100 ÷ total_de_la_moneda`, 1 decimal, *half up*; `0` si el total es cero.
- Las sumas se hacen en SQL (`SUM` sobre `NUMERIC`), no trayendo filas a memoria.
- Los DTO de salida serializan con `toFixed(2)` (montos) y `toFixed(6)` (tipos de cambio).

## 6. Fechas y "hoy"

Decisión aprobada. El servidor corre en UTC, y "hoy" depende de dónde esté el usuario, así que:

- El perfil guarda `timezone` (IANA, por ejemplo `America/Lima`, que es el valor inicial). El cliente la envía al registrarse y puede actualizarla con `PATCH /settings`.
- `common/dates` expone `todayFor(timezone)`. **Ninguna** regla usa `new Date()` ni `CURRENT_DATE` de Postgres para decidir el día.
- **Ninguna columna de fecha tiene valor por defecto en la base** (`occurred_on`, `purchase_date`, `rate_date`, `paid_on`). El esquema original usaba `default current_date`; se elimina. Si el servicio no pone la fecha, la inserción falla, que es lo correcto.
- La fecha de un movimiento se guarda en una columna **`DATE`** (`@db.Date` en Prisma) con el día **ya calculado** en la zona del usuario. No se guarda un instante para deducir el día después: el día de un movimiento no cambia aunque el usuario viaje o cambie su zona horaria.
- Si el cliente no envía `occurredOn` al crear un movimiento, el servidor usa `todayFor(timezone)`.
- "Fecha no posterior a hoy" se valida contra `todayFor(timezone)`.
- Las columnas `DATE` llegan de Prisma como `Date` a medianoche UTC. Se convierten a `YYYY-MM-DD` con los métodos UTC, nunca con los locales. Hay una prueba que lo fija.
- En pruebas, el reloj se inyecta (`Clock`) para poder fijar "hoy" en 2 oct 2026, como en los datos de ejemplo.

## 7. Paginación

Por cursor, nunca por `offset`. El orden de las listas de movimientos es `occurred_on DESC, created_at DESC, id DESC` y el cursor codifica esos tres valores en base64url opaco.

- `limit` por defecto 50, máximo 100.
- La respuesta incluye `meta.nextCursor` (o `null` si no hay más).
- Las listas pequeñas y acotadas (cuentas, categorías, tarjetas) no se paginan.

## 8. Idempotencia y reintentos

Las apps reintentan cuando la red falla, así que crear dos veces no debe duplicar.

- **Crear con `id` del cliente.** Si ya existe una fila propia con ese `id` y el mismo contenido, se devuelve la existente con `200`. Si el contenido difiere, `409 ID_CONFLICT`.
- **Actualizar** (`PATCH`) y **eliminar** (`DELETE`) son idempotentes por naturaleza. Eliminar algo ya eliminado devuelve `204`.
- **Pagar una compra** se protege con una actualización condicional (`status = 'pending'`); el segundo intento recibe `409 PURCHASE_ALREADY_PAID`.

## 9. Caché y frescura

- Respuestas de usuario: `Cache-Control: private, no-cache` con `ETag`. Si el cliente envía `If-None-Match` y nada cambió, `304` sin cuerpo.
- `GET /currencies`: `Cache-Control: public, max-age=86400`.
- No hay caché en el servidor en las primeras fases; las consultas van a índices.

## 10. Configuración

Todo por variables de entorno, validadas al arrancar. Si falta una obligatoria o tiene formato inválido, **el proceso no arranca**.

| Variable | Ejemplo | Uso |
|---|---|---|
| `NODE_ENV` | `production` | |
| `PORT` | `3000` | |
| `DATABASE_URL` | `postgresql://orbita_app:…@db:5432/orbita` | Rol de aplicación, sin permisos de esquema |
| `DATABASE_MIGRATE_URL` | `postgresql://orbita_migrator:…` | Solo para migraciones |
| `DATABASE_POOL_MAX` | `10` | Conexiones por réplica |
| `JWT_PRIVATE_KEY`, `JWT_PUBLIC_KEYS`, `JWT_KEY_ID` | PEM / JWKS | Firma y verificación |
| `JWT_ACCESS_TTL` | `900` | Segundos |
| `REFRESH_TTL_DAYS`, `REFRESH_ABSOLUTE_TTL_DAYS` | `30`, `90` | |
| `CORS_ORIGINS` | `https://app.orbita.pe` | Lista separada por comas |
| `TRUST_PROXY` | `1` | Saltos de proxy de confianza |
| `AUTH_REQUIRE_EMAIL_VERIFICATION` | `false` | |
| `MAIL_TRANSPORT` | `smtp` | `memory` solo en pruebas |
| `MAIL_SMTP_HOST`, `MAIL_SMTP_PORT`, `MAIL_SMTP_SECURE` | `smtp.resend.com`, `465`, `true` | Mailpit en desarrollo |
| `MAIL_SMTP_USER`, `MAIL_SMTP_PASSWORD` | `resend`, clave de API | La contraseña es un secreto |
| `MAIL_FROM`, `MAIL_REPLY_TO` | `Orbita <no-reply@mail.…>` | Del dominio verificado |
| `PASSWORD_RESET_URL`, `PASSWORD_RESET_TTL_MINUTES` | —, `30` | Enlace de recuperación |
| `EMAIL_VERIFY_URL`, `EMAIL_VERIFY_TTL_HOURS` | —, `24` | Verificación de correo |
| `FX_PROVIDER`, `FX_PROVIDER_API_KEY`, `FX_CRON` | — | Fase 6 |
| `LOG_LEVEL` | `info` | |

Se versiona `.env.example` sin valores reales. `.env` ya está en `.gitignore`.

## 11. Registros y trazas

- Un registro JSON por petición: método, ruta, estado, duración, `requestId`, `userId`.
- `requestId` se toma de `X-Request-Id` si viene del proxy, o se genera. Se devuelve en la respuesta y en los errores.
- **Se censuran** siempre: `authorization`, `cookie`, `password`, `currentPassword`, `newPassword`, `refreshToken`, `accessToken`.
- No se registran cuerpos de petición con montos ni descripciones.
- Los eventos de autenticación se registran aparte ([04](04-fase-1-autenticacion.md)).

## 12. Definición de "terminado"

Una tarea está terminada cuando:

1. El código pasa `pnpm lint`, la comprobación de tipos, `pnpm test` y `pnpm test:e2e`.
2. Tiene pruebas unitarias de sus reglas y pruebas e2e de sus endpoints, incluida la de aislamiento entre usuarios.
3. El contrato OpenAPI generado está actualizado y revisado.
4. Los cambios de esquema van en una migración versionada, probada sobre una base con datos.
5. No introduce secretos, ni `number` para dinero, ni consultas sin `userId`.
6. El plan de la fase tiene sus casillas marcadas.
