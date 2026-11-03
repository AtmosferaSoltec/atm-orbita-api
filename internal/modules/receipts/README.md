# receipts

Recibos por honorarios de la web (plan 18): los datos de cada recibo, el tope anual de la persona y sus archivos (XML y PDF) en atm-storage-api. No tiene pantallas en Android.

- **Tablas:** `receipts`, `receipt_year_limits`, `receipt_documents`.
- **Depende de:** `profile.Reader` ("hoy" en la zona de la persona) e `integrations/storage` (atm-storage-api con el token de servicio de Orbita).
- **Expone:** `OnUserDeleted`, que main engancha en `centralauth.Users.OnDelete`: lee los `storage_key` de la persona en `receipt_documents` y los borra (el token de servicio no puede listar archivos).

## Puertos (`app`)

| Puerto | Implementación |
|---|---|
| `Receipts`, `Limits`, `Documents` | `adapters/postgres` (sqlc) |
| `FileStorage` | `module.go` sobre `storage.Storage` (`StorageAPI`, o `Unavailable` sin `AUTH_CLIENT_SECRET`) |
| `Profile` | `module.go` sobre `profile.Reader` |

Los archivos se bajan con URL firmada (`…/url`) y se ven dentro de la web con `…/content`, que el API sirve bajando los bytes de la URL firmada (Orbita no controla el CORS del bucket; ver plan 18).

`receipt_documents.storage_key` guarda el id (UUID) que atm-storage-api da al subir; la persona dueña va como `company_id` (Orbita no tiene empresas). Un 404 al borrar cuenta como ya borrado.

## Errores

| Código | HTTP | Cuándo |
|---|---|---|
| `VALIDATION_FAILED` | 400 | Un campo no cumple (ver códigos de campo en el plan 18) |
| `RECEIPT_ALREADY_EXISTS` | 409 | Esa serie y número ya están registrados por la persona |
| `ID_CONFLICT` | 409 | El id lo usa otro registro |
| `FILE_INVALID` | 400 | El archivo no es un PDF real, no es XML o pasa el tamaño |
| `XML_RECEIPT_MISMATCH` | 422 | El `cbc:ID` del XML no es el del recibo |
| `STORAGE_UNAVAILABLE` | 503 | Falta `AUTH_CLIENT_SECRET` (sin token de servicio no hay almacenamiento) |
| `NOT_FOUND` | 404 | No existe o es de otra persona |
