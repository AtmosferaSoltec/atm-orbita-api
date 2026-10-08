# 07 · Fase 4 — Reportes e Inicio

**Objetivo:** responder "cuánto entró, cuánto salió y en qué" para cualquier periodo, y entregar en una sola petición todo lo que muestra la pantalla de Inicio.

**Tamaño:** mediano. **Depende de:** Fase 3. **Bloquea:** Fase 7 (PDF).

Pantallas de Android cubiertas: Reportes e Inicio (secciones 7.8 y 7.2 de `ORBITA_SPEC.md`). Equivale a `ReportsRepository` y a la combinación de flujos de `HomeViewModel`.

## 1. Reporte de un periodo

`GET /reports/summary?from=2026-09-01&to=2026-09-30`

```json
{
  "period": { "from": "2026-09-01", "to": "2026-09-30" },
  "currencies": [
    {
      "currency": "PEN",
      "incomeTotal": "4200.00",
      "expenseTotal": "2148.60",
      "balance": "2051.40",
      "expenses": [
        { "category": { "id": "…", "name": "Vivienda", "color": "#1D4ED8" },
          "total": "800.00", "percent": "37.2", "movements": 1 },
        { "category": { "id": "…", "name": "Alimentación", "color": "#0E7490" },
          "total": "612.40", "percent": "28.5", "movements": 4 }
      ],
      "incomes": [
        { "category": { "id": "…", "name": "Sueldo", "color": "#0B7A5A" },
          "total": "3500.00", "percent": "83.3", "movements": 1 }
      ]
    }
  ]
}
```

Reglas (las de `DemoState.report` y de las funciones `report_*` del esquema original):

- **Solo movimientos.** Las transferencias y las compras con tarjeta pendientes no cuentan. Una compra aparece cuando se paga, con la fecha del pago.
- **Un bloque por moneda**, nunca mezcladas. La moneda es la de la cuenta del movimiento.
- **Orden de los bloques:** la moneda principal del usuario primero; el resto, alfabético.
- **Orden de las categorías:** de mayor a menor total.
- `balance = incomeTotal − expenseTotal` (puede ser negativo).
- `percent = total × 100 ÷ total de ese tipo en esa moneda`, con 1 decimal, *half up*. Lo calcula el servidor para que todas las plataformas muestren lo mismo.
- Los movimientos de cuentas y categorías **archivadas** siguen contando.
- Los movimientos eliminados no cuentan.
- Un periodo sin movimientos devuelve `"currencies": []`.
- Si un tipo no tiene movimientos, su lista va vacía y su total es `"0.00"`.

Validaciones:

| Regla | Código |
|---|---|
| `from` y `to` obligatorios, con formato de fecha | `INVALID_FORMAT` |
| `from <= to` | `INVALID_FORMAT` |
| Rango de como máximo 10 años (configurable) | `RANGE_TOO_LARGE` |

El servidor **no** rechaza un `to` posterior a hoy: no existen movimientos futuros, así que el resultado es el mismo. Recortar el periodo a hoy ("1 ene – hoy") es tarea del cliente.

Los tres modos de Android (Mes, Año, Rango) usan este mismo endpoint con fechas distintas.

Implementación: la consulta agregada de [02](02-modelo-de-datos.md), sección 6, que agrupa en la base por moneda, tipo y categoría. El servicio solo ordena, suma totales y calcula porcentajes. **Nunca** se traen los movimientos a memoria para sumarlos.

## 2. Inicio

`GET /home` devuelve lo que la pantalla combina hoy de cinco fuentes distintas.

```json
{
  "today": "2026-10-02",
  "settings": {
    "mainCurrency": "PEN", "secondaryCurrency": "USD", "displayCurrency": "PEN",
    "fx": { "from": "USD", "to": "PEN", "rate": "3.200000", "source": "manual" }
  },
  "savings": {
    "includedCount": 4, "totalCount": 5,
    "main":      { "currency": "PEN", "total": "7166.30" },
    "secondary": { "currency": "USD", "total": "2239.47" }
  },
  "month": {
    "period": { "from": "2026-10-01", "to": "2026-10-31" },
    "main": { "currency": "PEN", "incomeTotal": "3500.00", "expenseTotal": "30.50" },
    "others": []
  },
  "credit": {
    "pendingCount": 4,
    "nextDueDate": "2026-10-05",
    "debt": [ { "currency": "PEN", "total": "525.90" }, { "currency": "USD", "total": "12.00" } ]
  },
  "recentEntries": [ ]
}
```

