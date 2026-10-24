# receipts

Recibos por honorarios de la web (plan 18): los datos de cada recibo, el tope anual de la persona y sus archivos (XML y PDF) en Cloudflare R2. No tiene pantallas en Android.

- **Tablas:** `receipts`, `receipt_year_limits`, `receipt_documents`.
- **Depende de:** `profile.Reader` ("hoy" en la zona de la persona) e `integrations/storage` (R2).
- **Expone:** `OnUserDeleted`, que main engancha en `identity.OnUserDeleted` para borrar los archivos de R2 de quien elimina su cuenta.

## Puertos (`app`)

| Puerto | Implementación |
|---|---|
| `Receipts`, `Limits`, `Documents` | `adapters/postgres` (sqlc) |
| `FileStorage` | `module.go` sobre `storage.Storage` (R2, o `Unavailable` sin `R2_*`) |
| `Profile` | `module.go` sobre `profile.Reader` |

## Errores

| Código | HTTP | Cuándo |
|---|---|---|
| `VALIDATION_FAILED` | 400 | Un campo no cumple (ver códigos de campo en el plan 18) |
| `RECEIPT_ALREADY_EXISTS` | 409 | Esa serie y número ya están registrados por la persona |
| `ID_CONFLICT` | 409 | El id lo usa otro registro |
| `FILE_INVALID` | 400 | El archivo no es un PDF real, no es XML o pasa el tamaño |
| `XML_RECEIPT_MISMATCH` | 422 | El `cbc:ID` del XML no es el del recibo |
| `STORAGE_UNAVAILABLE` | 503 | R2 no está configurado |
| `NOT_FOUND` | 404 | No existe o es de otra persona |
