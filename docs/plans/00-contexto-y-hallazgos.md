# 00 · Contexto y hallazgos del proyecto Android

Este documento resume lo que hoy implementa `atm-orbita-android`, lo traduce a lo que el API debe ofrecer y lista los problemas encontrados durante la revisión. Es la base de todos los demás planes.

- Fecha de la revisión: 7 oct 2026 (commit Android `b728ea2`).
- Fuentes leídas: `docs/ORBITA_SPEC.md`, `docs/01` a `docs/06`, `docs/CLAUDE.md`, `supabase/migrations/0001_init.sql`, los paquetes `core`, `domain`, `data`, `di`, todos los ViewModels y las pruebas unitarias.

## 1. Qué es Orbita y en qué estado está

App personal de finanzas: ingresos y egresos por cuenta, varias monedas, transferencias entre cuentas propias, compras con tarjeta de crédito pendientes de pago y reportes. Multiusuario: cada persona ve solo sus datos.

| Módulo Android | Estado | Dónde viven los datos hoy |
|---|---|---|
| Autenticación | Funcional | Supabase Auth si hay claves; si no, modo demo |
| Inicio, Cuentas, Categorías, Movimientos, Transferencias, Reportes, Ajustes, Tarjetas de crédito | Funcional de punta a punta | En memoria (`data/demo`), se pierde al cerrar la app |

La arquitectura Android ya está preparada para cambiar la fuente de datos: las pantallas hablan con ViewModels, estos con las interfaces de `domain/repository`, y `di/AppModule.kt` decide la implementación. **El API reemplaza a Supabase como destino de esas interfaces.**

## 2. Decisiones tomadas con el usuario (7 oct 2026)

| Tema | Decisión |
|---|---|
| Backend | **No se usa Supabase.** El API NestJS es el único backend. Android, web e iOS hablan solo con el API |
| Base de datos | **PostgreSQL propio en un contenedor Docker** |
| Acceso a datos | **Prisma** |
| Autenticación | **Propia**, con correo y contraseña. Acceso con Google queda para después |
| Alcance adicional | **Tipo de cambio automático diario** y **exportar reportes a PDF** |
| Fecha del movimiento | **Solo fecha** (`occurred_on`), con `created_at` para desempatar el orden |
| Fuera de alcance | Sincronización offline (el diseño queda preparado, no se planifica) |

Consecuencia: todo lo que los documentos de Android atribuían a Supabase (Auth, PostgREST, RLS, triggers, funciones RPC, `handle_new_user`) pasa a ser responsabilidad del API. Los documentos de Android ya se corrigieron ([14-integracion-android.md](14-integracion-android.md), sección 8).

### Segunda ronda, tras revisar los hallazgos (7 oct 2026)

Stack confirmado: **NestJS + PostgreSQL + Prisma**, sin Supabase ni ningún otro servicio de backend gestionado, con autenticación propia.

| # | Tema | Decisión | Dónde quedó |
|---|---|---|---|
| 1 | Zona horaria (H2) | Aprobado. Zona IANA en el perfil; "hoy" se calcula en el backend con ella. Ninguna columna de fecha tiene valor por defecto en la base. La fecha del movimiento es una columna `DATE` (`@db.Date`), ya calculada, nunca un instante | [01](01-arquitectura-y-convenciones.md) §6, [02](02-modelo-de-datos.md) §2 |
| 2 | Recuperar contraseña y eliminar cuenta (H12) | **Requisito de salida a producción.** Token de un solo uso, de vida corta, guardado como hash. Eliminación real de los datos, accesible desde la app | [04](04-fase-1-autenticacion.md) §3, §5 y §6 |
| 3 | `allowBackup` (H11) | `android:allowBackup="false"` y reglas de extracción para Android 12+. **Ya aplicado** en el manifiesto de Android | [14](14-integracion-android.md) §7 |
| 4 | Usuario sin tipo de cambio (H4) | El tipo de cambio puede no existir. La interfaz muestra "Configura tu tipo de cambio" y no se permiten operaciones en otra moneda hasta que exista | [05](05-fase-2-cuentas-categorias-ajustes.md) §3 |
| 5 | Nombre de categoría repetido (H5) | Único **por usuario y tipo**, sin distinguir mayúsculas **ni tildes** (columna `name_normalized`). `409` con código estable; Android muestra el mensaje bajo el campo | [05](05-fase-2-cuentas-categorias-ajustes.md) §5 |
| 6 | Compra pendiente (H6) | Editar y eliminar **aprobados**, dentro de `prisma.$transaction` | [08](08-fase-5-tarjetas-de-credito.md) §4 |
| 7 | Versiones (H16, H17) | `class-validator` en lugar de `nestjs-zod`. Prisma fijado en la versión exacta **7.10.0** | [01](01-arquitectura-y-convenciones.md) §2 |

