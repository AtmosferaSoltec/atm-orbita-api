# 22 · La autenticación pasa a atm-auth-api

**Estado:** hecho (2 nov 2026). Sustituye a los planes 01, 11, 12, 17, 19 y 21 en lo que toca a cuentas, sesiones, recuperación de contraseña, Google y eliminación de cuenta.

## Qué cambió

- Orbita ya no tiene módulo `identity` ni `integrations/mail`. Registro, inicio de sesión, renovación, recuperación de contraseña por correo, Google y eliminar cuenta los hace **atm-auth-api** (`https://auth.atmosferast.com/v1`, sistema `orbita`); el correo sale por atm-messaging-api (Resend).
- Las apps (web, Android, iOS) hablan con auth para todo eso y con este API, con el mismo access token, para los datos.
- Este API valida los tokens RS256 de auth con `AUTH_PUBLIC_KEY_BASE64` y `AUTH_ISSUER` (`internal/platform/centralauth`). Solo vale `role=user`, `system=orbita` y `sub` UUID.
- La primera petición de cada persona crea su fila en `users` (mismo id que en auth), su perfil (PEN, America/Lima) y sus datos iniciales. `users` queda solo con `id, email, name, created_at, updated_at`: sirve para que la cascada borre los datos de quien elimina su cuenta.
- `GET /me` sigue existiendo (lee esa fila). `DELETE /me` y todos los `/auth/*` se retiraron.
- Eliminar cuenta: la app llama a `DELETE /v1/account` de auth; auth llama antes a `POST /api/v1/internal/auth/user-deleted` de este API (solo con su token de servicio, `ACCOUNT_DELETION_HOOKS` en auth) y este borra sus archivos (de atm-storage-api desde el plan 23; antes R2) y datos. Si falla, auth no borra nada y se puede reintentar.
- Migración `00017`: borra los datos de prueba (aún no había usuarios) y las tablas `sessions`, `refresh_tokens`, `password_reset_codes`, `external_identities`, `google_login_challenges`. No es reversible: volver atrás es restaurar un respaldo.

## Variables

Ya no se leen: `JWT_ACCESS_SECRET`, `ACCESS_TOKEN_TTL_SECONDS`, `REFRESH_TOKEN_TTL_DAYS`, `WEB_REFRESH_TOKEN_TTL_DAYS`, `GOOGLE_WEB_CLIENT_ID`, `LOGIN_RATE_LIMIT_PER_MINUTE`, `RESEND_API_KEY`, `MAIL_FROM`. Obligatorias nuevas: `AUTH_PUBLIC_KEY_BASE64` y `AUTH_ISSUER`.

## Desviaciones

- El registro de auth no recibe moneda principal ni zona horaria: el perfil nace en PEN / America/Lima y la app aplica la zona después con `PATCH /settings`.
- Los códigos de error de auth son `snake_case` en minúsculas; los de este API siguen en `SCREAMING_SNAKE_CASE`.

## Después

[Plan 23](23-shared-services-storage-and-documents.md): Orbita también es cliente de storage y documents con un token de servicio.
