# 03 · Fase 0 — Fundaciones

**Objetivo:** dejar el proyecto listo para construir módulos de negocio sin volver a tocar la base: contenedores, base de datos, Prisma, configuración, errores, registros, salud, documentación y pipeline.

**Tamaño:** mediano. **Depende de:** nada. **Bloquea:** todas las demás fases.

## Estado actual (7 oct 2026)

Parte de esta fase **ya existe** en `main`: el commit `f76bb49` (Joel Maldonado) integró Prisma, Docker, integración continua y despliegue. Este plan se escribió sin conocerlo; esta sección lo reconcilia. **Donde el código y el resto de este documento difieren, vale lo que dice esta sección** hasta que se revise cada punto.

**Hecho**

| Pieza | Cómo quedó |
|---|---|
| Prisma | `prisma` y `@prisma/client` en `7.10.0`; `schema.prisma` con el generador `prisma-client` y salida en `src/generated/prisma`; configuración en `prisma7.config.ts` (Prisma la carga sola: comprobado) |
| `PrismaModule` global y `PrismaService` | Con el adaptador `@prisma/adapter-pg`, conexión al iniciar y cierre al terminar |
| `Dockerfile` | Multietapa, Node 24 alpine, pnpm 11.20.0, usuario sin privilegios |
| `docker-compose.yml` | API y `postgres:17-alpine` con volumen y `healthcheck` |
| Integración continua | `.github/workflows/ci.yml`: instalación con lockfile congelado, lint, pruebas y build, en cada push a `main` |
| Despliegue | `.github/workflows/deploy-production.yml`: al hacer push a `production`, construye la imagen, la publica en GHCR y despliega por SSH con `docker-compose.prod.yml` |
| Migraciones | `docker-entrypoint.sh` ejecuta `prisma migrate deploy` al arrancar el contenedor |
| `.env.example` | `PORT`, `DATABASE_URL` y, desde hoy, las variables `MAIL_*` de Resend |

**Corregido hoy**

- `@prisma/adapter-pg` estaba como `^7.10.0`. Quedó en `7.10.0` exacto en `package.json` y en `pnpm-lock.yaml`, como los otros dos paquetes de Prisma. Verificado con pnpm 11.20.0: `pnpm install --frozen-lockfile`, `lint`, `test` y `build` terminan bien.

**Decisiones que el código ya tomó, y que este plan adopta**

| Tema | El plan decía | El código tiene | Se adopta |
|---|---|---|---|
| Versión de PostgreSQL | 18 | 17 (`postgres:17-alpine`) | **17** |
| Gestor de paquetes | pnpm | pnpm 11.20.0, fijado en `packageManager` | **pnpm 11.20.0** |
| Nombres de los scripts | `db:migrate`, `db:deploy`… | `prisma:migrate:dev`, `prisma:migrate:deploy`, `prisma:generate`, `prisma:studio` | **Los del código** |
| Dónde corren las migraciones | Paso aparte, antes de arrancar | Al arrancar el contenedor | **Al arrancar**, mientras haya una sola réplica (ver abajo) |
| Despliegue | Por decidir | GitHub Actions → GHCR → SSH → Docker Compose, contra un PostgreSQL ya existente en el servidor | **El del código** |
| Ramas | — | `develop`, `main` y `production` en los flujos | El usuario decidió trabajar **solo en `main`**; `production` queda como disparador del despliegue |

**Observaciones sobre lo que ya existe** (tareas, no decisiones de producto)

