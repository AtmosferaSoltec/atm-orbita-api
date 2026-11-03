# 15 · Exportar el reporte a PDF

**Estado:** hecho (2 nov 2026). El PDF lo dibuja **atm-documents-api**; este repo no tiene lógica de PDF (ni librería, ni fuentes).

## Objetivo

Descargar el reporte de un periodo como PDF, con **solo el resumen** (totales y rankings), sin la lista de movimientos (guía §11).

## Pantallas de Android que cubre

- **Reportes → PDF** (`ORBITA_SPEC` §7.8).

## Módulo

`reports` (con `integrations/documents` y `integrations/servicetoken`).

## Endpoint

| Método y ruta | Query | Respuesta |
|---|---|---|
| `GET /reports/summary.pdf` | `from`, `to` | `200 application/pdf`, `Content-Disposition: attachment; filename="orbita-2026-10-01_2026-10-31.pdf"`, `Cache-Control: no-store` |

Errores: `400 VALIDATION_FAILED` (`RANGE_INVALID`), `422 REPORT_EMPTY`, `429 TOO_MANY_REQUESTS` (con `Retry-After`), `502 DOCUMENTS_UNAVAILABLE` (documents-api falló), `503 DOCUMENTS_UNAVAILABLE` (sin `AUTH_CLIENT_SECRET`).

## Cómo funciona

1. `PreparePDF` llama al **mismo caso de uso** que `GET /reports/summary` (mismas cifras, `to` recortado a hoy, mismo `RANGE_INVALID`). Además exige un periodo de hasta 366 días (inclusive, lo que acepta documents-api) y al menos un movimiento.
2. Se gasta un cupo del límite (10 por hora por persona, en memoria, `middleware.QuotaLimiter`). Un periodo inválido o vacío no gasta cupo.
3. `adapters/documents` convierte el resumen a `POST {DOCUMENTS_API_URL}/v1/documents/period-report` con el token de servicio del sistema `orbita` (el mismo de storage, ver plan 22/23):

```json
{"from":"2026-10-01","to":"2026-10-31","currencies":[{"currency":"PEN","income_total":"3200.00","expense_total":"1954.20","balance":"1245.80",
  "expenses":[{"name":"Comida","total":"700.00","percent":"35.8"}],"incomes":[{"name":"Sueldo","total":"3200.00","percent":"100.0"}]}]}
```

   Dinero como texto con 2 decimales (el balance puede ser negativo), porcentaje con 1, nombres de hasta 80 caracteres (se recortan con "…"), hasta 12 monedas y 50 filas por ranking: si un ranking pasa de 50, las últimas se juntan en una fila "Otros" que suma sus totales, para que todo siga cuadrando.
4. Los bytes del PDF se devuelven tal cual (se verifica que empiecen con `%PDF-`). El nombre del archivo sale solo de las fechas validadas. **No se guarda nada** en el servidor ni en storage.

## Reglas

- El contenido, el idioma, la fuente y los formatos (`S/ 1,245.80`, `2 oct 2026`, `37.2%`) son de documents-api.
- Cualquier fallo de documents-api (red, 4xx/5xx, respuesta que no es PDF) se responde `502 DOCUMENTS_UNAVAILABLE` con un mensaje fijo en español; su cuerpo nunca se reenvía y en el log solo van el estado y su `code`.
- Sin `AUTH_CLIENT_SECRET` la ruta responde `503 DOCUMENTS_UNAVAILABLE` antes de tocar la base.

## Pruebas

- `integrations/servicetoken`: token reutilizado, renovado antes de vencer, un solo pedido en concurrencia, error de auth, reintento tras 401.
- `reports/adapters/documents`: formato de dinero y porcentaje, "Otros", recorte de nombres y monedas.
- Integración (Postgres + documents-api falso, `internal/modules/ledger/reports_pdf_test.go`): cifras iguales al resumen, `to` recortado y nombre del archivo, validaciones, periodo vacío, 502 sin filtrar el cuerpo, límite por persona.

## Desviaciones respecto al plan original

| Qué | Motivo |
|---|---|
| Sin librería de PDF en Go ni fuente Manrope en este repo | El dibujo lo hace atm-documents-api (compartido con otros sistemas) |
| El nombre es `orbita-<from>_<to>.pdf`, no `orbita-2026-09.pdf` | Un periodo puede ser cualquier rango |
| Periodo máximo de 366 días para el PDF (el resumen JSON sigue aceptando hasta 10 años) | Límite de documents-api |
| `Content-Disposition` se expone en CORS | Para que la web lea el nombre del archivo |

## Variables nuevas para el servidor

`DOCUMENTS_API_URL` (por defecto `https://documents.atmosferast.com`) y `AUTH_CLIENT_SECRET` (ver plan 23).
