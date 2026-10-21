# Guía para el agente que implementa Orbita API

Este archivo es tu punto de partida. Léelo completo antes de escribir una línea. Las reglas de ramas están en [CLAUDE.md](CLAUDE.md) y **tienen prioridad** sobre cualquier otra cosa.

**Qué vas a construir:** el backend real (Go + Gin) de la app Android `atm-orbita-android`. Hoy la app funciona entera con datos de ejemplo en memoria (modo demo); tu trabajo es darle funcionalidad real creando la API y conectando la app a ella. Más adelante habrá un proyecto web y otros módulos que no tienen nada que ver con Orbita, por eso **la API se construye como módulos completamente independientes**.

El repo está vacío a propósito: solo existen `go.mod`, `go.sum`, `CLAUDE.md` y esta guía. Todo lo demás lo creas tú.

---

## 0. Cómo trabajar (reglas de proceso)

1. **Una cosa a la vez.** Nunca implementes varios planes de golpe. Cuando termines un plan: corre sus pruebas, resume qué quedó hecho y **pide revisión al usuario** antes de pasar al siguiente.
2. **Si algo es ambiguo, pregunta.** No inventes reglas de negocio. Lo que ya está decidido está en la sección 11.
3. **Ramas:** todo el trabajo va en `develop`. Nunca hagas push a `main` ni a `production`, y nunca pases `develop` a `production`. Flujo único: `develop` → `main` → `production`. Pasar a `main`/`production` lo decide el usuario.
4. **Commits:** Conventional Commits en inglés (`feat(auth): add refresh token rotation`). Commits pequeños, uno por cambio lógico.
5. **Servidor de producción:** puedes entrar con `ssh atmosfera` **solo para leer** (`docker ps`, ver compose, ver redes). No crees, borres ni modifiques nada allí sin pedir permiso explícito al usuario para esa acción concreta.
6. **Secretos:** nunca los escribas en el repo, en commits ni en logs. Solo nombres de variables en `.env.example`; los valores los llena el usuario.
7. Al terminar cada plan, actualiza los documentos afectados **en el mismo cambio** (el plan, `docs/`, y `ORBITA_SPEC.md` de Android si cambió una pantalla, regla o flujo).

---

## 1. Idioma

- **Todo el código en inglés**: paquetes, archivos, carpetas, structs, interfaces, funciones, variables, constantes, tablas, columnas, endpoints, códigos de error, ramas, commits, nombres de tests.
- **Solo es español lo que ve el usuario final**: el `message` de los errores de la API, los correos, los PDFs y las pantallas de la app.
- Comentarios en español explicando el **porqué**, no el qué. Poco comentario; que el nombre diga lo que hace.
- Sin abreviaturas inventadas (`quantity`, no `qty`). Se permiten `id`, `url`, `db`, `api`.

---

## 2. Stack

| Capa | Decisión |
|---|---|
| Lenguaje | Go (la versión de `go.mod`) |
| HTTP | **Gin** |
| PostgreSQL | **pgx v5** (`pgxpool`) + **sqlc** (ver 2.1) |
| Migraciones | **goose**, SQL puro, versionadas |
| Validación | `go-playground/validator` (ya viene con Gin) para forma; reglas de negocio en el dominio |
| Auth | JWT HS256 (`golang-jwt/jwt/v5`) + refresh token opaco; contraseñas con **argon2id** (`golang.org/x/crypto/argon2`) |
| Config | Variables de entorno validadas al arrancar (la app **no arranca** si falta una obligatoria) |
| Logs | `log/slog` en JSON, con `request_id` en cada línea |
| IDs | UUID v7 |
| Correo | Resend + plantillas MJML (ver 8) |
| Archivos | Cloudflare R2 por API S3 (ver 9) |
| Tests | `testing` + `testify`; integración contra Postgres real |
| Lint | `golangci-lint` + `gofmt` |

