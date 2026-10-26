# Plan 21 · Eliminar la cuenta confirmando con Google

**Estado:** hecho (26 oct 2026). Lo exige Google Play: quien puede crear una cuenta en la app debe poder pedir su eliminación, dentro de la app y desde la web. `DELETE /me` pide la contraseña, y una cuenta creada con Google no tiene (su `password_hash` queda vacío), así que nunca podía eliminarse.

## Decisión

| Tema | Decisión | Motivo |
|---|---|---|
| Cómo confirma quien no tiene contraseña | Una credencial de Google **recién emitida**, con un reto de un solo uso, de la **misma identidad** (`external_identities`) vinculada a la cuenta | Igual de fuerte que la contraseña: una sesión robada no basta, y la cuenta de Google de otra persona tampoco |
| Ruta | `POST /auth/google/delete-account` (con sesión), no `DELETE /me` | La cookie del reto solo viaja a `/api/v1/auth/google/*` (SameSite=Strict, `Path` acotado); en `/me` no llegaría |
| Reto | El mismo del inicio con Google: cookie en el navegador, `nonce` en el cuerpo en una app nativa | Un solo mecanismo ya revisado |
| Si falla | Todo va en una transacción: el reto se devuelve y se reintenta hasta que venza (5 min) | Igual que vincular una cuenta |
| Quien tiene contraseña y Google | Puede usar cualquiera de los dos | — |

## Cambios

- `identity/app`: `DeleteAccountWithGoogle`; el borrado común queda en `removeAccount` (ganchos + usuario).
- `identity/adapters/http`: ruta nueva. `DELETE /me` y la ruta nueva también borran la cookie de sesión del navegador.
- Sin migraciones ni variables nuevas.

## Pruebas

Navegador: borra la cuenta sin contraseña y su identidad vinculada; el reto sirve una sola vez; otra cuenta de Google no puede; un fallo devuelve el reto; sin sesión o sin la cookie del reto, `401`. App nativa: con el `nonce` en el cuerpo. La prueba "eliminar la cuenta no deja ninguna fila" ahora también siembra préstamos, servicios, recibos e identidad de Google.

## Para Android

Si la cuenta no tiene contraseña, pedir un reto (`POST /auth/google/challenge`), obtener la credencial de Google con ese `nonce` y llamar a `POST /auth/google/delete-account` con `{credential, nonce}` y el Bearer. La web ya lo usa (plan de la web: `/delete-account`).
