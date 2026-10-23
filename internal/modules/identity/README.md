# Módulo identity

Cuentas de usuario y sus sesiones: registro, inicio y cierre de sesión, renovación del access token, la autenticación de cada petición y recuperar la contraseña con un código. Planes: [`01`](../../../docs/plans/01-identity-sessions.md) y [`11`](../../../docs/plans/11-identity-password-recovery.md).

## Tablas (dueño)

`users`, `sessions`, `refresh_tokens` (migración `00001`) y `password_reset_codes` (migración `00010`: solo el HMAC del código, con una clave derivada de `JWT_ACCESS_SECRET`). Nadie más las consulta: los demás módulos solo guardan `user_id`, con `ON DELETE CASCADE`.

## API pública (`module.go`)

| Qué | Para quién |
|---|---|
| `New(Deps)`, `Routes(public, limited, protected)` | `cmd/api/main.go`. `Deps.Mailer` envía los correos de la recuperación (en `main`, la cola de `integrations/mail` sobre Resend o el log) |
| `Authenticator()` | `auth.Require`, que protege todas las rutas privadas |
| `OnUserRegistered(hook)` | Los módulos que crean datos al registrarse (perfil, categorías, cuenta "Efectivo"). Corre dentro de la transacción del alta |

## Puertos (`app/ports.go`)

`Users`, `Sessions`, `ResetCodes` (Postgres, `adapters/postgres`), `ResetMailer` (arma los correos con `integrations/mail`), `PasswordHasher` (argon2id) y `AccessTokens` (JWT HS256), los dos en `adapters/security`; `Transactor` (`platform/tx`).

## Endpoints

`POST /auth/register`, `POST /auth/login`, `POST /auth/refresh` , `POST /auth/forgot-password`, `POST /auth/verify-reset-code`, `POST /auth/reset-password` (con el límite `LOGIN_RATE_LIMIT_PER_MINUTE` por IP), `POST /auth/logout` y `GET /me` (privada).

## Recuperar la contraseña (plan 11)

- `forgot-password` responde `204` siempre y enseguida: el trabajo va en segundo plano, así el tiempo no delata si el correo existe. Si existe, deja sin efecto los códigos pendientes y envía uno nuevo de 6 dígitos (`crypto/rand`), que dura 15 minutos. Hasta 3 envíos por usuario cada 24 horas.
- `verify-reset-code` dice si el código sirve sin consumirlo; un fallo cuenta como intento. Al quinto fallo el código deja de servir.
- `reset-password`, en una transacción con el código bloqueado: lo marca usado, guarda la contraseña nueva y cierra todas las sesiones. Después envía "Tu contraseña se cambió". Una contraseña nueva inválida no consume el código.

## Errores

| `code` | HTTP | Cuándo |
|---|---|---|
| `VALIDATION_FAILED` + `EMAIL_INVALID`, `PASSWORD_TOO_SHORT`, `PASSWORD_TOO_LONG`, `CURRENCY_NOT_SUPPORTED`, `INVALID_FORMAT` | 400 | Formulario o cuerpo inválido, en el orden de revisión de Android |
| `EMAIL_ALREADY_REGISTERED` | 409 | Registro con un correo que ya existe |
| `INVALID_CREDENTIALS` | 401 | Correo o contraseña incorrectos (igual para ambos casos) |
| `SESSION_EXPIRED` | 401 | Refresh inválido, vencido, de una sesión cerrada o reutilizado (esto último revoca la sesión) |
| `UNAUTHORIZED` | 401 | Access token ausente, inválido, vencido o de una sesión revocada |
| `VALIDATION_FAILED` + `EMAIL_INVALID`, `CODE_INVALID`, `PASSWORD_TOO_SHORT` (campo `new_password`) | 400 | Recuperar la contraseña |
| `RESET_CODE_INVALID` | 422 | Código incorrecto, usado, vencido, sin intentos o de un correo sin cuenta |
| `TOO_MANY_REQUESTS` | 429 | Límite por IP, o 5 fallos seguidos con un correo (espera de 1, 5 y 15 minutos), con `Retry-After` |
