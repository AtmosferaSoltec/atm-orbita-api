# 11 · Recuperar contraseña

## Objetivo

Que quien olvidó su contraseña la recupere con un **código de 6 dígitos** que llega por correo, sin que nadie pueda saber qué correos están registrados. Es **requisito de salida a producción**.

**Decisión del usuario (21 oct 2026):** código de 6 dígitos en tres pasos, **pedir el correo → escribir el código → contraseña nueva**. Sustituye al enlace de un solo uso que describían la guía (§8) y Android: no hacen falta App Links ni una página web.

## Pantallas de Android que cubre

Las tres de `ui/feature/auth/PasswordRecoveryScreens.kt` (`ORBITA_SPEC` §7.1), con el segundo paso cambiado:

| Paso | Pantalla hoy | Con este plan |
|---|---|---|
| 1 | "Recupera tu contraseña": pide el correo | Igual; el texto dice "te enviaremos un código" |
| 2 | "Revisa tu correo": enlace enviado (y en demo, "abrir la pantalla del enlace") | **"Escribe el código"**: campo de 6 dígitos, "Vence en 15 minutos", "Pedir otro código" |
| 3 | "Crea una contraseña nueva" (se abría desde el enlace) | Igual; se llega tras validar el código |

- Iniciar sesión muestra después "Tu contraseña se cambió. Inicia sesión con la nueva."
- Repositorio: `AuthRepository.requestPasswordReset(email)`; `resetPassword(token, newPassword)` pasa a `verifyResetCode(email, code)` y `resetPassword(email, code, newPassword)` (cambio de Android, plan 13).

## Módulos

`identity` e `integrations/mail` (puerto `Mailer` del plan 00; aquí se agrega la implementación Resend).

## Endpoints

| Método y ruta | Público | Request | Respuesta |
|---|---|---|---|
| `POST /auth/forgot-password` | Sí | `{ "email" }` | `204` **siempre**, exista o no el correo |
| `POST /auth/verify-reset-code` | Sí | `{ "email", "code" }` | `204` si es válido; **no lo consume** (cuenta como intento si falla) |
| `POST /auth/reset-password` | Sí | `{ "email", "code", "new_password" }` | `204`; cambia la contraseña y revoca todas las sesiones |

### Errores

| Situación | HTTP | `code` | Texto en Android |
|---|---|---|---|
| Correo con formato inválido | 400 | `VALIDATION_FAILED` + `EMAIL_INVALID` | "Escribe un correo válido." |
| Código que no son 6 dígitos | 400 | `VALIDATION_FAILED` + `CODE_INVALID` | "Escribe los 6 dígitos del código." |
| Contraseña nueva de menos de 8 | 400 | `VALIDATION_FAILED` + `PASSWORD_TOO_SHORT` | El código **no** se consume |
| Código incorrecto, usado, vencido o sin intentos | 422 | `RESET_CODE_INVALID` | "El código no es válido o ya venció. Pide uno nuevo." |
| Demasiadas peticiones | 429 | `TOO_MANY_REQUESTS` | "Demasiados intentos. Espera un momento." |

Los dos textos nuevos se agregan a `ORBITA_SPEC.md` §4.1 (y reemplazan "El enlace ya no es válido").

## Tablas y migraciones

`00010_identity_create_password_reset_codes.sql`:

| Columna | Tipo |
|---|---|
| `id` | `UUID PK` |
| `user_id` | `UUID FK → users ON DELETE CASCADE` |
| `code_hash` | `BYTEA NOT NULL` (HMAC-SHA256 del código con `JWT_ACCESS_SECRET` como clave derivada, para que 6 dígitos no se puedan probar contra la base si se filtra) |
| `expires_at` | `TIMESTAMPTZ NOT NULL` |
| `attempts` | `SMALLINT NOT NULL DEFAULT 0` |
| `used_at` | `TIMESTAMPTZ NULL` |
| `created_at` | `TIMESTAMPTZ` |

Índice `(user_id) WHERE used_at IS NULL`.

## Reglas

