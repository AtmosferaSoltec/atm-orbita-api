# 23 · Archivos y PDF por los servicios compartidos de ATM

**Estado:** hecho (2 nov 2026). Continúa el plan 22: Orbita no solo delega las cuentas a atm-auth-api, también los archivos (atm-storage-api) y el PDF (atm-documents-api).

## Token de servicio

`internal/integrations/servicetoken`: pide `POST {AUTH_URL}/v1/token` con `{"client_id":"orbita","client_secret":AUTH_CLIENT_SECRET}`, lo guarda en memoria y lo renueva 60 s antes de vencer (un solo pedido aunque lleguen muchas peticiones a la vez). Nunca se registra. Si un servicio responde 401 se descarta y se reintenta una vez. Sin `AUTH_CLIENT_SECRET` la fuente queda "no configurada": el API arranca, y archivos y PDF responden 503.

## A. Archivos: atm-storage-api reemplaza a Cloudflare R2

- Se borró `integrations/storage/r2.go`, `aws-sdk-go-v2` (`go mod tidy`) y las variables `R2_*`.
- El puerto `Storage` cambió porque storage genera su propio id: `Put(ctx, ownerID, fileName, contentType, body, size) (ref, error)`; `Get`, `PresignGet` y `Delete` reciben esa referencia; `DeletePrefix` desapareció.
- `storage.StorageAPI`: sube `POST /v1/companies/{owner}/files?scope=system&purpose=file&system=orbita` (multipart con una sola parte `file`). **Orbita no tiene empresas: el `company_id` es el id de la persona dueña.** Storage guarda el archivo como de `system:orbita`.
  - `Get` y `PresignGet` piden `GET /v1/files/{id}/download` y usan la URL firmada (vence en unos 5 minutos; los bytes se bajan de ella sin el token). Storage siempre entrega como descarga: `download=0` ya no cambia nada.
  - `Delete` llama `DELETE /v1/files/{id}` por cada id; 404 cuenta como ya borrado y un fallo no impide borrar los demás.
- `receipt_documents.storage_key` (misma columna, sin migración) guarda ese id (UUID). Una referencia que no es UUID (de R2) se trata como inexistente.
- Los tokens de servicio no pueden listar archivos, así que al eliminar la cuenta `receipts.OnUserDeleted` lee los `storage_key` de la persona en `receipt_documents` (consulta nueva `ListStorageKeysOfUser`) y los borra antes de que la cascada borre las filas.
- Se mantienen `Memory` (pruebas) y `Unavailable` (sin credenciales: `503 STORAGE_UNAVAILABLE`). El resto del comportamiento de los recibos (plan 18) no cambia.

## B. PDF del reporte

Ver [plan 15](15-reports-pdf.md): `GET /reports/summary.pdf` → `POST {DOCUMENTS_API_URL}/v1/documents/period-report`.

## Variables

| Variable | Por defecto | |
|---|---|---|
| `AUTH_URL` | `https://auth.atmosferast.com` | Nueva |
| `AUTH_CLIENT_ID` | `orbita` | Nueva |
| `AUTH_CLIENT_SECRET` | vacía | Nueva, opcional. Vacía = archivos y PDF no disponibles. **Hay que ponerla en el `.env` del servidor** |
| `STORAGE_API_URL` | `https://storage.atmosferast.com` | Nueva |
| `DOCUMENTS_API_URL` | `https://documents.atmosferast.com` | Nueva |
| `R2_ENDPOINT`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET` | — | Eliminadas |

## Desviaciones

- Los archivos que ya estuvieran en R2 no se migran: el plan 22 borró los datos de prueba y aún no había usuarios reales.
- Un fallo de storage distinto de "no configurado" sigue saliendo como `500 INTERNAL_ERROR` en las rutas de archivos, como pasaba con R2.
- Los nombres de archivo de storage se validan allá (hasta 255 caracteres, sin control); `CleanFileName` ya lo cumple.