## 3. De los repositorios de Android a los endpoints del API

Los contratos de `domain/repository/Repositories.kt` son la especificación real de lo que la app necesita. Prefijo común: `/api/v1`.

| Repositorio Android | Operación | Endpoint | Plan |
|---|---|---|---|
| `AuthRepository` | `signUp` | `POST /auth/register` | [04](04-fase-1-autenticacion.md) |
| | `signIn` | `POST /auth/login` | |
| | `session` (restaurar y renovar) | `POST /auth/refresh`, `GET /me` | |
| | `signOut` | `POST /auth/logout` | |
| `SettingsRepository` | `observeSettings` | `GET /settings` | [05](05-fase-2-cuentas-categorias-ajustes.md) |
| | `setFx` | `PUT /settings/fx` | |
| | `setDisplayCurrency` | `PATCH /settings` | |
| `AccountsRepository` | `observeAccounts` | `GET /accounts` | [05](05-fase-2-cuentas-categorias-ajustes.md) |
| | `create` / `update` | `POST /accounts`, `PATCH /accounts/{id}` | |
| | `setIncludeInSavings` | `PATCH /accounts/{id}` | |
| | `archive` | `POST /accounts/{id}/archive` | |
| `CategoriesRepository` | `observeCategories` | `GET /categories` | [05](05-fase-2-cuentas-categorias-ajustes.md) |
| | `create` / `update` / `archive` | `POST`, `PATCH`, `POST …/archive` | |
| `EntriesRepository` | `observeEntries(period)` | `GET /entries?from&to` | [06](06-fase-3-movimientos-transferencias.md) |
| | `observeRecentEntries(limit)` | `GET /entries?limit` | |
| | movimientos | `POST/PATCH/DELETE /transactions` | |
| | transferencias | `POST/PATCH/DELETE /transfers` | |
| `ReportsRepository` | `observeReport(period)` | `GET /reports/summary?from&to` | [07](07-fase-4-reportes-e-inicio.md) |
| `CreditRepository` | tarjetas | `GET/POST/PATCH /credit-cards`, `POST …/archive` | [08](08-fase-5-tarjetas-de-credito.md) |
| | `observePendingPurchases` | `GET /credit-purchases?status=pending` | |
| | `createPurchase` | `POST /credit-purchases` | |
| | `pay` | `POST /credit-purchases/{id}/pay` | |
| `DateProvider` | `today()` | Reloj del dispositivo; el API valida con la zona horaria del perfil | [01](01-arquitectura-y-convenciones.md) |

Además, la pantalla de Inicio combina cinco flujos (cuentas, ajustes, recientes, reporte del mes, crédito pendiente). Para no hacer cinco viajes de red, el API expone `GET /home` ([07](07-fase-4-reportes-e-inicio.md)).

## 4. Reglas de negocio que el API debe garantizar

Estas reglas ya están implementadas y probadas en Android (`MoneyRulesTest`, `DemoRepositoriesTest`). El API debe dar **exactamente** los mismos resultados.