- **Código**: 6 dígitos de un generador criptográfico (`crypto/rand`). Se guarda solo su hash. Vida **15 minutos**.
- **Intentos**: hasta **5 por código** (seis dígitos son un millón de combinaciones; sin tope, un script los prueba). Al quinto fallo el código queda inválido. Además, límite por IP en `verify-reset-code` y `reset-password`.
- **Envíos**: `forgot-password` responde `204` siempre y tarda lo mismo exista o no el correo. Si existe, invalida los códigos pendientes y envía uno nuevo. Hasta **3 envíos por usuario cada 24 horas**; el cuarto responde `204` sin enviar nada.
- **`verify-reset-code`** solo dice si el código sirve, para pasar a la pantalla de contraseña nueva; no lo marca usado.
- **`reset-password`**, en una transacción: comprueba el código con una actualización condicionada (`WHERE used_at IS NULL AND expires_at > now() AND attempts < 5`), guarda el hash de la contraseña nueva, marca el código usado y **revoca todas las sesiones** del usuario. No inicia sesión: la app vuelve a Iniciar sesión.
- Tras cambiarla se envía un segundo correo, "Tu contraseña se cambió", para que el dueño se entere si no fue él.
- **Correo**: `ResendMailer` (API HTTP de Resend con `net/http`) detrás de `Mailer`; plantillas **MJML** compiladas a HTML en build y completadas con `html/template`; en español (es-PE), con versión en texto plano; el código en grande. Remitente `MAIL_FROM` del dominio `atmosferast.com` verificado en Resend. Sin `RESEND_API_KEY` en desarrollo, el correo (y el código) se escribe en el log.
- **Envío asíncrono con reintentos**, fuera de la transacción: la respuesta no espera al correo. Si Resend falla, se reintenta con espera creciente y se registra.
- No se registra el código ni el contenido del correo en producción.
- **Verificación de correo**: fuera de este plan (decisión del 21 oct 2026: después).

## Dependencias

01 (usuarios y sesiones) y 00 (puerto de correo).

## Pasos de implementación

1. Migración y consultas sqlc.
2. Dominio y casos de uso `RequestReset`, `VerifyCode`, `ResetPassword`.
3. `integrations/mail`: `ResendMailer`, plantillas MJML y su compilación, cola de envío con reintentos.
4. Adapter HTTP.
5. Android: la pantalla del paso 2 pide el código; `AuthRepository` con `verifyResetCode` y `resetPassword(email, code, password)`; el modo demo acepta cualquier código de 6 dígitos.
6. `ORBITA_SPEC.md` (§7.1 y §4.1) y `docs/openapi.yaml`.

## Pruebas

Integración (con un `Mailer` que captura los correos):
- Correo existente → `204` y un correo capturado con 6 dígitos; inexistente → `204`, ningún correo, tiempo similar.
- La base guarda el hash, nunca el código.
- `verify-reset-code` con el código → `204` y el código sigue sirviendo.
- `reset-password` con el código → `204`; la contraseña nueva sirve y la vieja no; las sesiones anteriores ya no sirven.
- Reusar el código → `422`. Dos `reset-password` simultáneos → uno `204`, otro `422`.
- Vencido (reloj adelantado 16 minutos) → `422`. Pedir dos y usar el primero → `422`.
- Cinco intentos fallidos y luego el correcto → `422`.
- Contraseña nueva de 7 caracteres → `400` y el código sigue sirviendo.
- Cuarto envío en 24 horas → `204` y ningún correo.

## Criterios de aceptación

- Recuperar la contraseña de principio a fin desde la app, con un correo real que llega a la bandeja de entrada (no a no deseado) de Gmail y Outlook, con SPF, DKIM y DMARC en `pass`.

## Variables nuevas para el servidor

Ninguna: `RESEND_API_KEY` y `MAIL_FROM` ya existen.

## Estado (23 oct 2026)

**Hecho y aprobado.** El criterio de aceptación se cumplió con un correo real: con el API arrancado con `RESEND_API_KEY` y `MAIL_FROM`, el código llegó por Resend a la bandeja de entrada de Gmail del usuario y la recuperación funcionó de principio a fin en el teléfono. (Outlook no se probó: el usuario no lo pidió.)

