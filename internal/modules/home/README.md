# Módulo home

Inicio en **una sola petición**: el ahorro total, las monedas y el tipo de cambio, los totales del mes, los últimos movimientos y el aviso de tarjetas. Plan: [`docs/plans/08-home.md`](../../../docs/plans/08-home.md).

## Tablas

Ninguna, y ninguna regla propia: compone lo que exponen los demás módulos.

| Lee | De |
|---|---|
| Monedas, tipo de cambio y "hoy" | `profile.Reader` |
| Ahorro total y últimos movimientos, ya con la forma JSON de `GET /accounts` y `GET /entries` | `ledger.HomeReader` |
| Totales del mes | `reports.Reader` |
| Aviso de tarjetas | `credit.CreditSummary` |

## API pública (`module.go`)

| Qué | Para quién |
|---|---|
| `New(profile.Reader, ledger.HomeReader, reports.Reader, credit.CreditSummary)`, `Routes(protected)` | `cmd/api/main.go` |

## Reglas

- Las cifras son exactamente las de cada endpoint en el mismo instante.
- "Hoy" y los ajustes se leen primero (el mes depende de hoy); lo demás va en paralelo (`errgroup`). Si una lectura falla, falla la respuesta: nunca un Inicio a medias.
- `month`: del día 1 del mes en curso (zona del perfil) a hoy, en la moneda principal.
- `recent_entries`: las 4 filas más recientes de la lista, movimientos y transferencias.
- `credit`: `null` sin compras pendientes.

## Endpoints

`GET /home`.

## Errores

Solo los generales (`UNAUTHORIZED`, `INTERNAL_ERROR`).