| # | Observación | Por qué importa | Qué hacer |
|---|---|---|---|
| 1 | `docker-compose.prod.yml` apunta a la imagen `ghcr.io/joelmaldonado/atm-orbita-api`, pero el flujo de despliegue publica en `ghcr.io/<dueño del repositorio>/atm-orbita-api`, que es `atmosferasoltec` | El servidor descargaría una imagen que el flujo no actualiza | Alinear el nombre; el propio archivo avisa de que deben coincidir |
| 2 | `docker-compose.prod.yml` publica el puerto `3000` del API | El API quedaría accesible sin TLS ni proxy | Ponerlo detrás de un proxy inverso con HTTPS y no publicar el puerto ([13](13-escalabilidad-y-operacion.md)) |
| 3 | Las migraciones corren al arrancar, con el mismo usuario de base de datos que usa el API | El usuario del API necesita permisos para alterar el esquema, justo lo que el plan quería evitar; y con varias réplicas todas intentarían migrar a la vez | Aceptable con una réplica. Antes de escalar o de salir a producción con datos reales: paso de migración aparte y dos roles (sección 1) |
| 4 | `PrismaService` lee `process.env.DATABASE_URL` directamente, sin tamaño de grupo ni tiempos máximos | Una consulta lenta puede retener todas las conexiones | Pasar por la configuración validada y fijar los límites de la sección 2 |
| 5 | `docker-compose.yml` publica PostgreSQL en todas las interfaces con usuario y contraseña `postgres` | Solo es desarrollo, pero queda expuesto en la red local | Publicar en `127.0.0.1` |
| 6 | El flujo de integración continua no comprueba tipos, ni ejecuta las pruebas e2e, ni audita dependencias | Menos de lo que pide la sección 6 | Añadir los pasos al crecer el proyecto |
| 7 | El `Dockerfile` no tiene `HEALTHCHECK`, y el despliegue no espera a que el API responda | Un despliegue roto no se detecta | Añadirlos junto con el módulo `health` |
| 8 | La CLI de Prisma sugiere `npm i prisma@latest`, que hoy es una versión candidata de la serie 8 | Seguir el aviso rompería el pin | No actualizar; ver [01](01-arquitectura-y-convenciones.md) §2.1 |
| 9 | `.gitignore` excluye `/.claude`, `/.agents` y `/.windsurf` | Ninguno; solo conviene saberlo | — |

Sigue existiendo el controlador de ejemplo ("Hello World") y su prueba.

## Alcance

Entra: infraestructura local y de pruebas, piezas comunes de `src/common`, primera migración vacía de negocio, pipeline de integración continua.

No entra: ningún endpoint de negocio ni de autenticación.

## 1. Contenedores

**`compose.yaml` (desarrollo)**

| Servicio | Imagen | Detalle |
|---|---|---|
| `db` | `postgres:17-alpine` | Volumen con nombre, `healthcheck` con `pg_isready`, puerto publicado solo en `127.0.0.1` |
| `db-test` | `postgres:17-alpine` | Sin volumen (datos en `tmpfs`), para pruebas locales rápidas |
| `mailpit` | `axllent/mailpit` | Bandeja de correo local para la Fase 1b |
| `api` | construida del `Dockerfile` | Opcional en desarrollo; lo normal es `pnpm start:dev` contra `db` |

**`Dockerfile` (multietapa)**

1. `deps`: instala dependencias con `pnpm install --frozen-lockfile`.
2. `build`: genera el cliente de Prisma y compila con `nest build`.
3. `runtime`: imagen mínima con solo `dist`, `node_modules` de producción y `prisma/`. Usuario sin privilegios, `NODE_ENV=production`, `HEALTHCHECK` contra `/api/v1/health/live`.

**`compose.prod.yaml`** se detalla en [13](13-escalabilidad-y-operacion.md).

**Roles de base de datos** (script de inicialización del contenedor):

| Rol | Permisos | Quién lo usa |
|---|---|---|
| `orbita_migrator` | Dueño del esquema; crea y altera tablas | Solo el paso de migraciones |
| `orbita_app` | `SELECT`, `INSERT`, `UPDATE`, `DELETE` sobre las tablas; sin `CREATE`, `ALTER` ni `DROP` | El API en ejecución |

Si el API fuera comprometido, no podría alterar el esquema ni leer otras bases.

## 2. Prisma

- Instalar `prisma`, `@prisma/client` y `@prisma/adapter-pg` en la versión exacta **7.10.0**, más `pg`. La justificación está en [01](01-arquitectura-y-convenciones.md) §2.1.
  - Comando: `pnpm add --save-exact @prisma/client@7.10.0 @prisma/adapter-pg@7.10.0 pg` y `pnpm add --save-exact -D prisma@7.10.0`.
  - Comprobar que en `package.json` quedaron **sin `^`**. No usar `prisma@latest`: hoy apunta a `8.0.0-rc.21` (H17).
  - Repetir antes `npm view prisma dist-tags` y `npm view @prisma/client version`; si existe una `7.10.x` de corrección, usarla en los tres y actualizar la tabla de [01](01-arquitectura-y-convenciones.md).