### 2.1 ¿Por qué sqlc + pgx?
Se eligió **sqlc sobre pgx** (no GORM ni otro ORM): escribes SQL real, sqlc genera código Go tipado y verificado en compilación, sin reflexión ni magia, con el mejor rendimiento y consultas de reportes fáciles de optimizar. Las consultas viven dentro del módulo dueño de la tabla.

Reglas: dinero en `NUMERIC(14,2)` mapeado a un tipo decimal (nunca `float`); fechas de movimiento en `DATE`; marcas de tiempo en `TIMESTAMPTZ` siempre UTC; transacciones con `pgx.Tx` envueltas en un helper del módulo `platform`, nunca abiertas a mano en los handlers.

---

## 3. Arquitectura: monolito modular + hexagonal

Un solo binario y un solo despliegue, pero **cada módulo es independiente** como si fuera un servicio: se puede entender, probar y cambiar sin tocar los demás. Aplica Clean Architecture y SOLID.

```
cmd/
└── api/main.go                  único punto de entrada: carga config, arma módulos, arranca Gin
                                 (subcomandos: `migrate`, `healthcheck`)
internal/
├── platform/                    infraestructura compartida y SIN reglas de negocio
│   ├── config/  database/  httpserver/  logger/  clock/  ids/
│   ├── middleware/              request id, recover, logging, CORS, rate limit
│   ├── apierror/                formato único de errores y respuestas
│   └── tx/                      helper de transacciones
├── integrations/                servicios externos detrás de una interfaz (puerto)
│   ├── mail/                    puerto + resend + console (dev)
│   └── storage/                 puerto + r2 + local (dev)
└── modules/
    └── <module>/                ej. identity, ledger, credit, reports, fx
        ├── domain/              entidades, value objects, reglas. Go puro, sin imports de gin/pgx
        ├── app/                 casos de uso (services) + puertos que necesitan (interfaces)
        ├── adapters/
        │   ├── http/            handlers Gin, DTOs, rutas; traduce HTTP ⇄ casos de uso
        │   └── postgres/        implementa los puertos; sqlc/queries aquí
        ├── module.go            constructor del módulo: recibe dependencias, expone rutas y API pública
        └── README.md            qué hace el módulo, sus puertos y sus errores
db/
├── migrations/                  goose, un solo historial ordenado, nombre con el módulo
└── sqlc.yaml                    una configuración por módulo, generando dentro del módulo
docs/
└── plans/                       ver sección 10
```

**Reglas de dependencia (se hacen cumplir, no se negocian):**

1. `domain` no importa nada del proyecto salvo otros `domain` del mismo módulo, ni gin, pgx o net/http.
2. `app` depende de `domain` y de **interfaces** que ella misma declara; nunca de adapters.
3. `adapters` dependen de `app`/`domain`, nunca al revés. La inyección de dependencias se hace a mano en `module.go` y `main.go` (sin frameworks de DI).
4. **Un módulo nunca importa los paquetes internos de otro.** Si `reports` necesita algo de `ledger`, `ledger` expone una interfaz pública pequeña (en su `module.go` o un paquete `api/`) y `reports` depende de esa interfaz. Verifica esto con una regla del linter (`depguard`) o un test de arquitectura.
5. **Cada módulo es dueño de sus tablas.** No se hacen `JOIN` ni consultas a tablas de otro módulo; se pide por su interfaz. Las claves foráneas entre módulos se limitan a lo indispensable (por ejemplo `user_id`).
6. Interfaces pequeñas, definidas donde se consumen. Sin paquetes `utils`, `helpers` ni `common` gigantes.
7. Los handlers son delgados: parsear, llamar al caso de uso, responder. Cero reglas de negocio ahí.
8. Contexto (`context.Context`) como primer parámetro en todo lo que toque BD o red. Errores envueltos con `%w`; el dominio define errores tipados y el adapter HTTP los traduce a códigos.

