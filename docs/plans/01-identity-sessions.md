# 01 · Registro, inicio y cierre de sesión

## Objetivo

Que una persona se registre con correo y contraseña, inicie sesión, mantenga la sesión entre usos de la app (renovación automática) y la cierre. Con esto, Android deja el modo demo en la autenticación.

## Pantallas de Android que cubre

- **Iniciar sesión** y **Registro** (`ui/feature/auth/AuthScreen.kt`; `ORBITA_SPEC` §7.1).
- Sesión persistente: al abrir la app con sesión guardada se entra directo a Inicio.
- **Cerrar sesión** en Ajustes (§7.12).
- Repositorio: `AuthRepository.session`, `signIn`, `signUp(credentials, mainCurrency)`, `signOut`.

## Módulo

`identity` (`internal/modules/identity`).

## Endpoints

Todos bajo `/api/v1`. Campos en `snake_case`.

| Método y ruta | Público | Request | Respuesta |
|---|---|---|---|
| `POST /auth/register` | Sí | `{ "email", "password", "main_currency", "timezone" }` | `201 { data: { user, tokens } }` |
| `POST /auth/login` | Sí | `{ "email", "password" }` | `200 { data: { user, tokens } }` |
| `POST /auth/refresh` | Sí | `{ "refresh_token" }` | `200 { data: { tokens } }` |
| `POST /auth/logout` | Sí | `{ "refresh_token" }` | `204` siempre, exista o no |
| `GET /me` | No | — | `200 { data: { id, email, created_at } }` |

```json
{
  "data": {
    "user": { "id": "0192…", "email": "ana@correo.com" },
    "tokens": {
      "access_token": "eyJ…",
      "access_token_expires_in": 900,
      "refresh_token": "b64url…"
    }
  }
}
```

`main_currency` y `timezone` los guarda el plan 02 (gancho de alta). En este plan se validan y se pasan al gancho; si `main_currency` no viene, `PEN`; si `timezone` no viene o no es una zona IANA válida, `America/Lima`.

### Errores

| Situación | HTTP | `code` | Texto en Android (`ORBITA_SPEC` §4.1) |
|---|---|---|---|
| Correo con formato inválido | 400 | `VALIDATION_FAILED` + `EMAIL_INVALID` | "Escribe un correo válido." |
| Contraseña de menos de 8 | 400 | `VALIDATION_FAILED` + `PASSWORD_TOO_SHORT` | "La contraseña debe tener al menos 8 caracteres." |
| Moneda fuera del catálogo | 400 | `VALIDATION_FAILED` + `CURRENCY_NOT_SUPPORTED` | — |
| Correo ya registrado | 409 | `EMAIL_ALREADY_REGISTERED` | "Ese correo ya tiene una cuenta. Inicia sesión." |
| Correo o contraseña incorrectos | 401 | `INVALID_CREDENTIALS` | "Correo o contraseña incorrectos." |
| Token de acceso ausente, inválido o vencido | 401 | `UNAUTHORIZED` | Renueva; si falla, "Tu sesión terminó. Inicia sesión de nuevo." |
| Refresh inválido, vencido o reutilizado | 401 | `SESSION_EXPIRED` | "Tu sesión terminó. Inicia sesión de nuevo." |
| Demasiados intentos | 429 | `TOO_MANY_REQUESTS` | "Demasiados intentos. Espera un momento." |

## Tablas y migraciones

`00001_identity_create_users_and_sessions.sql`:

| Tabla | Columnas |
|---|---|
| `users` | `id UUID PK`, `email VARCHAR(254) NOT NULL` (guardado en minúsculas y sin espacios), `password_hash VARCHAR(255) NOT NULL`, `created_at TIMESTAMPTZ`, `updated_at TIMESTAMPTZ`, `last_login_at TIMESTAMPTZ NULL` |
| `sessions` | `id UUID PK`, `user_id UUID FK → users ON DELETE CASCADE`, `created_at`, `last_used_at`, `revoked_at NULL`, `revoked_reason VARCHAR(30) NULL`, `user_agent VARCHAR(255) NULL` |
| `refresh_tokens` | `id UUID PK`, `session_id UUID FK → sessions ON DELETE CASCADE`, `token_hash BYTEA UNIQUE NOT NULL` (SHA-256), `created_at`, `expires_at`, `used_at NULL` |