1. **Dinero exacto.** Decimal de escala 2, redondeo *half up*, redondeo una sola vez al final. Nunca coma flotante. En el API: `Prisma.Decimal` y montos como **texto** en JSON.
2. **El saldo no se guarda.** `saldo = saldo_inicial + ingresos − egresos + transferencias que entran − transferencias que salen`, ignorando lo borrado.
3. **Un movimiento hereda la moneda de su cuenta.** Una cuenta tiene una sola moneda y no cambia después de creada.
4. **Las transferencias no son ingreso ni gasto.** No entran en reportes ni en los totales del mes.
5. **Las compras con tarjeta pendientes no afectan nada** hasta que se pagan. Al pagar nace un egreso con la categoría original y la fecha de pago, de forma atómica.
6. **Los reportes no mezclan monedas.** Un bloque por moneda, la principal primero.
7. **Borrado lógico.** Movimientos y transferencias se marcan con `deleted_at`; cuentas, categorías y tarjetas se archivan.
8. **No hay datos del futuro.** Movimientos, transferencias y pagos no pueden tener fecha posterior a hoy. Excepción: fecha límite de pago de una compra con tarjeta, que no puede ser anterior a hoy al crearla.
9. **Aislamiento por usuario.** Ningún usuario puede leer, modificar ni referenciar datos de otro.
10. **Identificadores UUID generados en el cliente**, para dejar lista la sincronización futura.
11. **Tipo de cambio:** unidades de la moneda destino por 1 unidad de la moneda origen, hasta 6 decimales. Editarlo inserta una fila nueva, no modifica la anterior.
12. **Alta de usuario:** se crean el perfil, 9 categorías iniciales y la cuenta "Efectivo" en soles.

Límites de texto: nombres 1–60 caracteres, descripción y nota hasta 500, contraseña mínimo 8. Monedas admitidas: PEN, USD, EUR, MXN, COP, CLP, ARS, BOB, BRL.

Vectores de prueba obligatorios (deben pasar en el API, ver [12-pruebas.md](12-pruebas.md)): ahorro total S/ 7,166.30 = US$ 2,239.47; septiembre 2026 con ingresos 4,200.00, gastos 2,148.60 y balance 2,051.40; porcentajes 37.2 / 28.5 / 10.0 / 8.8 / 8.5 / 6.9; pagar "Pasajes" deja Débito principal en 1,005.80.

## 5. Hallazgos

Problemas y vacíos detectados al revisar Android. Cada uno indica cómo lo resuelven estos planes.

