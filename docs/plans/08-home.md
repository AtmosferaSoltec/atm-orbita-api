# 08 · Inicio

## Objetivo

Entregar en **una sola petición** todo lo que muestra Inicio: el ahorro total en las dos monedas, el tipo de cambio, los totales del mes, los últimos movimientos y el aviso de tarjetas de crédito. Menos viajes de red al abrir la app.

**Decisión del usuario (21 oct 2026):** se hace el endpoint.

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

## Estado (22 oct 2026)

Implementado en API y Android, en revisión. Se hizo después del plan 09, del que depende.

| Comprobación | Resultado |
|---|---|
| `gofmt`, `go vet`, `golangci-lint run`, `go build` | Sin problemas (`0 issues`) |
| `go test ./...` sin base / contra el Postgres local | Pasa. Integración por HTTP (2), con los datos de ejemplo cargados y "hoy" = 2 oct en Lima: `today` 2026-10-02; ajustes con 3.200000; `savings` idéntico al de `GET /accounts`; octubre 3,500.00 / 30.50; recientes Almuerzo, Taxi al trabajo, Sueldo, Cambio de dólares (idénticos a las 4 primeras filas de `GET /entries`); crédito 4 pendientes, vence 5 oct, urgente. Usuario nuevo en MXN: ahorro 0.00 solo en MXN, `fx_configured: false`, sin recientes, `credit: null`; sin sesión → 401 |
| OpenAPI | Válido (Redocly) |
| Android: pruebas, `lintDebug`, `installDebug` | 82 pruebas pasan (1 de `DemoHomeRepository` con las cifras del plan: S/ 7,166.30 y US$ 2,239.47, 4 de 5 cuentas, octubre 3,500.00 / 30.50, recientes en orden, 4 pendientes urgentes; 3 de `RemoteHomeRepository`); ningún aviso nuevo de lint |

**En el teléfono** (API real + Postgres local):

| Prueba | Resultado |
|---|---|
| Abrir la app | Una sola petición de datos (`GET /home`, más la renovación de la sesión); mismas cifras que antes |
| Ver el ahorro en US$ y volver a S/ | US$ 479.25 "o S/ 1,533.60 · cambio 3.20" y de vuelta |
| Registrar un gasto | Se vuelve a pedir `GET /home` y el ahorro y los recientes cambian |
| Registrar una compra con tarjeta | El aviso pasa a "2 pagos pendientes · vence 4 nov" |

## Desviaciones

- **`ledger` expone `HomeReader` (ahorro y últimos movimientos) ya con la forma JSON de `GET /accounts` y `GET /entries`**, armada por su propio adaptador HTTP, en lugar de `AccountReader` y `EntriesReader` con tipos propios. Así es imposible que Inicio y esos endpoints difieran en un campo.
- **`reports` expone `Reader.Totals`** (ingresos y gastos por moneda de un periodo) en lugar de un `ReportReader` con el reporte completo; Inicio solo usa los totales.
- **La prueba de integración compara el ahorro con el de `GET /accounts`** en lugar de esperar S/ 7,166.30 y US$ 2,239.47: cargar los datos de ejemplo por el API no reproduce los saldos finales de las 5 cuentas. Esas cifras las comprueban la prueba del plan 04 (dominio) y la de `DemoHomeRepository` en Android.
- **Android no usa `today` del API para validar fechas**: `DateProvider` sigue siendo la fecha del teléfono, que es la zona con la que se creó el perfil. Llevar "hoy" del API a todos los formularios queda para cuando haga falta (por ejemplo, si el usuario cambia de zona).
- **Android: `RemoteChanges`**. REST no avisa cuando algo cambia, así que cada repositorio remoto avisa después de escribir, e Inicio se vuelve a pedir. Las demás pantallas siguen leyendo sus propios endpoints.
- **Android: la pantalla de Inicio recibe los totales de ahorro, los conteos y el resumen de crédito** en lugar de la lista de cuentas y de compras; `DemoHomeRepository` los calcula igual en el modo demo.
- **En las pruebas**, el sembrado de los datos de ejemplo registra el "Sueldo" del 1 oct después del "Cambio de dólares", como en `SampleData.kt`.
