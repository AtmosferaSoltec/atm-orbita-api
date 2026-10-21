# Planes de implementación · Orbita API (Go + Gin)

Planes para construir el backend real de la app Android `atm-orbita-android`, siguiendo [`AGENT_GUIDE.md`](../../AGENT_GUIDE.md) (arquitectura, contrato, despliegue y decisiones) y [`CLAUDE.md`](../../CLAUDE.md) (ramas).

- Escritos el 21 oct 2026, tras revisar completo el proyecto Android: sus documentos (`docs/01`…`06`, `ORBITA_SPEC.md`, `CLAUDE.md`), el dominio, `data/demo`, los ViewModels, las pantallas y sus pruebas.
- **Fuente de verdad de las reglas:** el código de Android (`data/demo` y `domain`). Donde sus documentos dicen otra cosa, manda el código y la diferencia queda anotada en el plan.
- Estado: **en revisión.** No se implementa nada hasta que el usuario apruebe estos planes.
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

## Preguntas abiertas (necesito tu respuesta)

| # | Pregunta | Afecta a | Mi recomendación |
|---|---|---|---|
| P1 | **Recuperar contraseña: ¿enlace o código de 6 dígitos?** La guía (§8) y Android piden un **enlace** de un solo uso de 30 minutos. El 16 oct 2026, en el proyecto anterior (NestJS), decidiste usar un **código de 6 dígitos**. Con enlace, el correo tiene que abrir la app (App Links con un dominio verificado) y hace falta una página web de respaldo; con código, todo pasa dentro de la app | 11, 13 | Código de 6 dígitos: no necesita App Links ni página web, y la pantalla "Enlace enviado" pasa a pedir el código |
| P2 | **Página web para eliminar la cuenta** (la exige Google Play). El proyecto web está fuera de este alcance. ¿La sirve el API (una página HTML mínima en `orbita-api.atmosferast.com`) o se hará en la web más adelante? | 12 | Página mínima servida por el API, con inicio de sesión y confirmación |
| P3 | **Revocación inmediata.** ¿El middleware de autenticación comprueba en cada petición que la sesión siga viva (una consulta por índice)? Sin eso, tras cerrar sesión o eliminar la cuenta, el token de acceso sigue sirviendo hasta 15 minutos | 01 | Sí, comprobarla |
| P4 | **`GET /home`.** El spec de Android pide un endpoint que junte todo Inicio en una petición. Hoy `HomeViewModel` lo arma con 5 repositorios. ¿Se hace el endpoint (menos viajes en el móvil, pero un repositorio nuevo en Android) o Inicio sigue componiendo los endpoints de cada módulo? | 08 | Hacer `GET /home` |
| P5 | **Verificación de correo.** La guía dice "queda lista, desactivada por configuración". ¿Se implementa ya (apagada) o se deja para cuando se quiera activar? | 11 | Dejarla para después; no se lanza con ella |
| P6 | **Binario `api` versionado.** El commit `d0b4df1` subió a la raíz un ejecutable de macOS de 20.5 MB (`api`), y `.dockerignore` no lo excluye. ¿Lo quito en `develop` y agrego `/api` a `.gitignore` y `.dockerignore`? | 00 | Sí (Joel debería saberlo) |
| P7 | **Rama de Android.** El `CLAUDE.md` de Android no define ramas, y su remoto tiene `main` y `chore/remove-supabase`. ¿Los cambios de Android van a `main`, a una rama `develop` nueva, o a una rama por plan? | 13 | Una rama `develop` también en Android |
| P8 | **Documentos de Android borrados.** En la copia local de `atm-orbita-android`, la carpeta `docs/` está borrada sin commit (los 8 archivos siguen en `HEAD`; los leí desde git). ¿Fue a propósito? Si se restauran, hay que actualizarlos: todavía dicen NestJS + Prisma | 13 | Restaurarlos y actualizar el backend a Go |
| P9 | **Herramientas locales.** En esta máquina no están Go 1.27, `golangci-lint` ni `sqlc`/`goose`, y Docker no respondía la última vez. Sin ellos no puedo compilar, probar ni levantar el Postgres de desarrollo | 00 | Instalar Go 1.27.3, golangci-lint y Docker Desktop |

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