| # | Hallazgo | Impacto | Resolución |
|---|---|---|---|
| H1 | Todos los documentos de Android asumen Supabase (Auth, RLS, RPC, triggers) | La seguridad y la atomicidad que daba la base de datos ahora dependen del API | Capas de aislamiento en [11](11-seguridad.md); claves foráneas compuestas en [02](02-modelo-de-datos.md); operaciones atómicas en transacciones |
| H2 | **"Hoy" y zona horaria.** El esquema usaba `current_date`, que en un servidor en UTC ya es "mañana" desde las 19:00 de Lima | Movimientos con fecha equivocada y validaciones de "fecha futura" erróneas | **Decidido:** el perfil guarda la zona horaria IANA y el API calcula "hoy" con ella; ninguna fecha tiene valor por defecto en la base ([01](01-arquitectura-y-convenciones.md), sección 6) |
| H3 | El esquema no tiene tarjetas de crédito, ni moneda secundaria, ni moneda de la tarjeta | La migración `0002` nunca se generó | Resuelto en [02](02-modelo-de-datos.md): tabla `credit_cards`, `secondary_currency`, `card_id` y `currency` en las compras |
| H4 | Un usuario nuevo no tiene tipo de cambio, pero `FxPair.rate` no admite nulo en Android | La app no puede mostrar conversiones tras registrarse | **Decidido:** `rate` pasa a ser opcional; estado "Configura tu tipo de cambio" y bloqueo de operaciones en otra moneda ([05](05-fase-2-cuentas-categorias-ajustes.md) §3) |
| H5 | Android no tiene mensaje para "nombre de categoría repetido" (la regla existe en la especificación) | El error llegaría como "Algo salió mal" | **Decidido:** único por usuario y tipo; `409 CATEGORY_NAME_TAKEN`; mensaje bajo el campo ([05](05-fase-2-cuentas-categorias-ajustes.md) §5) |
| H6 | Una compra pendiente no se puede editar ni eliminar | Un monto mal escrito queda para siempre | **Decidido:** `PATCH` y `DELETE` de compras pendientes, transaccionales ([08](08-fase-5-tarjetas-de-credito.md) §4) |
| H7 | Se puede archivar una tarjeta con compras pendientes | Las compras dejan de verse pero siguen debiéndose | **Decidido:** no se puede archivar una tarjeta con compras pendientes (`409 CARD_HAS_PENDING_PURCHASES`) ([08](08-fase-5-tarjetas-de-credito.md) §2) |
| H8 | El teclado de monto limita a 9,999,999.99 | En COP, CLP o ARS eso equivale a pocos miles de dólares | El API admite hasta `NUMERIC(14,2)`; se informa a Android |
| H9 | La lista de movimientos filtra y busca en el cliente, sin paginación | No escala con historial largo | `GET /entries` filtra, busca y pagina en el servidor ([06](06-fase-3-movimientos-transferencias.md)) |
| H10 | Los repositorios son `Flow` que reemiten al cambiar los datos; REST no empuja cambios | Datos desactualizados tras escribir | Caché en memoria con recarga tras cada escritura y `ETag` ([14](14-integracion-android.md)) |
| H11 | `AndroidManifest.xml` tenía `allowBackup="true"`, contra lo que decía `docs/02` | Los tokens de sesión podrían salir en un respaldo | **Corregido** en el manifiesto y en las reglas de respaldo ([14](14-integracion-android.md) §7) |
| H12 | "Olvidé mi contraseña" estaba fuera de alcance, y no existe eliminar cuenta | Con autenticación propia, quien olvida su contraseña pierde el acceso. Google Play exige poder eliminar la cuenta | **Decidido:** ambos son requisito de salida a producción ([04](04-fase-1-autenticacion.md)) |
| H13 | No está definido qué pasa si se edita o elimina el egreso creado al pagar una compra | La compra quedaría "pagada" sin egreso | Decisión abierta D2 |
| H14 | En transferencias entre monedas, `sale × cambio` no siempre da `entra` exacto (el cambio se guarda con 6 decimales) | Exigir igualdad rechazaría transferencias válidas | Los montos mandan; el cambio es informativo ([06](06-fase-3-movimientos-transferencias.md)) |
| H15 | El registro revela si un correo ya existe ("Ese correo ya tiene una cuenta") | Enumeración de usuarios | Se mantiene por experiencia de uso, con límite de intentos ([11](11-seguridad.md)) |
| H16 | `nestjs-zod` declara compatibilidad solo hasta Nest 11; el proyecto usa Nest 12 | Riesgo al validar con Zod | **Decidido:** `class-validator` + `class-transformer` ([01](01-arquitectura-y-convenciones.md) §2) |
| H17 | El tag `latest` del paquete `prisma` apunta a `8.0.0-rc.21`; `@prisma/client` estable es `7.10.0` | `pnpm add prisma` instalaría una versión candidata desalineada del cliente | **Decidido:** los tres paquetes de Prisma fijados en `7.10.0` exacto ([01](01-arquitectura-y-convenciones.md) §2) |
| H18 | La tabla `categories` tiene `icon`, pero el modelo Android no | Ninguno hoy | Se conserva la columna como opcional |

## 6. Decisiones abiertas

Quedan dos. Ninguna bloquea las fases 0 a 5.

| # | Pregunta | Recomendación asumida | Se necesita antes de |
|---|---|---|---|
| D5 | ¿Qué proveedor de tipo de cambio se usa? | Se decide al iniciar la Fase 6, tras verificar cobertura de las 9 monedas ([09](09-fase-6-tipo-de-cambio-automatico.md)) | Fase 6 |
| D7 | ¿El cliente web guarda el token de renovación en una cookie `httpOnly`? | Sí para web; las apps móviles lo envían en el cuerpo | Cliente web |

Además hay observaciones sobre el código que ya existe en el repositorio, que no son decisiones de producto sino tareas: están en [03](03-fase-0-fundaciones.md), sección "Estado actual".

### Cerradas en la cuarta ronda (7 oct 2026)

