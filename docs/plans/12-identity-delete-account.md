# 12 · Eliminar cuenta

## Objetivo

Que una persona borre su cuenta y **todos** sus datos de verdad (no un archivado ni un borrado lógico), desde la app. Es **requisito de salida a producción** y de Google Play.

**Decisión del usuario (21 oct 2026):** la página web para eliminar la cuenta sin la app **se omite por ahora**. Google Play la exige en la ficha, así que queda pendiente antes de publicar.

## Pantallas de Android que cubre

- **Ajustes → Eliminar cuenta**, con el diálogo que pide la contraseña (`SettingsScreen.kt`, `DeleteAccountDialog`; `ORBITA_SPEC` §7.12).
- Repositorio: `AuthRepository.deleteAccount(password)`.

## Módulo

`identity`, más un gancho `OnUserDeleted` en cada módulo con datos del usuario (para lo que no se borre por cascada).

## Endpoints

| Método y ruta | Request | Respuesta |
|---|---|---|
| `DELETE /me` | `{ "password" }` | `204`; borra al usuario y todos sus datos, y termina sus sesiones |

### Errores

| Situación | HTTP | `code` |
|---|---|---|
| Contraseña vacía | 400 | `VALIDATION_FAILED` + `PASSWORD_REQUIRED` ("Escribe tu contraseña.") |
| Contraseña incorrecta | 401 | `INVALID_CREDENTIALS` |
| Demasiados intentos | 429 | `TOO_MANY_REQUESTS` |

## Tablas y migraciones

Ninguna nueva. Se apoya en que **toda tabla con `user_id` lo referencia con `ON DELETE CASCADE`** (planes 01 a 10). Las filas globales de `exchange_rates` (`user_id` nulo, plan 14) no se tocan.

## Reglas

- Exige la contraseña actual aunque el token sea válido: un teléfono desbloqueado en manos ajenas no basta.
- **Borrado real**, en una transacción: verificar la contraseña → ganchos `OnUserDeleted` (hoy ninguno los necesita; quedan para archivos en R2 u otros datos fuera de la base) → `DELETE FROM users`. La cascada se lleva el resto. Si algo falla, no se borra nada.
- Las sesiones desaparecen con el usuario y el middleware lo comprueba en cada petición (plan 01): el token de acceso vigente deja de servir al instante.
- El correo queda libre: la misma persona puede registrarse otra vez y empieza de cero.
- No hay periodo de gracia ni papelera.
- Se registra el evento con el id del usuario, sin su correo.
- Los respaldos conservan los datos hasta que caducan; debe decirlo la política de privacidad.
- **Página web**: pendiente (ver arriba). Cuando se haga, usa el mismo caso de uso.

## Dependencias

01 a 10: se implementa cuando ya existen todas las tablas del usuario, para que la prueba las cubra todas.

## Pasos de implementación

1. Caso de uso `DeleteAccount` y gancho `OnUserDeleted`.
2. Adapter HTTP de `DELETE /me`.
3. Prueba que **descubre sola las tablas**: consulta el catálogo de Postgres, toma todas las del esquema `orbita` con columna `user_id`, crea un usuario con datos en todas, lo elimina y comprueba que no queda ninguna fila suya. Una tabla nueva sin cascada hace fallar la prueba.
4. Android: `RemoteAuthRepository.deleteAccount` (limpia los tokens y la sesión local al terminar).
5. `ORBITA_SPEC.md` §7.12 y OpenAPI.

## Pruebas

Integración:
- `DELETE /me` con la contraseña correcta → `204`, y cero filas del usuario en todas las tablas con `user_id` (prueba que descubre las tablas).
- Los datos de otro usuario quedan intactos.
- Contraseña incorrecta → `401` y no se borra nada.
- Fallo forzado a mitad → el usuario y sus datos siguen completos.
- Después: iniciar sesión y renovar → `401`; registrarse con el mismo correo → `201` con solo los datos iniciales.

## Criterios de aceptación

- Desde Ajustes, eliminar la cuenta borra todo y vuelve a Iniciar sesión.

## Variables nuevas para el servidor

Ninguna.

## Desviaciones

(Se completan al implementar.)
