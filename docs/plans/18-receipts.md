# Plan 18 · Recibos por honorarios

**Estado:** hecho (24 oct 2026). Migración `00012` aplicada en la base del VPS desde local por el túnel SSH; probado de punta a punta desde la web con el XML real de SUNAT y R2. Módulo **solo de la web** (`atm-orbita-web`, sección Recibos): no tiene pantallas en Android. Usa la misma cuenta.

## Objetivo

Registrar los recibos por honorarios electrónicos (RHE de SUNAT) que emite la persona, compararlos contra un **tope anual** que ella define y guardar con cada recibo su **XML** y, si quiere, su **PDF** en Cloudflare R2.

## Módulo y dependencias

- Módulo nuevo **`receipts`** (independiente de `ledger`; no toca sus tablas).
- Lee "hoy" de `profile.Reader` (por su interfaz pública, como `credit`).
- Integración nueva **`internal/integrations/storage`**: puerto `Storage` + implementación R2 (S3 de `aws-sdk-go-v2`) + una en memoria para las pruebas.
- Se engancha en `identity.OnUserDeleted`: al eliminar la cuenta borra sus archivos de R2 (la cascada solo alcanza a las filas).

## Reglas (las mismas que la web, `src/lib/domain/receipts.ts`)

- Serie y número obligatorios; la pareja (serie sin distinguir mayúsculas + número) es **única por usuario**. El número se guarda sin ceros a la izquierda (`00058` → `58`).
- Fecha de emisión obligatoria y **no posterior a hoy** (zona horaria del perfil).
- Cliente: nombre obligatorio (hasta 200). Documento `RUC` (11 dígitos), `DNI` (8 dígitos) u `OTRO` (texto libre, hasta 15).
- Monto bruto > 0. Retención ≥ 0 y **menor que el bruto** (se guarda tal cual viene del XML; SUNAT ya la calcula). Neto = bruto − retención.
- Descripción hasta 500. Fecha de cobro opcional, no anterior a la emisión ni posterior a hoy.
- Origen: `xml` o `manual`.
- **El tope no bloquea**: pasarlo solo se informa. Es por año; un año sin tope usa **S/ 50,000.00**.
- Archivos: a lo más **un XML y un PDF** por recibo. XML hasta 1 MB y su `cbc:ID` debe ser el del recibo; PDF hasta 5 MB y debe empezar con `%PDF-`. Subir otro del mismo tipo reemplaza al anterior.

## Tablas · migración `00012_receipts_create_receipts.sql`

```sql
receipts (
  id UUID PK, user_id UUID → users ON DELETE CASCADE,
  series VARCHAR(4), number VARCHAR(8), issue_date DATE,
  client_name VARCHAR(200), client_doc_type VARCHAR(4) CHECK IN ('RUC','DNI','OTRO'), client_doc_number VARCHAR(15),
  description VARCHAR(500), gross NUMERIC(14,2) CHECK > 0, withholding NUMERIC(14,2) CHECK >= 0 AND < gross,
  collected_on DATE NULL, source VARCHAR(10) CHECK IN ('xml','manual'), created_at, updated_at
)
UNIQUE (user_id, upper(series), number) · INDEX (user_id, issue_date DESC)

receipt_year_limits (user_id → users ON DELETE CASCADE, year SMALLINT, amount NUMERIC(14,2) CHECK > 0, updated_at, PK (user_id, year))

receipt_documents (
  id UUID PK, receipt_id → receipts ON DELETE CASCADE, user_id → users ON DELETE CASCADE,
  kind VARCHAR(3) CHECK IN ('xml','pdf'), file_name VARCHAR(255), size_bytes INTEGER, content_type VARCHAR(100),
  storage_key VARCHAR(300) UNIQUE, uploaded_at, UNIQUE (receipt_id, kind)
)
```

Llave en R2: `receipts/{user_id}/{receipt_id}/{document_id}.{xml|pdf}` (con el usuario delante, para borrar todo lo suyo por prefijo).

## Endpoints (todos con sesión)

