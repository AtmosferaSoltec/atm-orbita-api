# 08 · Inicio

## Objetivo

Entregar en **una sola petición** todo lo que muestra Inicio: el ahorro total en las dos monedas, el tipo de cambio, los totales del mes, los últimos movimientos y el aviso de tarjetas de crédito. Menos viajes de red al abrir la app.

**Depende de la pregunta P4.** Si se decide no hacer el endpoint, este plan se reduce a dejar Inicio leyendo los endpoints de cada módulo (como hoy en `HomeViewModel`) y no hay nada que construir en el API.

## Pantallas de Android que cubre

- **Inicio** (`ui/feature/home`; `ORBITA_SPEC` §7.2).
- Hoy `HomeViewModel` combina 5 repositorios: cuentas, ajustes, recientes, reporte del mes y compras pendientes. Con este plan pasa a un `HomeRepository.observeHome()` nuevo en `domain/repository` (cambio de Android, ver plan 13).

## Módulo

`home`. Sin tablas: compone las interfaces públicas de `profile` (`ProfileReader`), `ledger` (`AccountReader`, `EntriesReader`), `reports` (`ReportReader`) y `credit` (`CreditSummary`). No importa paquetes internos de ninguno.

## Endpoints

| Método y ruta | Respuesta |
|---|---|
| `GET /home` | `200 { data: home }` |

```json
{
  "data": {
    "today": "2026-10-02",
    "settings": { "main_currency": "PEN", "secondary_currency": "USD", "display_currency": "PEN",
                  "fx_configured": true, "rate": "3.200000", "fx_mode": "manual" },
    "savings": { "included_count": 4, "active_count": 5,
                 "totals": [ { "currency": "PEN", "amount": "7166.30" }, { "currency": "USD", "amount": "2239.47" } ] },
    "month": { "from": "2026-10-01", "to": "2026-10-02", "currency": "PEN",
               "income_total": "3500.00", "expense_total": "30.50" },
    "recent_entries": [ "… 4 entries, como en GET /entries …" ],
    "credit": { "pending_count": 4, "next_due_date": "2026-10-05", "has_urgent": true }
  }
}
```

- `savings` igual que en `GET /accounts` (plan 04); sin tipo de cambio, solo la moneda principal.
- `month`: el mes calendario actual según la zona del perfil, en la **moneda principal** (como `HomeViewModel`: toma el reporte de esa moneda). Excluye transferencias y compras pendientes.
- `recent_entries`: los 4 más recientes, movimientos y transferencias mezclados (`RECENT_ENTRIES = 4`).
- `credit`: `null` si no hay compras pendientes (Android oculta la tarjeta). `pending_count` cuenta las de todas las tarjetas; `has_urgent` si alguna vence en 3 días o menos o ya venció.
- `today` lo calcula el API con la zona del perfil; Android lo usa en lugar del reloj del teléfono para las validaciones de fecha (regla "hoy lo decide el API").

### Errores

Solo los generales (`UNAUTHORIZED`, `INTERNAL_ERROR`).

## Tablas y migraciones

Ninguna.

## Reglas

- Las cifras son **exactamente** las de los endpoints de cada módulo para el mismo instante; el endpoint no tiene reglas propias, solo compone.
- Las lecturas van en paralelo (`errgroup`) con el contexto de la petición; si una falla, falla la respuesta (no se entrega un Inicio a medias).

## Dependencias

04, 05, 06, 07 y 09 (resumen de crédito). Se implementa después del 09.

## Pasos de implementación

1. Interfaces públicas que falten en cada módulo (`EntriesReader.Recent`, `CreditSummary`).
2. Módulo `home`: caso de uso que compone y adapter HTTP.
3. Android: `HomeRepository` en `domain`, `DemoHomeRepository` (arma lo mismo con los repositorios de demo, para que el modo demo siga funcionando) y `RemoteHomeRepository`; `HomeViewModel` pasa a usarlo.
4. OpenAPI y `ORBITA_SPEC.md` §7.2.

## Pruebas

Integración con los datos de ejemplo (hoy 2 oct 2026):
- Ahorro total S/ 7,166.30 y US$ 2,239.47; 4 de 5 cuentas incluidas.
- Octubre: ingresos 3,500.00, gastos 30.50.
- Recientes: Almuerzo, Taxi al trabajo, Sueldo, Cambio de dólares, en ese orden.
- Crédito: 4 pendientes, vence 5 oct, urgente.
- Usuario nuevo: ahorro 0.00 solo en la principal, `fx_configured: false`, sin recientes, `credit: null`.

## Criterios de aceptación

- Inicio se carga con una sola petición y muestra lo mismo que el modo demo con los datos de ejemplo.

## Variables nuevas para el servidor

Ninguna.

## Desviaciones

(Se completan al implementar.)
