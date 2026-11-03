# Módulo reports

El reporte de un periodo (mes, año o rango): ingresos, gastos y balance, con el ranking de gasto por categoría y las fuentes de ingreso, **por moneda y sin mezclar**, con los porcentajes ya calculados para que todas las plataformas muestren lo mismo. Plan: [`docs/plans/07-reports.md`](../../../docs/plans/07-reports.md).

## Tablas

Ninguna, ni PDF propio. Las sumas las hace `ledger` en la base (`ledger.ReportQuery`, una consulta agrupada por moneda de la cuenta, tipo y categoría); la moneda principal y "hoy" los da `profile`.

## API pública (`module.go`)

| Qué | Para quién |
|---|---|
| `New(ledger.ReportQuery, profile.Reader, PDFOptions)`, `Routes(protected)` | `cmd/api/main.go` (`PDFOptions`: cliente de `integrations/documents` y el límite por persona) |
| `Reader()`: `Totals(ctx, userID, from, to)` por moneda | El módulo `home` (plan 08), para los totales del mes |

## Reglas

- Solo movimientos: no cuentan las transferencias ni las compras con tarjeta pendientes. Sin lo eliminado. Las categorías archivadas siguen apareciendo con sus movimientos.
- Por moneda de la cuenta, sin convertir: la principal primero, luego las demás por código.
- Porcentaje = total de la categoría × 100 ÷ total de esa moneda y tipo, 1 decimal, HALF_UP; 0 si el total es cero (`percentOf` de Android).
- Ranking de mayor a menor total; empates por nombre.
- `to` se recorta a hoy (zona del perfil). Un periodo vacío da `currencies: []`; una moneda solo con ingresos trae `expenses: []`.

## Endpoints

`GET /reports/summary?from&to`.

`GET /reports/summary.pdf?from&to` (plan 15): usa el **mismo caso de uso** (`PreparePDF` llama a `Summary`: mismas cifras, `to` recortado a hoy, mismo `RANGE_INVALID`), lo convierte a la petición de atm-documents-api (`adapters/documents`: dinero con 2 decimales, porcentaje con 1, nombres de hasta 80 caracteres, rankings de hasta 50 filas con el resto en "Otros", hasta 12 monedas) y transmite el PDF con `Content-Disposition: attachment; filename="orbita-<from>_<to>.pdf"` y `Cache-Control: no-store`. Nada se guarda. Reglas extra: periodo de hasta 366 días (inclusive) y al menos un movimiento; 10 PDF por hora por persona (`middleware.QuotaLimiter`, en memoria; solo gasta cupo un PDF que llega a pedirse).

## Errores

| `code` | HTTP | Cuándo |
|---|---|---|
| `VALIDATION_FAILED` + `RANGE_INVALID` | 400 | Fecha mal formada o ausente, `from` posterior a `to`, o un periodo de 10 años o más |
| `RANGE_INVALID` (en `VALIDATION_FAILED`) | 400 | También en el PDF: periodo de más de 366 días |
| `REPORT_EMPTY` | 422 | PDF de un periodo sin movimientos |
| `TOO_MANY_REQUESTS` | 429 | Más de 10 PDF en una hora (trae `Retry-After`) |
| `DOCUMENTS_UNAVAILABLE` | 502 | atm-documents-api no respondió o rechazó la petición (el cuerpo de su respuesta nunca se reenvía) |
| `DOCUMENTS_UNAVAILABLE` | 503 | Falta `AUTH_CLIENT_SECRET`: el PDF está apagado |
