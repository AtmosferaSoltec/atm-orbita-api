# 07 · Reportes

## Objetivo

Entregar el reporte de un periodo (mes, año o rango): ingresos, gastos y balance, con el ranking de gasto por categoría y las fuentes de ingreso, **por moneda y sin mezclar**, con los porcentajes ya calculados para que todas las plataformas muestren lo mismo.

## Pantallas de Android que cubre

- **Reportes** con las pestañas Mes | Año | Rango (`ui/feature/reports`; `ORBITA_SPEC` §7.8).
- Las tarjetas "Ingresos · mes" y "Gastos · mes" de Inicio (plan 08 usa el mismo cálculo).
- Repositorio: `ReportsRepository.observeReport(period)` → `List<CurrencyReport>`.

## Módulo

`reports`. No tiene tablas: pide las sumas a `ledger` por la interfaz `ReportQuery` (la consulta vive en `ledger`, dueño de las tablas) y aplica las reglas de presentación.

```go
// en ledger: lo que reports necesita, nada más
type ReportQuery interface {
    TotalsByCategory(ctx context.Context, userID uuid.UUID, from, to dates.LocalDate) ([]CategoryTotal, error)
    // CategoryTotal: currency, kind, category_id, name, color, total, count
}
```

## Endpoints

| Método y ruta | Query | Respuesta |
|---|---|---|
| `GET /reports/summary` | `from`, `to` (`YYYY-MM-DD`, obligatorios) | `200 { data: { from, to, currencies: [report] } }` |

```json
{
  "data": {
    "from": "2026-09-01", "to": "2026-09-30",
    "currencies": [
      {
        "currency": "PEN",
        "income_total": "4200.00", "expense_total": "2148.60", "balance": "2051.40",
        "expenses": [ { "category": { "id": "…", "name": "Vivienda", "color": "#1D4ED8" }, "total": "800.00", "percent": "37.2" } ],
        "incomes":  [ { "category": { "id": "…", "name": "Sueldo", "color": "#0B7A5A" }, "total": "3500.00", "percent": "83.3" } ]
      }
    ]
  }
}
```

### Errores

| Situación | HTTP | `code` |
|---|---|---|
| Fecha mal formada, `from` > `to` o rango de más de 10 años | 400 | `VALIDATION_FAILED` + `RANGE_INVALID` |
| `to` posterior a hoy | — | Se recorta a hoy (no hay datos del futuro); no es error |

## Tablas y migraciones

Ninguna nueva. Usa el índice `(user_id, category_id, occurred_on)` de `transactions` (plan 05).

## Reglas

- **Solo movimientos**: no cuentan transferencias ni compras con tarjeta pendientes (estas no son movimientos hasta pagarse; el egreso del pago sí cuenta, con su fecha de pago).
- **Por moneda de la cuenta**, sin convertir. La moneda principal primero, luego las demás por código (como `DemoState.report`).
- **Sin lo eliminado** (`deleted_at`). Las categorías archivadas siguen apareciendo con los movimientos que tuvieron.
- **Porcentaje** = `total_categoría × 100 ÷ total_de_esa_moneda_y_tipo`, 1 decimal, HALF_UP; `0` si el total es cero (`percentOf` en Android).
- **Orden** de cada ranking: de mayor a menor total; empate por nombre.
- Periodo vacío → `currencies: []` (Android muestra "No hay movimientos en este periodo"). Una moneda solo con ingresos trae `expenses: []`.
- Las sumas se hacen en SQL (`SUM` sobre `NUMERIC`), sin traer filas a memoria.
- El mes y el año en curso los recorta Android hasta hoy; el API recorta igual por si otra plataforma no lo hace.

## Dependencias

05 (movimientos). Se reutiliza en 08 (Inicio).

## Pasos de implementación

1. `ReportQuery` en `ledger` (consulta sqlc agrupada por moneda de la cuenta, tipo y categoría).
2. Módulo `reports`: dominio (`CurrencyReport`, porcentajes, orden), caso de uso `Summary`, adapter HTTP.
3. Cableado en `main.go`: `reports` recibe la implementación de `ReportQuery` de `ledger`.
4. Android: `RemoteReportsRepository`. Android ya calcula el porcentaje por su cuenta; se pasa a usar el del API para que iOS y la web muestren lo mismo (o se deja el de Android, que da igual; se decide al implementar).
5. OpenAPI y `ORBITA_SPEC.md`.

## Pruebas

Unitarias: porcentajes de septiembre de los datos de ejemplo → 37.2 / 28.5 / 10.0 / 8.8 / 8.5 / 6.9; total cero → 0.

Integración (datos de ejemplo cargados):
- Septiembre 2026: ingresos 4,200.00 (Sueldo 3,500.00 · Freelance 600.00 · Otros ingresos 100.00), gastos 2,148.60, balance +2,051.40.
- Octubre al 2 oct: ingresos 3,500.00, gastos 30.50; la transferencia de US$ 20 no aparece.
- Año 2026: ingresos 7,700.00, gastos 2,179.10.
- Un movimiento en una cuenta USD genera un segundo bloque `USD`, después de `PEN`.
- Un movimiento eliminado deja de sumar.
- A no ve las sumas de B.

## Criterios de aceptación

- Reportes muestra en la app las mismas cifras que el modo demo con los datos de ejemplo.

## Variables nuevas para el servidor

Ninguna.

## Desviaciones

(Se completan al implementar.)
