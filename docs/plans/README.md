# Planes de implementación · API de Orbita

Planes para construir el backend de Orbita en este repositorio (`atm-orbita-api`), a partir de lo que ya implementa la app Android (`atm-orbita-android`).

- Elaborados el 7 oct 2026, tras revisar el proyecto Android completo.
- Estado: **Fase 0 empezada.** El repositorio ya tiene Prisma, Docker, integración continua y despliegue (commit `f76bb49`); lo que eso cubre y lo que falta está en [03](03-fase-0-fundaciones.md), "Estado actual". No hay todavía ningún módulo de negocio ni de autenticación.
- **Se trabaja en una sola rama, `main`.**

## Punto de partida

La app Android tiene toda su lógica funcionando sobre datos en memoria, y sus documentos daban por hecho que el backend sería Supabase, sin servidor propio. **Esa decisión cambió**: el backend es este API.

| Decisión | Valor |
|---|---|
| Backend | API NestJS propio. No se usa Supabase |
| Base de datos | PostgreSQL en un contenedor Docker |
| Acceso a datos | Prisma |
| Autenticación | Propia, con correo y contraseña. Google, después |
| Además de la paridad con Android | Tipo de cambio automático diario y exportar reportes a PDF |
| Fecha del movimiento | Solo fecha |

Segunda ronda de decisiones (7 oct 2026), ya incorporada a los planes:

| Tema | Decisión | Plan |
|---|---|---|
| Stack | NestJS + PostgreSQL + Prisma, sin ningún backend gestionado, autenticación propia | [01](01-arquitectura-y-convenciones.md) |
| Zona horaria | Zona IANA en el perfil; "hoy" lo calcula el backend; fechas en columnas `DATE` sin valor por defecto | [01](01-arquitectura-y-convenciones.md) §6 |
| Recuperar contraseña y eliminar cuenta | Requisito de salida a producción | [04](04-fase-1-autenticacion.md) §5 y §6 |
| Respaldos en Android | `allowBackup="false"` y reglas de extracción; **ya aplicado** | [14](14-integracion-android.md) §7 |
| Tipo de cambio | Puede no existir; sin él no se opera en otra moneda | [05](05-fase-2-cuentas-categorias-ajustes.md) §3 |
| Nombre de categoría | Único por usuario y tipo, sin distinguir mayúsculas **ni tildes** (`name_normalized`); se muestra el original; `409` | [05](05-fase-2-cuentas-categorias-ajustes.md) §5 |
| Compras pendientes | Editar y eliminar, en `prisma.$transaction` | [08](08-fase-5-tarjetas-de-credito.md) §4 |
| Versiones | `class-validator`; Prisma `7.10.0` exacto | [01](01-arquitectura-y-convenciones.md) §2.1 |

Tercera ronda (7 oct 2026):

| Tema | Decisión | Plan |
|---|---|---|
| Tipo de cambio ausente | Ausencia de fila en `exchange_rates`; sin columna `Decimal?` en el perfil | [02](02-modelo-de-datos.md) §2 |
| Bloqueo sin tipo de cambio | Desde el origen: ni cuentas ni tarjetas en otra moneda | [05](05-fase-2-cuentas-categorias-ajustes.md) §3 |
| Cambio de par de monedas | Campo vacío; solo se guarda con un valor mayor que cero, validado en el backend | [05](05-fase-2-cuentas-categorias-ajustes.md) §2 |
| Correo | Resend, por SMTP y detrás de `MailService`; dominio verificado con SPF y DKIM | [04](04-fase-1-autenticacion.md) §7 |
| Supabase | Eliminado del repositorio de Android. **Las migraciones de Prisma son la única fuente del esquema** | [14](14-integracion-android.md) §8 |

## Documentos

### Base (leer primero, en este orden)

