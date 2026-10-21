# Guía para el agente que implementa Orbita API

Este archivo es tu punto de partida. Léelo completo antes de escribir una línea. Las reglas de ramas están en [CLAUDE.md](CLAUDE.md) y **tienen prioridad** sobre cualquier otra cosa.

**Qué vas a construir:** el backend real (Go + Gin) de la app Android `atm-orbita-android`. Hoy la app funciona entera con datos de ejemplo en memoria (modo demo); tu trabajo es darle funcionalidad real creando la API y conectando la app a ella. Más adelante habrá un proyecto web y otros módulos que no tienen nada que ver con Orbita, por eso **la API se construye como módulos completamente independientes**.

El repo trae lo mínimo: un servidor Gin con `/api/v1/health`, el `Dockerfile`, el compose de producción y los workflows de GitHub ya listos y probados (sección 6). Todo lo demás —módulos, configuración, base de datos— lo creas tú sobre esa base.

---

## 0. Cómo trabajar (reglas de proceso)

1. **Una cosa a la vez.** Nunca implementes varios planes de golpe. Cuando termines un plan: corre sus pruebas, resume qué quedó hecho y **pide revisión al usuario** antes de pasar al siguiente.
2. **Si algo es ambiguo, pregunta.** No inventes reglas de negocio. Lo que ya está decidido está en la sección 11.
3. **Ramas:** todo el trabajo va en `develop`. Nunca hagas push a `main` ni a `production`, y nunca pases `develop` a `production`. Flujo único: `develop` → `main` → `production`. Pasar a `main`/`production` lo decide el usuario.
4. **Commits:** Conventional Commits en inglés (`feat(auth): add refresh token rotation`). Commits pequeños, uno por cambio lógico.
5. **No tienes acceso al servidor de producción ni lo necesitas.** Todo lo que el servidor requiere ya está preparado (sección 6). Lo único que dependerá del usuario es subir el `.env` y crear la base de datos, y eso se coordina como se explica en 6.3.
6. **Migraciones siempre** (ver 7.1): ningún cambio de esquema se hace fuera de una migración versionada.
7. **Secretos:** nunca los escribas en el repo, en commits ni en logs. Solo nombres de variables en `.env.example`; los valores los llena el usuario.
8. Al terminar cada plan, actualiza los documentos afectados **en el mismo cambio** (el plan, `docs/`, y `ORBITA_SPEC.md` de Android si cambió una pantalla, regla o flujo).

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
| Migraciones | **goose**, SQL puro, versionadas, con tabla de control en la base (ver 7.1) |
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

Grupos: `APP_ENV`, `PORT`, `DATABASE_URL`, `JWT_ACCESS_SECRET`, `ACCESS_TOKEN_TTL_SECONDS`, `REFRESH_TOKEN_TTL_DAYS`, `CORS_ORIGIN`, límites de rate limit, `RESEND_API_KEY`, `MAIL_FROM`, `R2_ENDPOINT`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET`.

---

## 6. Despliegue

**Regla:** el servidor **nunca compila**. GitHub construye la imagen Docker y el VPS solo la descarga. Todo esto **ya está hecho y probado**; no lo rediseñes, solo manténlo al día si cambias algo (por ejemplo la versión de Go).

### 6.1 Cómo se despliega
Para desplegar, el usuario promueve `main` a la rama `production`; al hacer push a `production` se activa `.github/workflows/deploy.yml`, con tres jobs en cadena:

1. **check**: verifica que el commit **ya está en `main`** (hace cumplir `develop → main → production`), y corre `go vet`, `golangci-lint`, `go test ./...` y `go build ./...`.
2. **build**: construye la imagen con Docker Buildx **en GitHub** y la sube a GHCR como `ghcr.io/atmosferasoltec/atm-orbita-api` con tags `latest` y `sha-<commit>`.
3. **deploy**: por SSH copia `deploy/docker-compose.yml` al servidor, hace `docker compose pull` y `up -d` con `IMAGE_TAG=sha-<commit>`, y espera a que el contenedor `orbita-api` esté `healthy` (si no lo está en 120 s, imprime logs y falla).


No hay workflow en `develop` ni `main`: el check solo corre al desplegar. Por eso, **antes de pedir que se promueva a `production`, corre en local lo mismo que el check** (`go vet ./...`, `golangci-lint run`, `go test ./...`, `go build ./...`); un error de lint tumba el despliegue.

Los secrets del repo (`SSH_HOST`, `SSH_USER`, `SSH_KEY`) ya están configurados.

### 6.2 Piezas ya creadas (qué hace cada una)
- **`Dockerfile`**: multi-stage. Etapa `builder` (compila un binario estático con `CGO_ENABLED=0`) y etapa final `alpine` con usuario no root. El `HEALTHCHECK` ejecuta `/app/api healthcheck` porque la imagen final no trae `curl`.
- **Una sola imagen**: el mismo binario tiene los subcomandos `migrate` y `healthcheck` (`cmd/api/main.go`). Hoy `migrate` no hace nada (no hay migraciones); **tu tarea es conectarlo a goose** con las migraciones embebidas (`//go:embed`) cuando exista la primera.
- **`deploy/docker-compose.yml`**: se copia al servidor en cada despliegue. Servicio `migrate` (misma imagen, comando `migrate`, termina) y servicio `app` (`depends_on: migrate: service_completed_successfully`, `env_file: .env`, healthcheck, `no-new-privileges`, 512 MB de memoria, `restart: unless-stopped`). Si una migración falla, la app nueva **no arranca** y la anterior sigue sirviendo.
- **Red y puerto**: el contenedor se une a la red externa `atm-network` (donde vive el Postgres). Se publica solo en loopback, `127.0.0.1:3071` → `3001` del contenedor. Nginx del servidor ya publica `https://orbita-api.atmosferast.com` hacia ese puerto.
- **Dominio**: `https://orbita-api.atmosferast.com` (HTTPS ya configurado en el servidor).