- `class-validator@0.15.1` y `class-transformer@0.5.1`, también con `--save-exact`.
- `prisma/schema.prisma` con el generador `prisma-client` y salida en `src/generated/prisma` (carpeta ignorada por git y generada en `postinstall` y en el `Dockerfile`).
- `prisma.config.ts` con la URL de migraciones (`DATABASE_MIGRATE_URL`).
- `PrismaService`: crea el cliente con el adaptador y el tamaño de grupo de `DATABASE_POOL_MAX`, se conecta al iniciar y se cierra en el apagado.
- Parámetros de conexión del rol de aplicación: `statement_timeout = 5s`, `idle_in_transaction_session_timeout = 10s`, `lock_timeout = 3s`. Una consulta desbocada no puede retener el grupo de conexiones.
- Scripts en `package.json`: `db:migrate`, `db:deploy`, `db:reset`, `db:seed`, `db:studio`.

## 3. Arranque del API (`main.ts`)

- Prefijo global `api` y versionado por URI (`v1`).
- `ValidationPipe` global: `whitelist`, `forbidNonWhitelisted`, `transform`, y una fábrica de errores que produce el formato de [01](01-arquitectura-y-convenciones.md), sección 4.
- `helmet`, CORS con lista blanca (`CORS_ORIGINS`), límite de cuerpo de 100 KB, compresión.
- `trust proxy` según `TRUST_PROXY`, para que el límite de peticiones vea la IP real.
- `enableShutdownHooks()` y cierre ordenado: deja de aceptar peticiones, termina las que están en curso, cierra Prisma.
- Tiempo máximo por petición de 15 s.

## 4. Piezas comunes (`src/common`)

| Pieza | Qué hace |
|---|---|
| `config/` | Clase del entorno validada con `class-validator`; el proceso no arranca si algo falta |
| `errors/AppError` | Error de dominio con `code`, estado HTTP y `errors[]` opcional |
| `errors/ProblemFilter` | Filtro global: convierte `AppError`, errores de validación, errores conocidos de Prisma (`P2002` único, `P2003` clave foránea, `P2025` no encontrado) y cualquier otro en `problem+json`. Nunca expone trazas ni mensajes internos |
| `money/` | `Decimal` configurado, `round2`, `convert`, `percentOf`, analizador y validador `@IsMoney()` |
| `dates/` | Tipo `LocalDate` (`YYYY-MM-DD`), `todayFor(timezone)`, `Clock` inyectable, validador `@IsLocalDate()` |
| `pagination/` | Codificar y decodificar cursores opacos; DTO `limit` / `cursor` |
| `http/RequestIdMiddleware` | Propaga o genera `X-Request-Id` |
| `http/EtagInterceptor` | Calcula `ETag` y responde `304` |
| `auth/` | Decoradores `@Public()` y `@CurrentUser()`; el guard se completa en la Fase 1 |

El módulo `money` nace con sus pruebas: son los vectores obligatorios de `docs/06` de Android ([12](12-pruebas.md), sección 3).

## 5. Salud y documentación

| Endpoint | Público | Respuesta |
|---|---|---|
| `GET /api/v1/health/live` | Sí | `200` si el proceso responde |
| `GET /api/v1/health/ready` | Sí | `200` si la base responde a `SELECT 1`; `503` si no |
| `GET /api/docs` | Solo fuera de producción | Interfaz Swagger |
| `openapi.json` | Artefacto del pipeline | Contrato para Android, web e iOS |

Se elimina el controlador de ejemplo (`app.controller.ts`, `app.service.ts` y sus pruebas).

## 6. Integración continua

Un pipeline por cada *pull request* y por cada cambio en `main`:

1. `pnpm install --frozen-lockfile`
2. `pnpm lint` (oxlint) y comprobación de formato con Prettier
3. Comprobación de tipos (`tsc --noEmit`)
4. `prisma validate` y verificación de que no hay diferencias entre esquema y migraciones
5. `pnpm test` (unitarias, con cobertura)
6. `pnpm test:e2e` (con PostgreSQL real)
7. `pnpm build`
8. Construcción de la imagen Docker y análisis de vulnerabilidades
9. `pnpm audit` (falla con severidad alta o crítica)
10. Generación de `openapi.json` y comparación con la versión anterior para detectar cambios incompatibles