**Aislamiento y resiliencia (que si algo se rompe no se rompa para todos):**
- Middleware `recover` por petición: un `panic` en un handler devuelve 500 y se registra; no tumba el proceso.
- Timeouts en servidor HTTP, consultas y llamadas externas; cierre ordenado (`graceful shutdown`).
- Integraciones externas (correo, R2, tipo de cambio) detrás de puertos y **nunca bloquean** la ruta crítica: si Resend falla, el registro igual se completa y el correo se reintenta.
- `GET /api/v1/health` (liveness) y `GET /api/v1/health/ready` (comprueba la BD).
- Desplegar una versión mala se revierte en minutos relanzando el tag anterior (ver sección 6).
- Cada módulo se puede apagar por configuración si algún día hace falta (`MODULES_ENABLED`), sin tocar el resto.

---

## 4. Contrato de la API

- Prefijo `/api/v1`. JSON, `snake_case` en los campos.
- Éxito: `{ "data": ... }`; listas paginadas: `{ "data": [...], "meta": { "page", "page_size", "total" } }`.
- Error: `{ "error": { "code": "CATEGORY_NAME_TAKEN", "message": "Ya existe una categoría con ese nombre.", "details": ... } }`. El `code` es estable, en inglés y en `SCREAMING_SNAKE_CASE` (la app elige su texto a partir de él); el `message` va en español como respaldo.
- **Dinero viaja como texto** (`"1245.80"`), nunca como número JSON.
- Los IDs los genera el cliente (UUID) y la API los acepta al crear, para que reintentar no duplique.
- **Cada persona ve solo sus datos**: el `user_id` sale del token, jamás del cuerpo ni de la URL.
- "Hoy" lo calcula la API con la zona horaria IANA del perfil.
- Documenta el contrato con OpenAPI (archivo `docs/openapi.yaml`) y mantenlo al día.

Autenticación (detalles en su plan, con las decisiones de la sección 11):
- **Access token JWT:** vida corta, **15 minutos** por defecto (configurable). Aunque se pensó en un día, un token robado dura mucho; con renovación automática la app no lo nota.
- **Refresh token:** opaco y aleatorio, guardado **hasheado** en BD, vida **30 días**, **rotativo** (cambia en cada uso) con detección de reutilización: si se reutiliza uno viejo, se revoca toda esa sesión. Configurable por variable.
- Limitar intentos de login y de recuperación por IP y por cuenta.
- Recuperar contraseña y eliminar cuenta son requisitos para publicar la app.

---

## 5. Variables de entorno

Los **nombres** están en `.env.example` (sin valores). El usuario llena el `.env`. Cada variable nueva se agrega a `.env.example` con un comentario, y a la validación de arranque. El `.env` real nunca se sube a git.

Grupos: `APP_ENV`, `PORT`, `DATABASE_URL`, `JWT_ACCESS_SECRET`, `ACCESS_TOKEN_TTL_SECONDS`, `REFRESH_TOKEN_TTL_DAYS`, `CORS_ORIGIN`, límites de rate limit, `RESEND_API_KEY`, `MAIL_FROM`, `R2_*`, `MODULES_ENABLED`.

---

## 6. Despliegue

**Regla:** el servidor **nunca compila**. GitHub construye la imagen Docker y el VPS solo la descarga.

### 6.1 Lo primero que haces: el workflow
Antes de escribir funcionalidad, deja funcionando el pipeline con un `/health` mínimo. Referencia: `../atm-iris-api/.github/workflows/` y `../atm-iris-api/deploy/docker-compose.yml`, y los otros de `../lavendimia-api` y `../atm-hotel-api`. **Cópiate la forma, no el contenido**: esos son NestJS/Node; este es Go.

Dos archivos en `.github/workflows/`:

1. **`ci.yml`** — en push y PR a `develop` y `main`: solo el job `check`.
2. **`deploy.yml`** — en push a `production` (más `workflow_dispatch`), con `concurrency` para no solapar despliegues, tres jobs en cadena:
   - **check**: `go vet`, `golangci-lint`, `go test ./...`, `go build ./...`. Además verifica que el commit desplegado **ya está en `main`** (`git merge-base --is-ancestor`), para hacer cumplir `develop → main → production`.
   - **build**: construye la imagen con Docker Buildx **en GitHub** y la sube a GHCR: `ghcr.io/atmosferasoltec/atm-orbita-api` con tags `latest` y `sha-<commit>`. Caché de GitHub Actions.
   - **deploy**: por SSH (con los reintentos y `ControlMaster` del workflow de Iris) copia `deploy/docker-compose.yml` a `/opt/orbita-api/`, hace `docker compose pull` y `up -d` con `IMAGE_TAG=sha-<commit>`, y espera a que el contenedor esté `healthy`; si no lo está en 120 s, imprime logs y falla.

Secrets del repo **ya creados**: `SSH_HOST`, `SSH_USER`, `SSH_KEY`. El login a GHCR vive en el servidor (`~/.docker/config.json`).

### 6.2 Docker
- `Dockerfile` multi-stage: etapa `builder` (`golang`, compila binario estático con `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w"`), etapa final mínima (`alpine` o `distroless`) con usuario **no root**.
- **Una sola imagen**: el binario trae las migraciones embebidas (`//go:embed`) y los subcomandos `migrate` y `healthcheck`. No hace falta imagen "migrate" aparte como en Nest.
- Las plantillas MJML se compilan a HTML **durante el build de la imagen** (etapa Node), y el HTML resultante se embebe; el runtime no lleva Node.
- `deploy/docker-compose.yml` (se copia al servidor en cada despliegue, no se edita a mano allí):
  - Red externa **`atm-network`** (ahí vive `atm-postgres`).
  - Servicio `migrate`: misma imagen, comando `migrate`, `restart: "no"`.
  - Servicio `app` con `depends_on: migrate: condition: service_completed_successfully`, `env_file: .env`, healthcheck (`/app/api healthcheck`), `no-new-privileges`, límite de memoria, `restart: unless-stopped`.
  - Puerto solo en loopback: **`127.0.0.1:3071:3001`**. El `3061` que usaba el plan viejo de Nest ya lo ocupa `hotel-api`; puertos ocupados hoy: 3010, 3011, 3020, 3021, 3030, 3040, 3041, 3050, 3051, 3060, 3061. Verifica con `ssh atmosfera docker ps` antes de fijarlo.
  - Nombre de contenedor: `orbita-api`.
- `.env` real solo en `/opt/orbita-api/.env` del servidor.
- **Rollback:** relanzar el workflow (`workflow_dispatch`) sobre un commit anterior, o en el VPS `IMAGE_TAG=sha-<anterior> docker compose up -d`. Cada despliegue deja su imagen etiquetada con el SHA justamente para esto.

### 6.3 Cosas del servidor que NO hace el agente (las hace el usuario)
Crear `/opt/orbita-api` (propietario `deploy`) y su `.env`; crear la base `orbita`, rol `orbita_app` y esquema `orbita` en `atm-postgres` (ver 7); el vhost de nginx y el certificado para el dominio de la API. **Prepara los scripts y las instrucciones exactas**, pero no los ejecutes en producción sin autorización.

---

## 7. Base de datos