### 6.3 El `.env` de producción y la base de datos (ya preparados en el servidor)
El `.env` real vive **solo** en el servidor (`/opt/orbita-api/.env`); nunca en el repo. **El usuario ya creó en el servidor** la base `orbita`, el rol `orbita_app`, el esquema `orbita` (con `scripts/provision-db.sql`) y un `.env` inicial con estas variables ya con valor:

`APP_ENV`, `PORT`, `DATABASE_URL`, `JWT_ACCESS_SECRET`, `ACCESS_TOKEN_TTL_SECONDS` (900), `REFRESH_TOKEN_TTL_DAYS` (30), `CORS_ORIGIN` (`https://orbita.atmosferast.com`, el dominio de la futura web), `RATE_LIMIT_PER_MINUTE`, `LOGIN_RATE_LIMIT_PER_MINUTE`, `RESEND_API_KEY`, `MAIL_FROM`, `R2_ENDPOINT`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` y `R2_BUCKET`.

No conoces sus valores y no los necesitas: tu código solo lee esos nombres. Con eso, un despliegue del esqueleto actual funciona tal cual.

**Cada vez que tu código lea una variable nueva** (las de arriba ya existen; cualquier otra es nueva):
1. Agrégala a `.env.example` con un comentario de qué es y qué valor lleva en producción.
2. Haz que la validación de arranque falle con un mensaje claro si falta una obligatoria (así un `.env` incompleto se nota en el deploy, no en producción).
3. **Antes de pedir que se promueva a `production`**, entrégale al usuario la lista exacta de variables **nuevas** que debe agregar al `.env` del servidor (cuáles obligatorias, cuáles opcionales, y cuáles debe generar, por ejemplo con `openssl rand -base64 48`). Él las sube y te avisa; recién entonces se promueve.

**Rollback:** relanzar el workflow (`workflow_dispatch`) sobre un commit anterior, o que el usuario ejecute en el VPS `IMAGE_TAG=sha-<anterior> docker compose up -d`. Cada despliegue deja su imagen etiquetada con el SHA justamente para esto.

---

## 7. Base de datos

- La base es **PostgreSQL**, y ya corre en un **contenedor Docker** en el servidor de producción (nombre del contenedor: `atm-postgres`), compartido con otros proyectos del usuario. **Este repo no levanta ni administra ese Postgres.**
- Cada proyecto tiene su propio rol, su propia base y su propio **esquema** (no `public`). Orbita: rol `orbita_app`, base `orbita`, esquema `orbita`. **El usuario las crea en el servidor** (ver 6.3); tú entregas el script.
- Entrega `scripts/provision-db.sql` (parametrizado con la contraseña como variable de psql): crea el rol sin `CREATEDB`, la base con `OWNER orbita_app`, revoca el acceso a `PUBLIC`, crea el esquema `orbita`, quita permisos sobre `public` y fija el `search_path` del rol al esquema `orbita`. Se ejecuta como superusuario: `docker exec -i atm-postgres psql -v ON_ERROR_STOP=1 -U postgres -v app_password="'<clave>'" -d postgres < scripts/provision-db.sql`.
- **Desarrollo local:** como no tienes ese contenedor, entrega un `docker-compose.dev.yml` en el repo con un Postgres de desarrollo (base `orbita`, mismo rol y esquema, puerto local `5432`) para trabajar. Es solo para tu máquina; nunca se despliega.
- `DATABASE_URL`: en producción el host es el nombre del contenedor, **no** `localhost`: `postgresql://orbita_app:PASS@atm-postgres:5432/orbita?search_path=orbita`. En local, `localhost`.
- Tablas `snake_case` plural, columnas `snake_case`, UUID v7, `TIMESTAMPTZ`, `NUMERIC(14,2)` para dinero, `VARCHAR(n)` con tamaño. Casi nada se borra: `is_active`/`archived_at` para catálogos.
### 7.1 Migraciones (regla principal del proyecto)
El usuario trabaja **siempre con migraciones**, como Prisma: el esquema evoluciona por pasos versionados y nada se altera a mano ni se recrea.
- **goose** guarda en la base una tabla de control (`goose_db_version`, dentro del esquema `orbita`) con las migraciones ya aplicadas. En cada despliegue el servicio `migrate` aplica solo las pendientes, en orden.
- Un archivo por cambio en `db/migrations/`, numerado y con nombre descriptivo que incluya el módulo (`00003_ledger_add_entries_table.sql`), con sus secciones `-- +goose Up` y `-- +goose Down`.
- **Una migración aplicada nunca se edita.** Si hay que corregir o cambiar tablas existentes (añadir, renombrar o rehacer columnas), se crea una migración **nueva**.
- Cambios grandes (varias tablas nuevas + modificar las existentes) se parten en migraciones pequeñas y ordenadas, cada una revisable.
- Deben ser **seguras con la versión anterior aún corriendo**: primero agregar (columna nullable o con valor por defecto, backfill) y solo después quitar o endurecer en una migración posterior.
- Antes de aplicar en producción, probarlas en local sobre una base con datos de ejemplo y probar también el `Down`.
- El servicio `migrate` del compose ya bloquea el despliegue si una migración falla: la versión anterior sigue sirviendo.
- Nunca se usa auto-migración del ORM ni se ejecuta SQL de esquema a mano en producción.

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