| # | Tema | Decisión |
|---|---|---|
| D1 | Despliegue | Ya está definido por el código del repositorio (commit `f76bb49`): GitHub Actions construye la imagen, la publica en GHCR y despliega por SSH con Docker Compose en un servidor que ya tiene su contenedor de PostgreSQL. **Se trabaja en una sola rama, `main`** ([13](13-escalabilidad-y-operacion.md) §2) |
| D2 | Egreso de un pago eliminado | **La compra vuelve a pendiente.** Eliminar ese egreso deshace el pago. Una compra solo se elimina desde la deuda de la tarjeta, y solo mientras está pendiente; desde Movimientos nunca se elimina una compra, porque alteraría la información ([08](08-fase-5-tarjetas-de-credito.md) §4) |
| D3 | Archivar una tarjeta con compras pendientes | **No se puede:** primero hay que pagarlas o eliminarlas. `409 CARD_HAS_PENDING_PURCHASES` ([08](08-fase-5-tarjetas-de-credito.md) §2) |
| D4 | Verificación de correo | **No al lanzar.** Queda lista para activarse por configuración |
| D6 | Contenido del PDF | **Solo resumen:** totales y rankings, sin lista de movimientos ([10](10-fase-7-exportar-pdf.md)) |
| D14 | Dominio de correo | **`atmosferast.com`**, con Resend. Las variables `MAIL_*` ya están en `.env.example`; el usuario pondrá la clave de API y el remitente ([04](04-fase-1-autenticacion.md) §7) |
| D15 | Categoría archivada | **Deja libre su nombre** |
| D16 | Letra `ñ` | **Se conserva** al normalizar: "Año" y "Ano" son distintas |
| D17 | Monedas al registrarse | **La moneda principal se elige al registrarse** (la app propone la de la región del teléfono; por defecto soles). "Efectivo" nace en esa moneda. La secundaria es dólares, o soles si la principal ya es dólares ([04](04-fase-1-autenticacion.md) §8) |
| D18 | Editar el egreso de un pago | **Solo cuenta, monto y fecha.** La categoría y la descripción son las de la compra y no se cambian desde el movimiento; tampoco puede pasar a ser un ingreso. `422 PAYMENT_EXPENSE_LOCKED` ([06](06-fase-3-movimientos-transferencias.md) §1) |

### Cerradas en la tercera ronda (7 oct 2026)

| # | Tema | Decisión |
|---|---|---|
| D8 | Servicio de correo | **Resend** como proveedor inicial, por SMTP y detrás de `MailService`. Verificación del dominio y variables de entorno en [04](04-fase-1-autenticacion.md) §7 |
| D9 | Bloqueo sin tipo de cambio | **Desde el origen:** no se crean cuentas ni tarjetas en una moneda distinta de la principal. El mensaje lleva a "Configura tu tipo de cambio" ([05](05-fase-2-cuentas-categorias-ajustes.md) §3) |
| D10 | Tildes en nombres de categoría | **No se distinguen** ni tildes ni mayúsculas. Columna `name_normalized` calculada en el backend, restricción única por usuario, tipo y nombre normalizado; se muestra siempre el nombre original; colisión `409 CATEGORY_NAME_TAKEN` ([05](05-fase-2-cuentas-categorias-ajustes.md) §5) |
| D11 | Cambio de par de monedas | **Campo vacío** hasta que el usuario escriba; se guarda solo con un valor mayor que cero, validado también en el backend ([05](05-fase-2-cuentas-categorias-ajustes.md) §2) |
| D12 | Restos de Supabase | **Eliminados** del repositorio de Android: la migración SQL (commit `f03269f`) y el código, las dependencias y la configuración (commit `b81bdc0`), en la rama `chore/remove-supabase`. Las migraciones de Prisma son la única fuente del esquema ([14](14-integracion-android.md) §8) |
| D13 | Tipo de cambio ausente | **Sin columna `Decimal?`** en el perfil: es la ausencia de fila en `exchange_rates`. Opcional en la respuesta del API y en `FxPair.rate` |

En D10, el texto con las decisiones pedía la restricción sin el tipo (`[userId, nameNormalized]`). Al confirmarlas, el usuario mantuvo su elección anterior: **con tipo**.