Índices: único sobre `users(email)`; `sessions(user_id) WHERE revoked_at IS NULL`; `refresh_tokens(session_id)`.

Nunca se guarda un token en claro.

## Reglas

**Contraseñas.** argon2id (`golang.org/x/crypto/argon2`), parámetros de partida 64 MiB, 3 iteraciones, paralelismo 1, sal de 16 bytes, formato PHC en `password_hash`. Mínimo 8 caracteres (regla de Android), máximo 128. Al iniciar sesión con un correo que no existe se verifica contra un hash ficticio, para que el tiempo no revele qué correos existen. El registro sí lo revela (`409`): es lo que pide la app.

**Correo.** Se recorta y se pasa a minúsculas antes de guardar y de comparar. Formato: el mismo patrón que Android (`^[^@\s]+@[^@\s]+\.[^@\s]+$`).

**Tokens** (guía §4):

| | Acceso | Renovación |
|---|---|---|
| Forma | JWT HS256 (`golang-jwt/jwt/v5`) con `JWT_ACCESS_SECRET` | 32 bytes aleatorios en base64url, opaco |
| Contenido | `sub` (usuario), `sid` (sesión), `iat`, `exp` | — |
| Vida | `ACCESS_TOKEN_TTL_SECONDS` (900) | `REFRESH_TOKEN_TTL_DAYS` (30) |
| En el servidor | Nada | Solo su hash SHA-256 |

- **Rotación.** Cada `refresh` marca el token usado (`used_at`) y entrega uno nuevo de la misma sesión, en una transacción con actualización condicionada (`WHERE used_at IS NULL AND expires_at > now()`): dos renovaciones simultáneas no pueden usar el mismo.
- **Reutilización.** Si llega un refresh ya usado, se revoca la sesión entera (`revoked_reason = 'reuse'`) y se registra el evento. Android ya serializa la renovación con un `Mutex` (docs/02), así que no debería pasar con la app legítima.
- **Verificación del acceso.** El middleware exige exactamente HS256 (rechaza `alg: none` y cualquier otro), firma y `exp`, y **comprueba que la sesión `sid` siga sin revocar** (decisión del 21 oct 2026), para que cerrar sesión o eliminar la cuenta corte el acceso al instante. Es una consulta por clave primaria por petición.
- `@Public` no existe en Gin: las rutas públicas se montan en un grupo sin el middleware. Todo lo demás está protegido.
- `user_id` sale **solo** del token; ningún request lo acepta.

**Alta en una transacción.** `register` crea el usuario, la sesión y su primer refresh, y llama a los ganchos `OnUserRegistered(ctx, user, RegistrationOptions{MainCurrency, Timezone})` dentro de la misma transacción (`tx.Runner`). Si un gancho falla, no queda nada. En este plan no hay ganchos todavía; los agregan 02 (perfil), 03 (9 categorías) y 04 (cuenta "Efectivo").

**Límites.** `LOGIN_RATE_LIMIT_PER_MINUTE` por IP en `login`, `register` y `refresh`. Además, tras 5 fallos seguidos para un mismo correo, espera creciente (1, 5, 15 minutos) sin bloquear la cuenta.

**Eventos de seguridad** en el log (sin contraseñas ni tokens): registro, inicio correcto y fallido, renovación, reutilización, cierre.

## Dependencias

00.

## Pasos de implementación

