# Plan 17 · Sesión para la web y nombre de la cuenta

**Estado:** hecho (24 oct 2026). Pedido por el usuario: la web (`atm-orbita-web`) usa las **mismas cuentas** que Android; iniciar sesión, registrarse y recuperar la contraseña tienen que funcionar de verdad desde el navegador.

## Objetivo

1. Que un navegador pueda mantener la sesión **sin guardar el refresh token donde JavaScript lo lea**.
2. Que la cuenta tenga un **nombre** (lo pide la web al registrarse). Android no lo envía todavía y sigue funcionando igual.

## Decisiones

| Tema | Decisión | Motivo |
|---|---|---|
| Dónde vive el refresh token en la web | Cookie `orbita_refresh`: `HttpOnly`, `SameSite=Strict`, `Path=/api/v1/auth`, `Max-Age` = la duración del token emitido (plan 19: `WEB_REFRESH_TOKEN_TTL_DAYS` para sesiones web nuevas). `Secure` siempre, salvo con `APP_ENV=development` (http://localhost) | Un XSS no puede robarlo. El access token sigue igual: en memoria, por `Authorization: Bearer` |
| Cómo pide la web la cookie | Cabecera `X-Session-Transport: cookie` en `register`, `login`, `refresh` y `logout` | Sin la cabecera todo funciona como hoy (Android no cambia). Una cabecera propia obliga al navegador a hacer *preflight*: otro sitio no puede forjar la petición (CSRF), y además la cookie es `Strict` |
| Respuesta con la cabecera | `tokens.refresh_token` no viene en el cuerpo (`omitempty`); viene en la cookie | El token nunca toca JavaScript |
| Nombre | Columna `users.name VARCHAR(60)` **nullable**. `register` acepta `name` opcional (recortado; vacío = sin nombre; más de 60 → `NAME_TOO_LONG`). `user.name` sale en `register`, `login` y `GET /me` (`null` si no tiene) | La web lo exige en su formulario; Android puede seguir sin enviarlo. Nullable para que la versión desplegada del API siga funcionando con la base ya migrada |

## Endpoints que cambian

| Método y ruta | Cambio |
|---|---|
| `POST /auth/register` | Acepta `name`. Con `X-Session-Transport: cookie`: pone la cookie y omite `refresh_token` |
| `POST /auth/login` | Con la cabecera: cookie, sin `refresh_token` en el cuerpo |
| `POST /auth/refresh` | Con la cabecera, el token sale de la cookie (el cuerpo puede venir vacío) y la cookie se rota |
| `POST /auth/logout` | Con la cabecera, usa la cookie y la borra (`Max-Age=0`). Sigue respondiendo 204 siempre |
| `GET /me` | Agrega `name` |

CORS: `X-Session-Transport` se agrega a `Access-Control-Allow-Headers` y se responde `Access-Control-Allow-Credentials: true` a los orígenes de `CORS_ORIGIN`. En local la web no necesita CORS: su servidor de desarrollo reenvía `/api` al API (mismo origen).

## Migración

`00011_identity_add_user_name.sql`: `ALTER TABLE users ADD COLUMN name VARCHAR(60)`. Solo agrega una columna nullable: es segura con la versión anterior del API corriendo contra la misma base (la que usa Android en el VPS).

## Pruebas

- Dominio: nombre recortado, vacío → sin nombre, 61 caracteres → `NAME_TOO_LONG`.
- Integración (Postgres local): registro con nombre lo devuelve en `/me`; registro sin nombre → `null`.
- HTTP: con la cabecera, `login` pone la cookie con sus atributos y no trae `refresh_token`; `refresh` lee la cookie y la rota; reusar la cookie vieja revoca la sesión; `logout` la borra. Sin la cabecera, todo igual que antes.

## Criterios de aceptación

- Android (sin cambios) se registra, entra y renueva igual que antes.
- La web entra, recarga la página y sigue con sesión, sin que el refresh token aparezca en `localStorage` ni en JavaScript.

## Desviaciones

Ninguna. Migración `00011` aplicada en la base del VPS el 24 oct 2026, desde local por el túnel SSH (`scripts/dev.sh migrate up`); el API desplegado siguió `healthy`.
