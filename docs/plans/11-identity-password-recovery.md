# 11 · Recuperar contraseña

## Objetivo

Que quien olvidó su contraseña la recupere por correo, sin que nadie pueda saber qué correos están registrados. Es **requisito de salida a producción**.

> **Pendiente de la pregunta P1: ¿enlace o código de 6 dígitos?** La guía (§8) y Android piden un **enlace** de un solo uso que vence en 30 minutos. El 16 oct 2026 se había decidido un **código de 6 dígitos** (en el proyecto anterior). Este plan describe las dos variantes; al aprobarlo se borra la que no se use. La recomendación es el código: todo pasa dentro de la app, sin App Links ni página web.

## Pantallas de Android que cubre

- **¿Olvidaste tu contraseña?**, **Enlace enviado** y **Contraseña nueva** (`ui/feature/auth/PasswordRecoveryScreens.kt`; `ORBITA_SPEC` §7.1).
- Iniciar sesión con el aviso "Tu contraseña se cambió. Inicia sesión con la nueva."
- Repositorio: `AuthRepository.requestPasswordReset(email)` y `resetPassword(token, newPassword)`.

## Módulos

`identity` e `integrations/mail` (puerto `Mailer` del plan 00; aquí se agrega la implementación Resend).

## Endpoints

| Método y ruta | Público | Request | Respuesta |
|---|---|---|---|
| `POST /auth/forgot-password` | Sí | `{ "email" }` | `204` **siempre**, exista o no el correo |
| *(solo código)* `POST /auth/verify-reset-code` | Sí | `{ "email", "code" }` | `204` si es válido, sin consumirlo |
| `POST /auth/reset-password` | Sí | Enlace: `{ "token", "new_password" }` · Código: `{ "email", "code", "new_password" }` | `204`; cambia la contraseña y revoca todas las sesiones |

### Errores

| Situación | HTTP | `code` | Texto en Android |
|---|---|---|---|
| Correo con formato inválido | 400 | `VALIDATION_FAILED` + `EMAIL_INVALID` | "Escribe un correo válido." |
| Contraseña nueva de menos de 8 | 400 | `VALIDATION_FAILED` + `PASSWORD_TOO_SHORT` | El enlace o el código **no** se consume |
| Enlace o código inválido, usado, vencido o sin intentos | 422 | `RESET_TOKEN_INVALID` | "El enlace ya no es válido. Pide uno nuevo." (o "El código no es válido o ya venció.") |
| Demasiadas peticiones | 429 | `TOO_MANY_REQUESTS` | "Demasiados intentos. Espera un momento." |

## Tablas y migraciones

`00010_identity_create_password_resets.sql`:

| Columna | Tipo |
|---|---|
| `id` | `UUID PK` |
| `user_id` | `UUID FK → users ON DELETE CASCADE` |
| `secret_hash` | `BYTEA NOT NULL` (SHA-256 del enlace, o HMAC del código) |
| `expires_at` | `TIMESTAMPTZ` |
| `used_at` | `TIMESTAMPTZ NULL` |
| `attempts` | `SMALLINT NOT NULL DEFAULT 0` (solo código) |
| `created_at` | `TIMESTAMPTZ` |

Índice `(user_id) WHERE used_at IS NULL`.

## Reglas

| | Enlace (guía y Android) | Código (recomendado) |
|---|---|---|
| Secreto | 32 bytes aleatorios en base64url, en el enlace | 6 dígitos de un generador criptográfico |
| Guardado | Solo su hash | Solo su hash |
| Vida | 30 minutos | 15 minutos |
| Intentos | — | 5 por código; luego queda inválido |
| Cómo llega a la app | App Links: el correo abre `https://…/reset-password#token=…` en la app (dominio verificado con `assetlinks.json`), o una página web de respaldo si no está instalada | El usuario lo escribe en la pantalla que hoy es "Enlace enviado" |

Comunes a las dos:
- `forgot-password` responde `204` siempre y tarda lo mismo exista o no el correo. Si existe, invalida los anteriores pendientes y envía uno nuevo. Hasta 3 envíos por usuario cada 24 horas; además, el límite por IP.
- `reset-password` en una transacción: comprueba el secreto con una actualización condicionada (`WHERE used_at IS NULL AND expires_at > now()`), guarda el hash de la contraseña nueva, marca el secreto usado y **revoca todas las sesiones** del usuario. No inicia sesión: la app vuelve a Iniciar sesión.
- Tras cambiarla se envía un segundo correo, "Tu contraseña se cambió", para que el dueño se entere si no fue él.
- **Correo**: `ResendMailer` (SDK HTTP de Resend o `net/http` contra su API) detrás de `Mailer`; plantillas **MJML** compiladas a HTML en build (`html/template` para los datos), en español (es-PE), con versión en texto plano. Remitente `MAIL_FROM` del dominio `atmosferast.com` verificado en Resend.
- **Envío asíncrono con reintentos**, nunca dentro de la transacción: la respuesta no espera al correo. Si Resend falla, se reintenta con espera creciente y se registra; la respuesta al cliente no cambia.
- No se registra el contenido del correo, ni el enlace, ni el código.
- **Verificación de correo** (P5): con la misma tabla y el mismo correo; queda fuera de este plan salvo que se decida otra cosa.

## Dependencias

01 (usuarios y sesiones) y 00 (puerto de correo).

## Pasos de implementación

1. Migración y consultas sqlc.
2. Dominio y casos de uso `RequestReset`, (`VerifyCode`), `ResetPassword`.
3. `integrations/mail`: `ResendMailer`, plantillas MJML y su compilación en build, cola de envío con reintentos.
4. Adapter HTTP.
5. Android: `RemoteAuthRepository.requestPasswordReset` y `resetPassword`. Con enlace: App Links en el manifiesto y `assetlinks.json` en el dominio. Con código: la pantalla "Enlace enviado" pasa a pedir el código, y `resetPassword(token, …)` cambia a `resetPassword(email, code, …)`.
6. Actualizar `ORBITA_SPEC.md` (§7.1 y §4.1), `docs/openapi.yaml` y `.env.example` si hiciera falta.

## Pruebas

Integración (con un `Mailer` que captura los correos):
- Correo existente → `204` y un correo capturado; inexistente → `204`, ningún correo, tiempo similar.
- La base guarda el hash, nunca el secreto.
- `reset-password` con el secreto del correo → `204`; la contraseña nueva sirve y la vieja no; las sesiones anteriores no renuevan.
- Reusar el secreto → `422`. Dos usos simultáneos → uno `204`, otro `422`.
- Vencido (reloj adelantado) → `422`. Pedir dos y usar el primero → `422`.
- (Código) sexto intento con un código equivocado y luego el correcto → `422`.
- Contraseña nueva de 7 caracteres → `400` y el secreto sigue sirviendo.
- Cuarto envío en 24 horas → `204` y ningún correo.

## Criterios de aceptación

- Recuperar la contraseña de principio a fin con un correo real que llega a la bandeja de entrada (no a no deseado) de Gmail y Outlook, con SPF, DKIM y DMARC en `pass`.

## Variables nuevas para el servidor

Ninguna obligatoria: `RESEND_API_KEY` y `MAIL_FROM` ya existen. Con enlace, una variable nueva para la URL base del enlace (por ejemplo `PASSWORD_RESET_URL`); se informará al implementar.

## Desviaciones

(Se completan al implementar.)
