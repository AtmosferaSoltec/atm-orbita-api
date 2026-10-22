# Módulo ledger

El libro del usuario. Por ahora, sus **categorías** de ingreso y de egreso; en los planes que siguen, cuentas, movimientos y transferencias. Plan: [`docs/plans/03-ledger-categories.md`](../../../docs/plans/03-ledger-categories.md).

## Tablas (dueño)

`categories`, migración `00003`. `name` es lo que escribió la persona (recortado, con espacios simples); `name_normalized` (minúsculas, sin tildes, conserva la ñ) solo sirve para comparar y es nulo al archivar. La migración también da las 9 iniciales a los usuarios que ya existían.

## API pública (`module.go`)

| Qué | Para quién |
|---|---|
| `New(runner, clock)`, `Routes(protected)` | `cmd/api/main.go` |
| `OnUserRegistered(ctx, userID)` | El gancho de alta de `identity` (lo cablea `main.go`, después del perfil): crea las 9 categorías iniciales en la misma transacción |

## Reglas

- Iniciales: egreso → Alimentación, Transporte, Vivienda, Salud, Ocio, Otros; ingreso → Sueldo, Freelance, Otros ingresos (`domain.InitialCategories`, los colores de `SampleData.kt`).
- El nombre no se repite dentro del mismo tipo, sin distinguir mayúsculas ni tildes. Lo decide el índice único de la base (no una lectura previa), así dos altas simultáneas no pasan las dos.
- El tipo no cambia después de crearla.
- Archivar la oculta y libera su nombre; los movimientos que ya la usan la conservan. Archivar dos veces no es un error.
- La lista sale en el orden en que se crearon.
- La app puede mandar el `id`: un reintento con el mismo contenido devuelve la existente (200); con otro, `ID_CONFLICT`.

## Endpoints

`GET /categories`, `POST /categories`, `PATCH /categories/{id}`, `POST /categories/{id}/archive`.

## Errores

| `code` | HTTP | Cuándo |
|---|---|---|
| `VALIDATION_FAILED` + `NAME_REQUIRED`, `NAME_TOO_LONG`, `KIND_INVALID`, `COLOR_INVALID`, `INVALID_FORMAT` | 400 | En ese orden |
| `CATEGORY_NAME_TAKEN` (details: `name` / `NAME_TAKEN`) | 409 | Nombre repetido en ese tipo |
| `ID_CONFLICT` | 409 | El `id` ya existe con otro contenido o es de otro usuario |
| `NOT_FOUND` | 404 | No existe, está archivada (al editar) o es de otro usuario |