La rama `main` queda protegida: no se integra sin pipeline en verde y una revisión.

## 7. Tareas

Las marcadas ya están en `main` (ver "Estado actual").

- [x] `pnpm install` y verificar que la plantilla compila y sus pruebas pasan
- [x] `docker-compose.yml` con el API y PostgreSQL
- [ ] Añadir `db-test` y `mailpit` al `docker-compose.yml`; script de roles
- [x] `.env.example`
- [ ] Validación del entorno al arrancar
- [ ] Repetir `npm view` de Nest, Prisma y `class-validator` y confirmar que la tabla de [01](01-arquitectura-y-convenciones.md) §2.1 sigue vigente
- [x] Instalar Prisma `7.10.0` exacto en sus tres paquetes; `schema.prisma`, `prisma7.config.ts`, `PrismaModule`
- [ ] Comprobación en el pipeline: falla si `prisma`, `@prisma/client` y `@prisma/adapter-pg` no tienen la misma versión o llevan `^`
- [ ] Configurar tiempos máximos de sentencia en la conexión
- [ ] `main.ts`: prefijo, versión, validación, `helmet`, CORS, límite de cuerpo, apagado ordenado
- [ ] `common/errors` con el catálogo de códigos y el filtro global
- [ ] `common/money` con sus pruebas unitarias
- [ ] `common/dates` con `Clock` y sus pruebas (incluida la de medianoche UTC)
- [ ] `common/pagination`
- [ ] Registros con `nestjs-pino`, `requestId` y censura de campos sensibles
- [ ] Módulo `health` con `live` y `ready`
- [ ] Swagger y exportación de `openapi.json`
- [x] `Dockerfile` multietapa con usuario sin privilegios
- [ ] `HEALTHCHECK` en el `Dockerfile`
- [ ] Utilidades de pruebas e2e: arranque con Testcontainers, migraciones, limpieza entre pruebas
- [x] Pipeline de integración continua (lint, pruebas, build)
- [ ] Ampliar el pipeline: tipos, e2e, auditoría, contrato OpenAPI
- [x] Flujo de despliegue
- [ ] Resolver las observaciones 1 a 7 de "Estado actual"
- [ ] Borrar el código de ejemplo
- [x] `README.md` con cómo levantar el proyecto

## 8. Pruebas de la fase

- Unitarias de `money` y `dates` (todas las de [12](12-pruebas.md), sección 3).
- e2e: `health/live` responde `200`; `health/ready` responde `503` con la base apagada.
- e2e: una ruta inexistente devuelve `404` en `problem+json`, sin traza.
- e2e: un cuerpo de más de 100 KB devuelve `413`.
- Arranque: con una variable obligatoria ausente, el proceso termina con un mensaje claro.
- Prueba de humo del contenedor: la imagen construida arranca y responde a `live`.

## 9. Criterios de aceptación

- `docker compose up -d db && pnpm db:migrate && pnpm start:dev` deja el API respondiendo en una máquina limpia.
- El pipeline corre de principio a fin en verde.
- La imagen de producción corre como usuario sin privilegios y pesa lo razonable para una imagen mínima de Node.
- Ningún error devuelve una traza o un mensaje interno de Prisma.
- El rol `orbita_app` no puede ejecutar `DROP TABLE` (hay una prueba que lo intenta).

## 10. Riesgos

| Riesgo | Mitigación |
|---|---|
| NestJS 12, Prisma 7 y el modo ESM son recientes; puede haber fricción entre ellos | Hacer primero una prueba mínima: un módulo con Prisma y una prueba e2e con Testcontainers, antes del resto de tareas |
| El cliente generado de Prisma y la resolución `nodenext` (extensiones `.js` en las importaciones) | Verificar las opciones del generador en la documentación de la versión instalada |
| Testcontainers en Windows requiere Docker Desktop en marcha | Alternativa documentada: apuntar las pruebas al servicio `db-test` del `compose.yaml` |