| # | Documento | Contenido |
|---|---|---|
| 00 | [Contexto y hallazgos](00-contexto-y-hallazgos.md) | Qué hace Android, qué endpoint cubre cada cosa, reglas de negocio, problemas encontrados y decisiones abiertas |
| 01 | [Arquitectura y convenciones](01-arquitectura-y-convenciones.md) | Stack, estructura de carpetas, formato de peticiones y errores, dinero, fechas, paginación |
| 02 | [Modelo de datos](02-modelo-de-datos.md) | Tablas, restricciones, índices, consultas clave y migraciones |

### Fases (se ejecutan en orden)

| # | Fase | Entrega | Tamaño | Depende de |
|---|---|---|---|---|
| 03 | [Fase 0 · Fundaciones](03-fase-0-fundaciones.md) | Docker, PostgreSQL, Prisma, errores, registros, salud, pipeline | Mediano | — |
| 04 | [Fase 1 · Autenticación](04-fase-1-autenticacion.md) | Registro, sesión renovable, cierre, eliminar cuenta, recuperación de contraseña | Grande | 0 |
| 05 | [Fase 2 · Cuentas, categorías y ajustes](05-fase-2-cuentas-categorias-ajustes.md) | Cuentas con saldo, categorías, monedas y cambio manual | Mediano | 1 |
| 06 | [Fase 3 · Movimientos y transferencias](06-fase-3-movimientos-transferencias.md) | Ingresos, egresos, transferencias y lista paginada | Grande | 2 |
| 07 | [Fase 4 · Reportes e Inicio](07-fase-4-reportes-e-inicio.md) | Reporte por periodo y resumen de Inicio | Mediano | 3 |
| 08 | [Fase 5 · Tarjetas de crédito](08-fase-5-tarjetas-de-credito.md) | Tarjetas, compras pendientes y pago atómico | Mediano | 3 |
| 09 | [Fase 6 · Tipo de cambio automático](09-fase-6-tipo-de-cambio-automatico.md) | Tarea diaria y modo automático | Mediano | 2 |
| 10 | [Fase 7 · Exportar a PDF](10-fase-7-exportar-pdf.md) | PDF del reporte | Mediano | 4 |

```
0 ──▶ 1 ──▶ 2 ──▶ 3 ──▶ 4 ──▶ 7
            │     └───▶ 5
            └─────────▶ 6
```

Las fases 4, 5 y 6 no dependen entre sí y pueden hacerse en cualquier orden o en paralelo. Con las fases 0 a 5 el API alcanza la paridad con todo lo que Android implementa hoy.

### Transversales (aplican a todas las fases)

| # | Documento | Contenido |
|---|---|---|
| 11 | [Seguridad](11-seguridad.md) | Aislamiento entre usuarios, autenticación, validación, límites, secretos, privacidad, lista previa a producción |
| 12 | [Pruebas](12-pruebas.md) | Niveles, vectores obligatorios de dinero, matriz de aislamiento, concurrencia, carga |
| 13 | [Escalabilidad y operación](13-escalabilidad-y-operacion.md) | Despliegue, observabilidad, respaldos y camino de crecimiento |

### Clientes y futuro

| # | Documento | Contenido |
|---|---|---|
| 14 | [Integración con Android](14-integracion-android.md) | Qué cambia en la app para dejar Supabase y consumir el API |
| 15 | [Después](15-futuro.md) | Google, sincronización sin internet, web, iOS, notificaciones |

## Reglas que no se negocian

Vienen de Android y se mantienen en el API.

1. **Dinero exacto.** Decimal, escala 2, redondeo *half up*, una sola vez al final. En JSON, los montos son texto. Nunca coma flotante.
2. **El saldo no se guarda**: se calcula.
3. **Cada usuario ve solo sus datos.** El identificador del usuario sale únicamente del token; toda consulta filtra por él; la base lo refuerza con claves foráneas compuestas.
4. **Las transferencias no son ingreso ni gasto**, y las compras con tarjeta pendientes no cuentan hasta pagarse.
5. **Los reportes no mezclan monedas.**
6. **No hay datos del futuro**, calculando "hoy" con la zona horaria del usuario.
7. **Los cambios de esquema van solo por migraciones versionadas.**
8. **Ningún secreto en el repositorio.**
9. No se agregan funciones fuera del alcance de la fase en curso.

