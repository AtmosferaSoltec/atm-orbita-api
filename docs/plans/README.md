# Planes de implementación · Orbita API (Go + Gin)

Planes para construir el backend real de la app Android `atm-orbita-android`, siguiendo [`AGENT_GUIDE.md`](../../AGENT_GUIDE.md) (arquitectura, contrato, despliegue y decisiones) y [`CLAUDE.md`](../../CLAUDE.md) (ramas).

- Escritos el 21 oct 2026, tras revisar completo el proyecto Android: sus documentos (`docs/01`…`06`, `ORBITA_SPEC.md`, `CLAUDE.md`), el dominio, `data/demo`, los ViewModels, las pantallas y sus pruebas.
- **Fuente de verdad de las reglas:** el código de Android (`data/demo` y `domain`). Donde sus documentos dicen otra cosa, manda el código y la diferencia queda anotada en el plan.
- Estado: **en revisión.** Decisiones del 21 oct 2026 incorporadas (abajo). No se implementa nada hasta que el usuario apruebe estos planes.
- Rama: todo en `develop`. Flujo `develop` → `main` → `production`; promover lo decide el usuario.

## Orden y dependencias

Un plan = una funcionalidad que se entrega, se prueba y se revisa sola. Se ejecutan **en este orden y de a uno**; al terminar cada uno se corren sus pruebas, se resume y se pide revisión.

| # | Plan | Módulo(s) | Pantallas de Android | Depende de | Estado |
|---|---|---|---|---|---|
| 00 | [Fundaciones](00-foundations.md) | `platform`, `integrations/mail` | — | — | Por aprobar |
| 01 | [Registro, inicio y cierre de sesión](01-identity-sessions.md) | `identity` | Iniciar sesión, Registro, sesión persistente | 00 | Por aprobar |
| 02 | [Perfil, monedas y tipo de cambio](02-profile-settings.md) | `profile` | Ajustes (monedas, tipo de cambio), Registro (moneda) | 01 | Por aprobar |
| 03 | [Categorías](03-ledger-categories.md) | `ledger` | Categorías, Nueva/Editar categoría | 01, 02 | Por aprobar |
| 04 | [Cuentas](04-ledger-accounts.md) | `ledger` | Cuentas, Nueva/Editar cuenta | 02, 03 | Por aprobar |
| 05 | [Movimientos (ingresos y egresos)](05-ledger-transactions.md) | `ledger` | Nuevo/Editar movimiento, Lista de movimientos | 04 | Por aprobar |
| 06 | [Transferencias](06-ledger-transfers.md) | `ledger` | Transferir / Editar transferencia | 05 | Por aprobar |
| 07 | [Reportes](07-reports.md) | `reports` | Reportes (Mes, Año, Rango) | 05 | Por aprobar |
| 08 | [Inicio](08-home.md) | `home` | Inicio | 04–07, 09 | Por aprobar |
| 09 | [Tarjetas y compras pendientes](09-credit-cards-purchases.md) | `credit` | Tarjetas de Crédito, Nueva/Editar tarjeta, compra con tarjeta | 03, 04 | Por aprobar |
| 10 | [Pagar una compra con tarjeta](10-credit-payments.md) | `credit`, `ledger` | Marcar como pagada; editar/eliminar el egreso de un pago | 05, 09 | Por aprobar |
| 11 | [Recuperar contraseña](11-identity-password-recovery.md) | `identity`, `integrations/mail` | ¿Olvidaste tu contraseña? (3 pantallas) | 01 | Por aprobar |
| 12 | [Eliminar cuenta](12-identity-delete-account.md) | `identity` + todos | Ajustes → Eliminar cuenta | 01–10 | Por aprobar |
| 13 | [Integración con Android (transversal)](13-android-integration.md) | — | Todas | Se aplica en cada plan | Por aprobar |
| 14 | [Tipo de cambio automático](14-fx-automatic.md) | `profile` | Ajustes → Automático | 02 | Fase tardía, sin proveedor |
| 15 | [Exportar reporte a PDF](15-reports-pdf.md) | `reports` | Reportes → PDF | 07 | Fase tardía |

```
00 ─▶ 01 ─▶ 02 ─▶ 03 ─▶ 04 ─▶ 05 ─┬─▶ 06
       │                     │    ├─▶ 07 ─┐
       │                     └─▶ 09 ─▶ 10  ├─▶ 08
       ├─▶ 11                             │
       └──────────── 12 (cuando existan todas las tablas de usuario)
```

Con los planes 00 a 12 el API cubre todo lo que Android hace hoy en modo demo, más los dos requisitos de salida a producción (11 y 12). El 13 no es una entrega aparte: describe la base común de Android (cliente HTTP, sesión, errores) que se construye en el plan 01 y cómo cada plan conecta su repositorio.

**Por qué este orden.** El registro tiene que dejar al usuario con su perfil, sus 9 categorías y la cuenta "Efectivo". En lugar de esperar a que existan todos esos módulos, el plan 01 crea al usuario y define un **gancho de alta** (`OnUserRegistered`) que corre dentro de la misma transacción. Los planes 02, 03 y 04 se enganchan ahí. Así Android puede registrarse de verdad desde el plan 01, y cada plan siguiente completa el alta.

## Módulos

