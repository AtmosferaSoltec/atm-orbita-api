# Módulo identity

Cuentas de usuario y sus sesiones: registro, inicio y cierre de sesión, renovación del access token y la autenticación de cada petición. Plan: [`docs/plans/01-identity-sessions.md`](../../../docs/plans/01-identity-sessions.md).

## Tablas (dueño)

`users`, `sessions`, `refresh_tokens` (migración `00001`). Nadie más las consulta: los demás módulos solo guardan `user_id`, con `ON DELETE CASCADE`.

## API pública (`module.go`)

| Qué | Para quién |
|---|---|
| `New(Deps)`, `Routes(public, limited, protected)` | `cmd/api/main.go` |
| `Authenticator()` | `auth.Require`, que protege todas las rutas privadas |
| `OnUserRegistered(hook)` | Los módulos que crean datos al registrarse (perfil, categorías, cuenta "Efectivo"). Corre dentro de la transacción del alta |

## Puertos (`app/ports.go`)

`Users`, `Sessions` (Postgres, `adapters/postgres`), `PasswordHasher` (argon2id) y `AccessTokens` (JWT HS256), los dos en `adapters/security`; `Transactor` (`platform/tx`).

## Endpoints

`POST /auth/register`, `POST /auth/login`, `POST /auth/refresh` (con el límite `LOGIN_RATE_LIMIT_PER_MINUTE` por IP), `POST /auth/logout` y `GET /me` (privada).

## Errores

| `code` | HTTP | Cuándo |
|---|---|---|
| `VALIDATION_FAILED` + `EMAIL_INVALID`, `PASSWORD_TOO_SHORT`, `PASSWORD_TOO_LONG`, `CURRENCY_NOT_SUPPORTED`, `INVALID_FORMAT` | 400 | Formulario o cuerpo inválido, en el orden de revisión de Android |
| `EMAIL_ALREADY_REGISTERED` | 409 | Registro con un correo que ya existe |
| `INVALID_CREDENTIALS` | 401 | Correo o contraseña incorrectos (igual para ambos casos) |
| `SESSION_EXPIRED` | 401 | Refresh inválido, vencido, de una sesión cerrada o reutilizado (esto último revoca la sesión) |
| `UNAUTHORIZED` | 401 | Access token ausente, inválido, vencido o de una sesión revocada |
| `TOO_MANY_REQUESTS` | 429 | Límite por IP, o 5 fallos seguidos con un correo (espera de 1, 5 y 15 minutos), con `Retry-After` |