## Cómo trabajar con estos planes

- Una fase a la vez. Al terminarla: correr sus pruebas, marcar sus casillas, resumir lo hecho y pedir revisión antes de pasar a la siguiente.
- Cada plan de fase tiene las mismas secciones: objetivo, alcance, diseño, tareas, pruebas, criterios de aceptación y riesgos.
- Si el código y un plan no coinciden, se corrige uno de los dos en el mismo cambio. Los planes son documentos vivos.
- Si algo es ambiguo, se pregunta. No se inventan reglas de negocio.
- La definición de "terminado" está en [01](01-arquitectura-y-convenciones.md), sección 12.

## Lo que queda por decidir

Quedan dos decisiones, y ninguna bloquea las fases 0 a 5. El detalle y la recomendación de cada una están en [00](00-contexto-y-hallazgos.md), sección 6.

| # | Pregunta | Se necesita antes de |
|---|---|---|
| D5 | Proveedor de tipo de cambio | Fase 6 |
| D7 | Cómo guarda la web el token de renovación | Cliente web |

Cuarta ronda (7 oct 2026), ya incorporada:

| Tema | Decisión | Plan |
|---|---|---|
| Despliegue y ramas | El que ya tiene el repositorio (GitHub Actions, GHCR, SSH, Docker Compose). Se trabaja solo en `main` | [13](13-escalabilidad-y-operacion.md) §2 |
| Egreso de un pago eliminado | La compra vuelve a pendiente. Una compra solo se elimina desde la deuda de la tarjeta, mientras está pendiente | [08](08-fase-5-tarjetas-de-credito.md) §4 |
| Archivar tarjeta con compras pendientes | No se puede | [08](08-fase-5-tarjetas-de-credito.md) §2 |
| Verificación de correo | No al lanzar | [04](04-fase-1-autenticacion.md) §5 |
| PDF | Solo resumen | [10](10-fase-7-exportar-pdf.md) |
| Correo | Resend, dominio `atmosferast.com`; variables `MAIL_*` ya en `.env.example` | [04](04-fase-1-autenticacion.md) §7 |
| Categoría archivada | Deja libre su nombre | [05](05-fase-2-cuentas-categorias-ajustes.md) §5 |
| Letra `ñ` | Se conserva al normalizar | [05](05-fase-2-cuentas-categorias-ajustes.md) §5 |
| Monedas al registrarse | La principal se elige al registrarse; "Efectivo" nace en ella | [04](04-fase-1-autenticacion.md) §8 |
| Editar el egreso de un pago | Solo cuenta, monto y fecha | [06](06-fase-3-movimientos-transferencias.md) §1 |

Además hay **tareas** sobre el código que ya existe (nombre de la imagen de producción, puerto publicado sin proxy, migraciones al arrancar, y otras): están en [03](03-fase-0-fundaciones.md), "Estado actual".

Lo que solo puede hacer el usuario: poner la clave de API de Resend y el remitente en las variables `MAIL_SMTP_PASSWORD` y `MAIL_FROM`, y verificar el dominio `atmosferast.com` en Resend.

## Requisitos para salir a producción

Además de las fases, tienen que estar hechos:

- **Recuperación de contraseña** de principio a fin, con correo real (Fase 1b). Decisión del usuario.
- **Eliminar la cuenta** desde la app y desde una página web, con borrado real. Decisión del usuario.
- Dominio de correo verificado en Resend con SPF y DKIM, y con DMARC.
- Respaldos automáticos con una restauración probada.
- La lista de verificación de [11](11-seguridad.md), sección 13.
- Los objetivos de carga de [12](12-pruebas.md), sección 8, medidos en el servidor de destino.
- En Android: todos los módulos conectados al API ([14](14-integracion-android.md), sección 9). Sus documentos ya están corregidos y Supabase ya está retirado del código.