- `today` es el día según la zona horaria del usuario; le sirve al cliente para detectar un reloj desajustado.
- `month` es el mes calendario actual. `main` es lo que Android muestra hoy (solo la moneda principal); `others` trae las demás monedas con movimientos, por si la interfaz decide mostrarlas.
- `credit` llega vacío (`pendingCount: 0`, `debt: []`) hasta que exista la Fase 5.
- `recentEntries` son las 5 entradas más recientes, con el mismo formato de `GET /entries`. El número se puede pedir con `?recent=4`, con máximo 20.

Implementación: el servicio de `home` llama a los servicios de `accounts`, `settings`, `reports`, `entries` y `credit` y ejecuta sus consultas en paralelo. No duplica reglas: cada cifra sale del mismo servicio que la calcula para su propia pantalla.

## 3. Frescura

Los dos endpoints llevan `ETag`. La app, al volver a Inicio, envía `If-None-Match` y recibe `304` si nada cambió, sin transferir el cuerpo.

No hay caché en el servidor en esta fase. Si las mediciones lo piden, el primer candidato es guardar el reporte de **meses ya cerrados**, que solo cambia si se edita un movimiento antiguo ([13](13-escalabilidad-y-operacion.md)).

## 4. Tareas

- [ ] Módulo `reports`: consulta agregada, orden, totales, porcentajes
- [ ] Validación del rango
- [ ] Módulo `home`: composición en paralelo
- [ ] `ETag` en ambos endpoints
- [ ] Verificar con `EXPLAIN` que el reporte usa el índice por usuario y fecha
- [ ] Datos de carga para medir: un usuario con 100,000 movimientos repartidos en 10 años

## 5. Pruebas

Con la semilla de ejemplo:

| Caso | Esperado |
|---|---|
| Septiembre 2026 | Ingresos **4,200.00**, gastos **2,148.60**, balance **2,051.40** |
| Gastos de septiembre, orden | Vivienda, Alimentación, Transporte, Ocio, Otros, Salud |
| Gastos de septiembre, porcentajes | **37.2 / 28.5 / 10.0 / 8.8 / 8.5 / 6.9** |
| Ingresos de septiembre | Sueldo 3,500.00 · Freelance 600.00 · Otros ingresos 100.00 |
| Octubre 2026 | Ingresos **3,500.00**, gastos **30.50** (la transferencia del 1 oct no cuenta) |
| Año 2026 | Ingresos **7,700.00**, gastos **2,179.10** |
| Agosto 2026 | `currencies: []` |
| Un movimiento en una cuenta en USD | Aparece un segundo bloque, después del de PEN |
| El usuario cambia su moneda principal a USD | El bloque de USD pasa a ir primero |
| Solo ingresos en el periodo | `expenses: []`, `expenseTotal: "0.00"` |
| Se elimina un movimiento | Deja de contar |
| Se archiva la categoría Ocio | Sus movimientos siguen en el reporte |
| `from` posterior a `to` | `400` |
| Rango de 11 años | `422 RANGE_TOO_LARGE` |

Inicio:

- `savings` da 7,166.30 y 2,239.47, con "4 de 5".
- `month.main` da 3,500.00 y 30.50.
- `recentEntries` trae las más recientes, mezclando movimientos y transferencias.
- Con la Fase 5: `pendingCount: 4`, `nextDueDate: 2026-10-05`.

Seguridad y rendimiento:

- El reporte de A nunca incluye movimientos de B, aunque compartan fechas y nombres de categoría.
- Con 100,000 movimientos, el reporte de un año responde por debajo del objetivo de [13](13-escalabilidad-y-operacion.md).

## 6. Criterios de aceptación

- Con los datos de ejemplo se obtienen exactamente las cifras del criterio de la Fase 5 de Android: ahorro total S/ 7,166.30 y US$ 2,239.47; septiembre con 4,200.00, 2,148.60 y 2,051.40.
- Inicio se pinta con una sola petición.
- El reporte se calcula en la base, sin cargar movimientos en memoria.

## 7. Riesgos

| Riesgo | Mitigación |
|---|---|
| Los porcentajes calculados en el servidor difieren de los de Android por el orden de las operaciones | Misma fórmula que `percentOf` y los mismos vectores de prueba |
| `GET /home` se vuelve lento porque junta cinco consultas | Se ejecutan en paralelo y cada una usa su índice; se mide por separado |
| Los porcentajes de un bloque no suman exactamente 100.0 | Es el comportamiento esperado al redondear cada uno; no se "ajusta" ninguno |