| Método y ruta | Qué hace | Errores |
|---|---|---|
| `GET /receipts?year=2026` | Recibos del año (fecha desc.), cada uno con sus `documents` | `400` año inválido |
| `GET /receipts/years` | `{ "years": [2026, 2025], "current_year": 2026 }`: años con recibos + el actual | — |
| `GET /receipts/:id` | Un recibo | `404` |
| `POST /receipts` | Crea; acepta el `id` del cliente (reintentar no duplica: mismo id → `200`) | `400` validación · `409 RECEIPT_ALREADY_EXISTS` · `409 ID_CONFLICT` |
| `PATCH /receipts/:id` | Cambia los datos (mismo cuerpo que crear) | `400` · `404` · `409 RECEIPT_ALREADY_EXISTS` |
| `PUT /receipts/:id/collected` | `{ "collected_on": "2026-10-24" }` o `null` | `400` · `404` |
| `DELETE /receipts/:id` | Borra el recibo y sus archivos | `404` |
| `GET /receipt-limits/:year` | `{ "year", "amount", "is_default" }` | `400` |
| `PUT /receipt-limits/:year` | `{ "amount": "48000.00" }` | `400 AMOUNT_INVALID` |
| `PUT /receipts/:id/documents/:kind` | `multipart/form-data`, campo `file`. Sube a R2 y reemplaza el anterior | `400 FILE_INVALID` · `413` · `404` · `422 XML_RECEIPT_MISMATCH` · `503 STORAGE_UNAVAILABLE` |
| `GET /receipts/:id/documents/:kind/url` | URL firmada de R2 por 5 minutos: `{ "url", "expires_in" }`. `?download=1` la entrega como descarga | `404` |
| `GET /receipts/:id/documents/:kind/content` | El archivo, para verlo **dentro** de la web (XML formateado, PDF embebido). `Cache-Control: private, no-store` | `404` · `503` |
| `DELETE /receipts/:id/documents/:kind` | Quita el archivo | `404` |

Cuerpo de un recibo (crear/editar y respuesta):

```json
{
  "id": "0193…", "series": "E001", "number": "109", "issue_date": "2026-09-07",
  "client": { "name": "SISTEMAS Y SOLUCIONES EMPRESARIALES S.A.C.", "doc_type": "RUC", "doc_number": "20492684679" },
  "description": "SERVICIO DE PROGRAMACION…", "gross": "2000.00", "withholding": "0.00", "net": "2000.00",
  "collected_on": null, "source": "xml",
  "documents": [{ "id": "…", "kind": "xml", "file_name": "RHE10744555189E001109.xml", "size_bytes": 5120, "uploaded_at": "…" }],
  "created_at": "…", "updated_at": "…"
}
```

Códigos de campo (validación): `SERIES_REQUIRED`, `NUMBER_REQUIRED`, `ISSUE_DATE_REQUIRED`, `DATE_IN_FUTURE`, `CLIENT_NAME_REQUIRED`, `CLIENT_NAME_TOO_LONG`, `DOC_TYPE_INVALID`, `RUC_INVALID`, `DNI_INVALID`, `DOC_NUMBER_TOO_LONG`, `AMOUNT_INVALID`, `WITHHOLDING_INVALID`, `DESCRIPTION_TOO_LONG`, `COLLECTED_BEFORE_ISSUE`, `SOURCE_INVALID`, `INVALID_FORMAT`.

## Archivos y R2

- **Subida a través del API** (multipart): así el API valida el contenido (XML del recibo, PDF real) antes de guardarlo. El límite general de 1 MB del cuerpo se amplía a 6 MB **solo** en esa ruta.
- **Descarga y "abrir en otra pestaña" con URL firmada** de corta duración (AGENT_GUIDE §9): el navegador baja el archivo directo de R2 (una navegación no necesita CORS).
- **Ver dentro de la página** (XML formateado, PDF embebido): `…/content`, servido por el API (ver desviaciones).
- Borrar un recibo, un archivo o la cuenta borra los objetos **después** de confirmar la transacción; si R2 falla se registra en el log (queda un objeto huérfano, nunca un dato a medias).
- Sin `R2_*` configurado (pruebas), las rutas de archivos responden `503 STORAGE_UNAVAILABLE` y el resto funciona.

## Pruebas

- Dominio: cada regla de validación, número sin ceros, lectura del `cbc:ID` del XML, firma `%PDF-`.
- Integración (Postgres local + storage en memoria): crear/editar/borrar, unicidad serie-número por usuario (otro usuario sí puede), idempotencia por id, aislamiento entre usuarios, tope por defecto y propio, subir/reemplazar/borrar archivos, XML de otro recibo → `422`.

## Criterios de aceptación

- Desde la web: registrar un recibo con el XML real de SUNAT, adjuntar el PDF, verlos, descargarlos, editar, marcar cobrado, cambiar el tope y borrar; todo persiste en la base del VPS y los archivos en R2.
- Android sigue funcionando igual (no usa nada de esto).

## Desviaciones

| Qué | Motivo |
|---|---|
| `GET …/documents/:kind/content` sirve el archivo a través del API, además de la URL firmada | Para leer el XML con `fetch` (y embeber el PDF) desde la web, R2 tendría que responder con CORS. La llave de R2 del `.env` solo tiene permisos de objetos: `GetBucketCors`/`PutBucketCors` dan `403 AccessDenied`. Los archivos son chicos (1 MB / 5 MB) y la ruta exige sesión y `no-store`. Si más adelante se configura CORS en el bucket desde el panel de Cloudflare, la web puede volver a usar solo la URL firmada |
| `scripts/dev.sh` | Para correr el API en local contra la base del VPS por túnel SSH (pedido del usuario); carga `.env` sin romperse con valores como `MAIL_FROM` |