### Paso 1 · Pipeline (ya hecho)
El pipeline, el `Dockerfile` y el compose ya están listos (sección 6). Lo único pendiente de tu parte aquí: construye sobre la base (`cmd/api/main.go` es solo un esqueleto mínimo; reemplázalo por la estructura de la sección 3). **No dispares un despliegue**: eso lo decide el usuario al promover a `production`.

### Paso 2 · Revisar TODO el proyecto Android
Antes de planificar, lee el proyecto `atm-orbita-android` completo: no es opcional ni se resume.
- `docs/CLAUDE.md` y `docs/01` … `docs/06` y `docs/ORBITA_SPEC.md`.
- El código: capas `domain`, `data/demo`, `ui`, los ViewModels, los modelos, las reglas de dinero y monedas, las 8+ pantallas y cada flujo (login, registro, recuperar contraseña, inicio, movimientos, cuentas, categorías, tarjetas de crédito, ajustes, eliminar cuenta, etc.).
- Entiende **cómo funcionan las maquetas y qué hace cada funcionalidad**, qué datos necesita cada pantalla y qué reglas aplica `data/demo` (esas reglas son la especificación que la API debe reproducir de verdad).
- El proyecto Android es `atm-orbita-android`. **Si no lo tienes en tu máquina, pídeselo al usuario antes de planificar**: sin él no se puede hacer este paso. Si algo de sus documentos contradice al código, manda el código; anota la diferencia.

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
3. Conecta la parte correspondiente de Android: cliente HTTP, repositorio `data/remote` y el cambio de línea en `di/AppModule.kt` (ver `docs/04-guia-api.md` de ese proyecto). Sigue las convenciones de ese proyecto (su `docs/CLAUDE.md`).
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
