# 00 · Fundaciones

## Objetivo

Reemplazar el esqueleto de `cmd/api/main.go` por la estructura de `AGENT_GUIDE.md` §3: configuración validada, logs, errores y respuestas con forma única, base de datos con migraciones, transacciones, salud y un Postgres de desarrollo. Al terminar, cualquier módulo de negocio se escribe sin volver a tocar la infraestructura.

No entra ningún endpoint de negocio.

## Pantallas de Android que cubre

Ninguna. Es la base de todo lo demás.

## Lo que ya existe (no se rediseña)

| Pieza | Estado |
|---|---|
| `Dockerfile` | Multi-stage, binario estático, `alpine` con `tzdata`, usuario no root, `HEALTHCHECK` con `/app/api healthcheck` |
| `deploy/docker-compose.yml` | Servicio `migrate` (comando `migrate`) y `app`, red `atm-network`, `127.0.0.1:3071:3001`, 512 MB |
| `.github/workflows/deploy.yml` | `check` (commit en `main`, `go vet`, `golangci-lint`, `go test`, `go build`) → `build` (GHCR) → `deploy` (SSH, espera `healthy`) |
| `scripts/provision-db.sql` | Rol, base y esquema `orbita` |
| `cmd/api/main.go` | Gin con `GET /api/v1/health`, subcomandos `healthcheck` y `migrate` (vacío) |

## Módulos

`internal/platform/*` (sin reglas de negocio) e `internal/integrations/mail` (el puerto; Resend se usa en el plan 11).

## Endpoints

| Método y ruta | Público | Respuesta |
|---|---|---|
| `GET /api/v1/health` | Sí | `200 { "data": { "status": "ok" } }` si el proceso responde (liveness; lo usa el healthcheck de Docker) |
| `GET /api/v1/health/ready` | Sí | `200 { "data": { "status": "ok", "database": "ok" } }`; `503` con `{ "error": { "code": "DATABASE_UNAVAILABLE", … } }` si la base no responde en 2 s |

El healthcheck de Docker sigue apuntando a `/api/v1/health` (liveness). Que la base falle no debe reiniciar el contenedor en bucle.

## Estructura que se crea

```
cmd/api/main.go                    carga config, logger, pool, módulos y rutas; subcomandos migrate y healthcheck
internal/platform/
  config/        Load(): lee el entorno, valida y junta todos los errores en un mensaje
  logger/        slog JSON (texto legible en development); request_id en cada línea
  database/      pgxpool con timeouts; Ping
  tx/            Runner.InTx(ctx, func(ctx) error): abre pgx.Tx y la pasa por el contexto
  httpserver/    Gin, timeouts del servidor, graceful shutdown, grupo /api/v1
  middleware/    request id, recover, logging de acceso, CORS, rate limit por IP
  apierror/      tipo Error{Status, Code, Message, Details}; Respond(c, err); OK(c, data); Page(c, data, meta)
  clock/         Clock con Now(); uno fijo para pruebas
  ids/           New() UUID v7; Parse() acepta cualquier UUID que mande el cliente
  money/         decimal (shopspring/decimal): parseo estricto de texto, Round2, JSON como texto
  dates/         LocalDate YYYY-MM-DD; Today(clock, timezone)
  text/          NormalizeName (minúsculas, sin tildes, conserva la ñ) — la usan categorías
internal/integrations/mail/
  mailer.go      puerto Mailer
  console.go     implementación de desarrollo: escribe el correo en el log
db/
  migrations/    goose, SQL; embebidas con //go:embed
  sqlc.yaml      una entrada por módulo (se agregan con cada plan)
docker-compose.dev.yml             Postgres 17 local, la versión de producción (base orbita, rol orbita_app, esquema orbita, puerto 5432)
docs/openapi.yaml                  contrato; arranca con health
```

## Tablas y migraciones

Ninguna de negocio. `migrate` se conecta a goose con las migraciones embebidas y la tabla de control `goose_db_version` dentro del esquema `orbita`. Con el historial vacío, `migrate` termina bien (el despliegue actual no se rompe).

Subcomandos: `api migrate` (aplica las pendientes, para el compose), `api migrate down` y `api migrate status` (solo en local).

## Reglas