1. Migración y consultas sqlc del módulo.
2. Dominio: `Email`, `Password` (validación), `Session`, errores tipados (`ErrEmailTaken`, `ErrInvalidCredentials`, `ErrSessionExpired`).
3. Casos de uso: `Register`, `Login`, `Refresh`, `Logout`, `Me`; puertos `UserRepository`, `SessionRepository`, `PasswordHasher`, `TokenIssuer`, `Clock`; registro de ganchos `OnUserRegistered`.
4. Adapters: Postgres, HTTP (handlers, DTOs, rutas), middleware de autenticación en `platform/middleware` (recibe un verificador por interfaz).
5. `module.go` y cableado en `main.go`.
6. Android (ver plan 13): cliente Ktor, `TokenStore` cifrado, `RemoteAuthRepository`, renovación única con `Mutex`, mapeo de errores, y la línea en `di/AppModule.kt` cuando `AppConfig.usesApi`.
7. OpenAPI y `ORBITA_SPEC.md` (§7.1 y §10).

## Pruebas

Unitarias (puertos falsos):
- Registro: correo inválido, contraseña corta y moneda desconocida → el error de campo correspondiente, en ese orden.
- Login: contraseña incorrecta y correo inexistente devuelven el mismo error.
- Refresh: rota; reusar el anterior revoca la sesión y deja inservible el nuevo.
- Un JWT vencido, con firma alterada, con `alg: none` o firmado con otro secreto → `UNAUTHORIZED`.

Integración (Postgres de desarrollo, por HTTP):
- Registrar, entrar, renovar, cerrar sesión, y no poder renovar después.
- Registrar dos veces el mismo correo, también con otras mayúsculas → `409`.
- Dos `refresh` simultáneos con el mismo token → uno `200`, otro `401`.
- Un gancho de alta que falla → no queda el usuario.
- Toda ruta protegida responde `401` sin token.
- El sexto intento de login en un minuto desde la misma IP → `429` con `Retry-After`.

Android: registrarse, cerrar la app, reabrirla y seguir con sesión; cerrar sesión vuelve a Iniciar sesión; varias peticiones con el acceso vencido producen una sola renovación.

## Criterios de aceptación

- Un usuario nuevo se registra desde la app, la cierra, la reabre al día siguiente y sigue con sesión.
- Cerrar sesión impide renovar y también usar el acceso vigente.
- Ninguna contraseña ni token aparece en claro en la base ni en los logs.

## Variables nuevas para el servidor

Ninguna (`JWT_ACCESS_SECRET`, `ACCESS_TOKEN_TTL_SECONDS`, `REFRESH_TOKEN_TTL_DAYS` y `LOGIN_RATE_LIMIT_PER_MINUTE` ya existen).

## Estado (21 oct 2026)

Implementado en API y Android, en revisión.

| Comprobación | Resultado |
|---|---|
| `go vet`, `golangci-lint run`, `go build` | Sin problemas (`0 issues`) |
| `go test ./...` sin base | Pasa; las de integración se saltan |
| `go test ./...` contra el Postgres local | Pasa: 3 del dominio, 11 unitarias de los casos de uso, 6 de seguridad (argon2id y JWT: vencido, otra firma, alterado, `alg: none`, HS512) y 9 de integración por HTTP (ciclo completo, hashes en la base, correo repetido con otras mayúsculas, orden de validación, mismo error para clave mala y correo inexistente, dos renovaciones simultáneas, gancho de alta que falla sin dejar nada, rutas protegidas, sexto login limitado) |
| Migración `00001`: `up`, `down`, `up` | Funciona; el esquema `orbita` queda con `users`, `sessions` y `refresh_tokens` |
| Binario real por HTTP | Registro 201, `/me` 200, renovación 200, reuso del refresh viejo 401 y la sesión queda revocada, `logout` 204 y luego `/me` 401 |
| Android: `testDebugUnitTest`, `assembleDebug`, `lintDebug` | Pasa: 45 pruebas (10 nuevas de la sesión remota, con servidor simulado: errores, validación, sin red, sesión guardada, cierre sin red, **5 peticiones con el token vencido = 1 renovación**, renovación rechazada) |