- Ya existe un contenedor **PostgreSQL compartido** (`atm-postgres` en el VPS, `atmosfera-postgres` en local en `../atmosfera-postgres`). **Este repo no levanta Postgres.** Verifica la versión con `ssh atmosfera docker ps` (hoy `postgres:17-alpine`).
- Cada proyecto tiene su propio rol, su propia base y su propio **esquema** (no `public`). Orbita: rol `orbita_app`, base `orbita`, esquema `orbita`. En el servidor aún no existen.
- Entrega `scripts/provision-db.sql` siguiendo el patrón de `../forma-de-trabajo.md` sección 3.2 (rol sin `CREATEDB`, `REVOKE` sobre `public`, `search_path` del rol al esquema).
- Conexión en producción por nombre de contenedor: `postgresql://orbita_app:PASS@atm-postgres:5432/orbita` (el host **no** es `localhost`); en local, `localhost:5432`.
- Tablas `snake_case` plural, columnas `snake_case`, UUID v7, `TIMESTAMPTZ`, `NUMERIC(14,2)` para dinero, `VARCHAR(n)` con tamaño. Casi nada se borra: `is_active`/`archived_at` para catálogos.
- **Solo migraciones versionadas** (goose); el esquema jamás se toca a mano. Las migraciones deben ser seguras de aplicar con la versión anterior aún corriendo (agregar antes que quitar).

---

## 8. Correo

- **Resend** detrás de un puerto `Mailer` en `internal/integrations/mail` (hay una implementación `console` para desarrollo que escribe el correo en el log). En producción las variables son obligatorias.
- Plantillas escritas en **MJML**, compiladas a HTML en build, con `html/template` para los datos. Todo el texto en español (es-PE).
- Correos mínimos: recuperar contraseña (con enlace de un solo uso y caducidad corta). Verificación de correo queda lista pero desactivada por configuración.
- Envío asíncrono con reintentos; nunca dentro de la transacción del caso de uso.

## 9. Almacenamiento (Cloudflare R2)

- Puerto `Storage` en `internal/integrations/storage`; implementación R2 con `aws-sdk-go-v2/service/s3` apuntando al endpoint de R2 y una implementación local para desarrollo.
- Los archivos nunca pasan por el repositorio; se entregan por URL firmada de corta duración.
- No se usa hoy para nada de Orbita; créalo solo cuando un plan lo necesite.

---

## 10. Tu trabajo, en este orden

### Paso 1 · Pipeline
Crea `.gitignore` (ya hay uno mínimo), estructura base, `/health`, `Dockerfile`, `deploy/docker-compose.yml`, `ci.yml` y `deploy.yml` (sección 6). Valida `ci.yml` en `develop`. **No dispares un despliegue**: eso lo decide el usuario al promover a `production`.

### Paso 2 · Revisar TODO el proyecto Android
Antes de planificar, lee `../atm-orbita-android` completo: no es opcional ni se resume.
- `docs/CLAUDE.md` y `docs/01` … `docs/06` y `docs/ORBITA_SPEC.md`.
- El código: capas `domain`, `data/demo`, `ui`, los ViewModels, los modelos, las reglas de dinero y monedas, las 8+ pantallas y cada flujo (login, registro, recuperar contraseña, inicio, movimientos, cuentas, categorías, tarjetas de crédito, ajustes, eliminar cuenta, etc.).
- Entiende **cómo funcionan las maquetas y qué hace cada funcionalidad**, qué datos necesita cada pantalla y qué reglas aplica `data/demo` (esas reglas son la especificación que la API debe reproducir de verdad).
- Material de apoyo: `../atm-orbita-api-nest/docs/plans/` contiene 17 planes de una versión anterior en NestJS (contexto, modelo de datos, fases, seguridad, decisiones D1–D18). **Úsalos como referencia de decisiones de negocio**, no como verdad: el stack cambió (Go/sqlc en lugar de Nest/Prisma) y el código de Android manda si hay contradicción. Anota cualquier diferencia que encuentres.

### Paso 3 · Escribir los planes en `docs/plans/`
Crea la carpeta `docs/plans/` y, **un archivo por plan**, numerados y separados por funcionalidad, por ejemplo:

```
docs/plans/
├── README.md                   índice, orden, dependencias, estado de cada plan
├── 00-foundations.md           (el pipeline y la base ya hechos en el Paso 1)
├── 01-identity-login.md
├── 02-identity-register.md
├── 03-identity-forgot-password.md
├── 04-settings-profile.md
├── 05-accounts.md
├── 06-categories.md
├── 07-entries.md               ingresos, egresos, transferencias
├── 08-home-dashboard.md
├── 09-reports.md
├── 10-credit-cards.md
├── 11-android-integration.md
└── ...
```
Ese listado es solo una guía: la división real sale de tu revisión de Android. Cada plan debe contener: **objetivo**, **pantallas de Android que cubre**, **módulo(s) de la API**, **endpoints** (método, ruta, request, response, errores), **tablas y migraciones**, **reglas de negocio**, **dependencias** de otros planes, **pasos de implementación**, **pruebas**, **criterios de aceptación** y, al implementarlo, las **desviaciones con su motivo**.

Un plan = una funcionalidad que se puede entregar, probar y revisar sola. Si es demasiado grande, pártelo.

**Detente al terminar los planes y pide revisión al usuario.** No empieces a implementar hasta que los apruebe.

### Paso 4 · Implementar, uno por uno
Con la aprobación del usuario, ejecuta los planes **en orden y de a uno**. Por cada plan:
1. Implementa el módulo de la API según el plan, respetando la sección 3.
2. Pruebas unitarias del dominio y de los casos de uso (con puertos falsos) + pruebas de integración de los adapters de Postgres y de los endpoints.
3. Conecta la parte correspondiente de Android: cliente HTTP, repositorio `data/remote` y el cambio de línea en `di/AppModule.kt` (ver `../atm-orbita-android/docs/04-guia-api.md`). Sigue las convenciones de ese proyecto (su `docs/CLAUDE.md`).
4. Actualiza plan, OpenAPI y `ORBITA_SPEC.md`.
5. Resume, **pide revisión** y espera antes del siguiente.

---

## 11. Decisiones ya tomadas (no las reabras sin preguntar)

- Backend: este API en Go/Gin; **no** se usa Supabase ni backend gestionado. Autenticación propia con correo y contraseña (Google, después).
- La moneda principal se elige al registrarse (por defecto soles, PEN); la cuenta "Efectivo" nace en esa moneda. La secundaria es dólares, o soles si la principal ya es dólares.
- El tipo de cambio puede no existir (ausencia de fila, no una columna nula); sin él no se crean cuentas ni se opera en otra moneda.
- Nombres de categoría únicos por usuario y tipo, sin distinguir mayúsculas ni tildes (`name_normalized`, la `ñ` se conserva); se muestra el nombre original; conflicto → `409 CATEGORY_NAME_TAKEN`. Una categoría archivada libera su nombre.
- Compras con tarjeta de crédito **no son movimientos**: quedan pendientes hasta pagarse. Pagar crea el egreso y marca la compra en una sola transacción. Eliminar ese egreso devuelve la compra a pendiente. Ese egreso solo permite editar cuenta, monto y fecha (`422 PAYMENT_EXPENSE_LOCKED`). Solo se editan y eliminan compras pendientes. No se archiva una tarjeta con compras pendientes (`409 CARD_HAS_PENDING_PURCHASES`).
- Las transferencias no cuentan como ingreso ni gasto. El saldo de una cuenta **no se guarda**: se calcula.
- La fecha de un movimiento es solo fecha (`DATE`). Reportes calculados en la base. Listas paginadas.
- Verificación de correo: no al lanzar (queda lista por configuración).
- El PDF de reportes, cuando llegue, contiene solo resumen (totales y rankings).
- Eliminar cuenta y recuperar contraseña son requisitos de salida a producción.
- Tipo de cambio automático diario y exportar a PDF: fases tardías; proveedor de tipo de cambio aún sin decidir (**pregunta al usuario** al llegar ahí).
- Dominio de correo: `atmosferast.com`, vía Resend.