- **Configuración.** La app no arranca si falta una variable obligatoria o tiene mal formato; el error lista **todas** las que fallan. Obligatorias: `APP_ENV`, `PORT`, `DATABASE_URL`, `JWT_ACCESS_SECRET` (≥ 32 caracteres), `ACCESS_TOKEN_TTL_SECONDS`, `REFRESH_TOKEN_TTL_DAYS`, `RATE_LIMIT_PER_MINUTE`, `LOGIN_RATE_LIMIT_PER_MINUTE`. `CORS_ORIGIN`: obligatoria en producción. `RESEND_API_KEY` y `MAIL_FROM`: obligatorias en producción; en desarrollo, sin ellas el correo va a la consola. `R2_*`: no se leen hasta que un plan las necesite.
- **Formato de respuesta** (guía §4): éxito `{ "data": … }`; paginado `{ "data": [...], "meta": { "page", "page_size", "total" } }`; error `{ "error": { "code", "message", "details" } }`. `code` en inglés y `SCREAMING_SNAKE_CASE`; `message` en español. Campos JSON en `snake_case`.
- **Errores base:** `VALIDATION_FAILED` (400, `details` = lista de `{ field, code }`), `UNAUTHORIZED` (401), `NOT_FOUND` (404), `CONFLICT` (409), `ID_CONFLICT` (409), `TOO_MANY_REQUESTS` (429, cabecera `Retry-After`), `INTERNAL_ERROR` (500, sin detalles), `DATABASE_UNAVAILABLE` (503). Un error no controlado se registra completo y al cliente le llega solo `INTERNAL_ERROR`.
- **Errores de validación por campo.** `details: [{ "field": "amount", "code": "AMOUNT_REQUIRED" }]`, con los códigos del enum `ValidationError` de Android y en su orden de revisión (`ORBITA_SPEC` §4.1). Así la app muestra el primero con su propio texto.
- **Dinero.** Entrada solo como texto que cumpla `^\d{1,12}(\.\d{1,2})?$`; se rechazan número JSON, coma, signo y notación científica. Salida con 2 decimales (`"1245.80"`). Tipos de cambio hasta 6 decimales. Nunca `float64`.
- **Request id.** Se respeta `X-Request-Id` del cliente si es seguro (máx. 128 caracteres, `[A-Za-z0-9._-]`); si no, se genera. Va en la respuesta y en cada línea de log.
- **Logs.** Nunca se registran `Authorization`, cookies, cuerpos de autenticación, contraseñas ni tokens. No se registra `/health`.
- **Timeouts.** Servidor: `ReadHeaderTimeout` 5 s, `ReadTimeout` 15 s, `WriteTimeout` 20 s, `IdleTimeout` 60 s. Consultas: contexto de la petición más un tope de 5 s en el pool. Cierre ordenado de 10 s.
- **Cuerpo máximo:** 1 MB (`http.MaxBytesReader`); más grande → `413`.
- **Rate limit por IP** (`RATE_LIMIT_PER_MINUTE`), en memoria (una sola réplica). La IP real sale de `X-Forwarded-For` solo si la petición llega desde loopback o red privada (nginx del host), con `gin.SetTrustedProxies`.
- **CORS** solo para los orígenes de `CORS_ORIGIN` (separados por coma). Las apps nativas no lo usan.
- **Transacciones** solo con `tx.Runner`, nunca abiertas en un handler. Los repositorios toman la `pgx.Tx` del contexto si existe; si no, usan el pool.

## Dependencias

Ninguna.

## Pasos de implementación

1. Quitar el binario `api` del repo y agregar `/api` a `.gitignore` y `.dockerignore` (decisión del 21 oct 2026).
2. Base local: el Postgres del usuario en Docker, con la base `orbita`, el rol `orbita_app` y el esquema `orbita` creados con `scripts/provision-db.sql`. **Todas las pruebas de integración apuntan a esa base local** (decisión del 21 oct 2026), con `TEST_DATABASE_URL`; cada prueba crea sus propios usuarios y los borra al terminar. Se entrega además `docker-compose.dev.yml` (guía §7) con el mismo Postgres, para quien no lo tenga.
3. `internal/platform/config`, `logger`, `clock`, `ids`, `apierror` con sus pruebas.
4. `money`, `dates` y `text` con los vectores de Android (abajo).
5. `database` + `tx` + goose embebido; subcomando `migrate`.
6. `httpserver` + middlewares; `GET /health` y `/health/ready`.
7. `integrations/mail`: puerto `Mailer` e implementación `console`.
8. Prueba de arquitectura y regla `depguard` en `.golangci.yml`: `domain` no importa gin/pgx/net/http; un módulo no importa paquetes internos de otro.
9. `docs/openapi.yaml` con health y los esquemas comunes (error, página, dinero).
10. Actualizar `.env.example` si cambia algo y la sección 6 de la guía si cambia el despliegue (no debería).

## Pruebas