**En el teléfono** (I24P01, Android 13), con la app debug contra el binario del API y el Postgres local (por USB con `adb reverse tcp:3001 tcp:3001` y `API_BASE_URL=http://127.0.0.1:3001/api/v1`):

| Prueba | Resultado |
|---|---|
| Registro con un correo mal escrito | "Escribe un correo válido." (validación de la app, no llega al API) |
| Registro válido | Entra a Inicio; en la base, el usuario y 1 sesión |
| Cerrar la app y volver a abrirla | Sigue con sesión |
| Abrirla con el API apagado | Sigue con sesión (sin red) |
| Ajustes → Cerrar sesión | Vuelve a Iniciar sesión; en la base, la sesión revocada con motivo `logout` |
| Contraseña equivocada | "Correo o contraseña incorrectos." |
| Contraseña correcta | Entra; sesión nueva en la base |
| Registro con el mismo correo en mayúsculas | "Ese correo ya tiene una cuenta. Inicia sesión." |
| Iniciar sesión con el API apagado | "Sin conexión. Revisa tu internet e inténtalo de nuevo." |
| Agente de la sesión | `Orbita-Android/1.0 (Android 13; I24P01)` |

Para probarlo por Wi-Fi en lugar de USB: `API_BASE_URL=http://<IP de la PC>:3001/api/v1` y permitir el puerto 3001 en el firewall de Windows.

Aún no se ve en la app: que la sesión caducada en el servidor devuelva a Iniciar sesión. Ninguna pantalla llama todavía a una ruta privada (todo lo demás sigue en datos de ejemplo); lo cubren las pruebas unitarias de `ApiSession` y se verá en el teléfono desde el plan 02.

## Desviaciones

- **`PASSWORD_TOO_LONG`**: código de campo nuevo (más de 128 caracteres). Android lo agrega a `ValidationError`, con su texto, y también lo valida antes de enviar.
- **Catálogo de monedas en `internal/platform/currency`**: lo necesita el registro (validar `main_currency`) y lo usará el perfil (plan 02). Es un dato, no una regla.
- **Dos renovaciones simultáneas con el mismo token**: una responde 200 y la otra 401, y como la segunda cuenta como reuso, **la sesión queda revocada** (regla de reutilización). Android nunca lo provoca porque renueva de a una (`Mutex`).
- **Sesión al abrir la app**: con un refresh token guardado la app entra directo, **sin red**; el access token se pide en la primera petición. Si la renovación se rechaza, vuelve a Iniciar sesión.
- **Con `API_BASE_URL`, recuperar contraseña y eliminar cuenta fallan** con "Algo salió mal" hasta los planes 11 y 12, en lugar de fingir que funcionan.
- **`network_security_config`**: no existía. Release solo HTTPS; debug permite HTTP sin cifrar hacia el API local.
- **Error de lint ya existente en Android** (`OrbitaApp.kt`: leía textos con `LocalContext`): corregido con `LocalResources`, porque bloqueaba `lintDebug`.
- **sqlc se corre con su imagen de Docker** (necesita cgo, que no hay en Windows); el código generado se versiona, así el check del despliegue no lo necesita.
- **`PasswordParams`** en las dependencias del módulo: las pruebas bajan el costo de argon2id; en producción se usan 64 MiB, 3 pasadas, 1 hilo.
- **Agente de la app**: Android se identifica como `Orbita-Android/<versión> (Android <versión>; <modelo>)`, que el API guarda en `sessions.user_agent` (antes llegaba `ktor-client`).
- **Documentos de Android regenerados**: `docs/CLAUDE.md` nuevo y `docs/ORBITA_SPEC.md` a partir del anterior; los `01` a `06` se retiran y su contenido vigente pasa al spec (tokens de diseño, casos de prueba, datos de ejemplo).
