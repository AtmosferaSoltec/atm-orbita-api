# Módulo reports

El reporte de un periodo (mes, año o rango): ingresos, gastos y balance, con el ranking de gasto por categoría y las fuentes de ingreso, **por moneda y sin mezclar**, con los porcentajes ya calculados para que todas las plataformas muestren lo mismo. Plan: [`docs/plans/07-reports.md`](../../../docs/plans/07-reports.md).

## Tablas

Ninguna. Las sumas las hace `ledger` en la base (`ledger.ReportQuery`, una consulta agrupada por moneda de la cuenta, tipo y categoría); la moneda principal y "hoy" los da `profile`.

## API pública (`module.go`)

| Qué | Para quién |
|---|---|
| `New(ledger.ReportQuery, profile.Reader)`, `Routes(protected)` | `cmd/api/main.go` |

## Reglas

- Solo movimientos: no cuentan las transferencias ni las compras con tarjeta pendientes. Sin lo eliminado. Las categorías archivadas siguen apareciendo con sus movimientos.
- Por moneda de la cuenta, sin convertir: la principal primero, luego las demás por código.
- Porcentaje = total de la categoría × 100 ÷ total de esa moneda y tipo, 1 decimal, HALF_UP; 0 si el total es cero (`percentOf` de Android).
- Ranking de mayor a menor total; empates por nombre.
- `to` se recorta a hoy (zona del perfil). Un periodo vacío da `currencies: []`; una moneda solo con ingresos trae `expenses: []`.

## Endpoints

`GET /reports/summary?from&to`.

## Errores

| `code` | HTTP | Cuándo |
|---|---|---|
| `VALIDATION_FAILED` + `RANGE_INVALID` | 400 | Fecha mal formada o ausente, `from` posterior a `to`, o un periodo de 10 años o más |