Unitarias:
- `config`: falta `DATABASE_URL` → error que la nombra; varias faltantes → todas en un mensaje; `JWT_ACCESS_SECRET` corto → error.
- `money` (vectores de Android, `docs/06`): `0.1 + 0.2 = 0.3`; `"45.52"` válido; `"45.523"`, `"0"`, `"-5"`, `"1e3"`, `"45,52"` y el número JSON `45.52` inválidos como entrada del API; `1245.8` sale como `"1245.80"`; redondeo HALF_UP.
- `text.NormalizeName`: "Alimentación", "alimentacion", "ALIMENTACIÓN" → misma clave; "Año" ≠ "Ano"; espacios al inicio, al final y repetidos se ignoran; `ñ` escrita como `n` + tilde combinante también se conserva. Debe dar **exactamente** lo mismo que `normalizeName` de Kotlin (`domain/model/Names.kt`).
- `dates.Today`: a las 20:00 de Lima del 2 oct 2026 (01:00 UTC del 3 oct) "hoy" es 2 oct en `America/Lima`.
- `ids.Parse`: acepta UUID v4 (los que genera Android) y v7; rechaza otro texto.

Integración:
- `migrate` sobre una base vacía: crea `goose_db_version` en el esquema `orbita`, no en `public`.
- `GET /health/ready` con la base arriba → `200`; con la base apagada → `503`.
- Un panic en un handler → `500 INTERNAL_ERROR`, el proceso sigue vivo.
- Una ruta inexistente → `404 NOT_FOUND` con el formato de error.
- Respuesta con `X-Request-Id`, el mismo que mandó el cliente.

## Criterios de aceptación

- `go vet ./...`, `golangci-lint run`, `go test ./...` y `go build ./...` pasan.
- `docker compose -f docker-compose.dev.yml up -d` + `go run ./cmd/api migrate` + `go run ./cmd/api` dejan el API respondiendo en `http://localhost:3001/api/v1/health/ready`.
- Sin una variable obligatoria, el proceso termina con un mensaje que la nombra.
- La imagen se construye igual que hoy y un despliegue de este plan funciona con el `.env` actual del servidor (sin variables nuevas obligatorias).

## Variables nuevas para el servidor

Ninguna.

## Cómo correrlo en local

```powershell
docker compose -f docker-compose.dev.yml up -d          # Postgres 17 en 127.0.0.1:5432
# solo la primera vez:
Get-Content -Raw scripts\provision-db.sql | docker exec -i orbita-postgres-dev psql -v ON_ERROR_STOP=1 -U postgres -v "app_password='orbita_dev'" -d postgres

go run ./cmd/api migrate                                 # aplica las migraciones (lee DATABASE_URL del entorno)
go run ./cmd/api                                         # http://localhost:3001/api/v1/health/ready

$env:TEST_DATABASE_URL="postgresql://orbita_app:orbita_dev@localhost:5432/orbita?search_path=orbita"
go vet ./...; golangci-lint run; go test ./...; go build ./...
```

`go run` y `go test` leen las variables del entorno de la terminal, no del archivo `.env`.

## Estado (21 oct 2026)

Implementado y en revisión. Verificado en local:

| Comprobación | Resultado |
|---|---|
| `go vet ./...` | Sin problemas |
| `golangci-lint run` (v2.15.0, compilado con go1.27.3, con `.golangci.yml`) | `0 issues` |
| `go test ./...` sin `TEST_DATABASE_URL` (como el check del despliegue) | Todo pasa; las pruebas de integración se saltan |
| `go test ./...` contra el Postgres 17 local | Todo pasa: migraciones (tabla de control en `orbita`, up y down), transacciones (commit, rollback por error y por panic, anidadas) y el servidor con la base real |
| `go build ./...` | Compila |
| Binario: `api migrate`, `api`, `api healthcheck` | `no migrations to apply`; `/health` y `/health/ready` en 200 con `X-Request-Id`; ruta inexistente en 404; `healthcheck` sale con 0; sin `PORT` ni `DATABASE_URL` no arranca y nombra las dos |
| `docker build .` (Dockerfile de producción sin cambios) | Imagen de 43 MB; su `migrate` corre contra la base local con logs JSON |

## Desviaciones

- **`db/sqlc.yaml` llega con el plan 01.** sqlc necesita al menos un paquete con consultas; en este plan no hay ninguna.
- **`db/migrations/README.txt`.** `go:embed` falla con una carpeta vacía, y `.dockerignore` excluye `*.md`; un `.txt` mantiene la carpeta embebible hasta la primera migración (goose solo lee `.sql`).
- **`migrate` asegura la tabla `goose_db_version` antes de aplicar**, aunque no haya migraciones: queda creada desde el primer despliegue.
- **Dos códigos de error más** que los de la lista: `METHOD_NOT_ALLOWED` (405) y `PAYLOAD_TOO_LARGE` (413).
- **El log de acceso tampoco registra `/health/ready`**, además de `/health`.
- **`TEST_DATABASE_URL`** es una variable solo local (pruebas de integración); está en `.env.example` y nunca va al servidor.
- **`go test -race` no corre en esta máquina**: necesita cgo y un compilador de C. El workflow tampoco lo usa.
- **`time/tzdata` embebido en el binario**: "hoy" por zona horaria no depende de que el sistema tenga la base de zonas (la imagen la tiene igual).