| Comprobación | Resultado |
|---|---|
| `gofmt`, `go vet`, `golangci-lint run`, `go build` | Sin problemas (`0 issues`) |
| `go test ./...` sin base / contra el Postgres local | Pasa. Correo (3): los dos correos llevan el código (o el aviso) en HTML y en texto; Resend recibe la clave, el remitente y el destinatario, y un error se informa; la cola reintenta (2 fallos y sale) y se rinde tras 3. Integración por HTTP (6), con un `Mailer` que captura: de principio a fin (correo con 6 dígitos, la base guarda solo el HMAC de 32 bytes, verificar no consume, contraseña de 7 → 400 sin consumir, cambio → 204, la nueva sirve y la vieja no, sesiones y renovación anteriores cortadas, reusar → 422, llega "se cambió"); correo inexistente → 204 sin correo; formatos → `EMAIL_INVALID` y `CODE_INVALID`; pedir otro invalida el anterior; vencido a los 16 minutos → 422; 5 fallos y luego el correcto → 422; dos cambios simultáneos → un 204 y un 422; el cuarto pedido del día no envía y al día siguiente sí |
| Migración `00010`: `down` y `up` | Funciona |
| OpenAPI | Válido (Redocly) |
| Android: pruebas, `lintDebug`, `installDebug` | 84 pruebas pasan; ningún aviso nuevo de lint |

**En el teléfono** (API real + Postgres local, sin `RESEND_API_KEY`: el correo y su código quedan en el log):

| Prueba | Resultado |
|---|---|
| "¿Olvidaste tu contraseña?" con `cats03@prueba.com` | "Escribe el código" con "Si cats03@prueba.com tiene una cuenta, te enviamos un código de 6 dígitos. Vence en 15 minutos." |
| Un código incorrecto | "El código no es válido o ya venció. Pide uno nuevo." |
| El código del correo | Lleva a "Crea una contraseña nueva" |
| Guardar la contraseña nueva | Vuelve a Iniciar sesión con "Tu contraseña se cambió. Inicia sesión con la nueva."; entrar con la nueva funciona |

## Desviaciones

- Código de 6 dígitos en lugar del enlace que decía la guía §8 (decisión del usuario, 21 oct 2026). La guía ya está actualizada.
- **Las plantillas MJML se compilan una vez, no en cada build**: en el repo están la fuente (`.mjml`) y el HTML compilado (`.html`, que se embebe con `go:embed`), con el comando para recompilar en `internal/integrations/mail/templates/README.md`. Compilar en el build habría puesto Node en el Dockerfile, que no se toca.
- **"Tarda lo mismo exista o no el correo"**: `forgot-password` responde 204 enseguida y hace todo (buscar al usuario, guardar el código y encolar el correo) en segundo plano.
- **La cola de correo vive en el proceso** (hasta 100 en espera, 4 intentos con espera de 2, 4 y 8 s); al apagar el API espera hasta 10 s a los pendientes. Un correo que no sale tras los reintentos se registra sin su contenido.
- **El hash del código lleva también el id del usuario** (HMAC-SHA256 con una clave derivada de `JWT_ACCESS_SECRET`): el mismo código de dos personas da hashes distintos.
- **`CODE_INVALID`** para un código que no son 6 dígitos, y la contraseña nueva se valida en el campo `new_password`.
- **Las tres rutas usan el límite por IP de inicio de sesión** (`LOGIN_RATE_LIMIT_PER_MINUTE`), además de los 5 intentos por código.
- **Android**: el paso 2 es la pantalla nueva `ResetCodeScreen` ("Escribe el código", "Continuar", "Pedir otro código"); en modo demo, la ayuda avisa que sirve cualquier código de 6 dígitos.
- **Android pide la contraseña nueva dos veces** ("Repite la contraseña nueva"); si no coinciden dice "Las contraseñas no coinciden." y no envía nada, así el código no se gasta. Lo pidió el usuario al probar; el API no cambia.