| Módulo | Tablas | Expone a otros módulos |
|---|---|---|
| `identity` | `users`, `sessions`, `refresh_tokens`, `password_resets` | `OnUserRegistered` / `OnUserDeleted` (ganchos) |
| `profile` | `profiles`, `exchange_rates` | `ProfileReader` (monedas, tipo de cambio, zona horaria, "hoy") |
| `ledger` | `categories`, `accounts`, `transactions`, `transfers` | `CategoryReader`, `AccountReader`, `PaymentLedger` (crear, editar y revertir el egreso de un pago), `EntriesReader` |
| `credit` | `credit_cards`, `credit_purchases` | `CreditSummary` (para Inicio), `PaymentReverter` (lo llama `ledger` al eliminar el egreso de un pago) |
| `reports` | ninguna: lee por la interfaz de `ledger` | `ReportReader` |
| `home` | ninguna | — |

Reglas de dependencia: las de `AGENT_GUIDE.md` §3. Un módulo nunca importa paquetes internos de otro; se cablean en `cmd/api/main.go` por interfaces pequeñas. Se verifica con `depguard` y una prueba de arquitectura (plan 00).

> **`reports` y los JOIN.** La guía prohíbe consultar tablas de otro módulo. Los reportes suman `transactions` agrupadas por categoría y moneda de la cuenta, todas tablas de `ledger`. Por eso la consulta del reporte **vive en `ledger`** y `reports` la consume por la interfaz `ReportQuery` que expone `ledger`. `reports` agrega las reglas de presentación (porcentajes, orden, moneda principal primero). Ver plan 07.

## Decisiones del usuario (21 oct 2026)

| # | Tema | Decisión | Afecta a |
|---|---|---|---|
| P1 | Recuperar contraseña | **Código de 6 dígitos**, en 3 pasos: pedir el correo → escribir el código → contraseña nueva. Sin enlace, sin App Links ni página web | 11, 13 |
| P2 | Página web para eliminar la cuenta | **Se omite por ahora.** Queda pendiente antes de publicar en Google Play | 12 |
| P3 | Revocación inmediata | **Sí**: el middleware comprueba en cada petición que la sesión siga viva | 01 |
| P4 | `GET /home` | **Sí**, Inicio en una petición | 08 |
| P5 | Verificación de correo | **Después**; no se lanza con ella | 11 |
| P6 | Binario `api` en el repo | **Se quita** en `develop` y se ignora en git y Docker | 00 |
| P7 | Rama de Android | **`develop`** también en `atm-orbita-android` | 13 |
| P8 | Documentos de Android | Se borraron **a propósito**, para **generarlos de nuevo** con esta forma de trabajo (backend Go) | 13 |
| P9 | Herramientas locales | Go instalado (`go1.27.3`). **Las pruebas apuntan al Postgres local** del usuario, en su contenedor de Docker | 00 |
## Diferencias entre los documentos de Android y su código (manda el código)

| Tema | Documentos | Código (`data/demo`, `domain`) | Qué hace el API |
|---|---|---|---|
| Moneda de una cuenta | "No se puede cambiar si ya tiene movimientos" (`docs/05` §4) | No se cambia nunca; `update` la ignora (`AccountsRepository.update`) | Nunca se cambia |
| Saldo inicial | Editable al crear | Solo al crear; `update` lo ignora | Solo al crear |
| Paginación de movimientos | "Scroll infinito con cursor" (`docs/02`) | La lista muestra un mes a la vez, 5 meses (`ORBITA_SPEC` §7.7) | `page`/`page_size` sobre un rango de fechas (contrato de la guía) |
| Tarjeta archivada al revertir un pago | No lo menciona | Eliminar el egreso de un pago **desarchiva** la tarjeta de esa compra (`deleteMovement`) | Igual: la deuda nunca queda oculta |
| Moneda de una compra al editarla | "Toma la moneda de la tarjeta nueva" | Solo si cambia de tarjeta; si no, conserva la suya aunque la tarjeta haya cambiado de moneda | Igual que el código |
| Eliminar una compra ya eliminada | No lo menciona | No es error (idempotente) | `204` |
| Mensajes del API | "El API no envía textos de interfaz" (`docs/CLAUDE.md`) | La app elige el texto por `code` | El API manda `code` (la app lo usa) y `message` en español como respaldo (guía §4) |

## Reglas que no se negocian

Las de `AGENT_GUIDE.md` §0, §1 y §11. En resumen: código en inglés y solo en español lo que ve el usuario; hexagonal y módulos independientes; esquema solo por migraciones de goose (nunca se edita una aplicada); ningún secreto en el repo; dinero como texto en JSON y `NUMERIC(14,2)` en la base; cada persona ve solo sus datos; "hoy" según la zona horaria del perfil.

## Cómo se trabaja cada plan

1. Implementar el módulo según el plan (dominio → casos de uso → adapters → `module.go` → cableado en `main.go`).
2. Pruebas: unitarias del dominio y de los casos de uso con puertos falsos; integración de los adapters de Postgres y de los endpoints contra el Postgres de desarrollo.
3. Conectar la parte de Android (plan 13).
4. Actualizar el plan (con sus desviaciones), `docs/openapi.yaml` y `ORBITA_SPEC.md` de Android.
5. Correr `go vet ./...`, `golangci-lint run`, `go test ./...` y `go build ./...`; resumir y pedir revisión.
6. Antes de pedir que se promueva a `production`: la lista de variables nuevas para el `.env` del servidor.
