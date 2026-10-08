# 13 · Escalabilidad y operación

Plan transversal: cómo se despliega, cómo se observa, cómo se respalda y cómo crece. La idea rectora es **empezar con lo mínimo que funciona bien y tener claro, de antemano, qué señal dispara cada pieza adicional**. No se construye infraestructura para una carga que todavía no existe.

## 1. Por qué el diseño ya escala

Estas decisiones están tomadas en los planes de fase y son las que permiten crecer sin reescribir:

| Decisión | Efecto |
|---|---|
| API sin estado en memoria | Se añaden réplicas detrás del proxy sin coordinación |
| Toda consulta entra por `user_id` con índice | El costo depende de los datos de **un** usuario, no del total |
| Sumas y agrupaciones en SQL | No se traen miles de filas a memoria para sumar |
| Paginación por cursor | La página 500 cuesta lo mismo que la primera |
| Límite de tamaño de página, de rango y de cuerpo | Ninguna petición puede pedir "todo" |
| Saldos en una sola consulta agrupada | Sin una consulta por cuenta |
| Embebidos cargados por lote | Sin una consulta por fila de la lista |
| `GET /home` agregado | Una petición en lugar de cinco al abrir la app |
| `ETag` y `304` | Las relecturas sin cambios casi no transfieren datos |
| Tiempos máximos de sentencia y de petición | Una consulta mala no arrastra al resto |
| Tarea diaria con candado en PostgreSQL | Funciona igual con una réplica o con diez |
| PDF transmitido, con concurrencia acotada | La memoria no crece con el tamaño del reporte |

## 2. Topología inicial

**Lo que ya existe** (commit `f76bb49`; el despliegue ya no es una decisión abierta):

| Pieza | Cómo funciona hoy |
|---|---|
| Disparador | Un push a la rama `production` |
| Construcción | GitHub Actions construye la imagen y la publica en GHCR con las etiquetas `latest` y el identificador del commit |
| Despliegue | Entra por SSH al servidor, descarga la imagen y ejecuta `docker compose -f docker-compose.prod.yml up -d` |
| Base de datos | Un contenedor de PostgreSQL **que ya existe en el servidor**; el API se une a su red de Docker |
| Migraciones | `prisma migrate deploy` al arrancar el contenedor del API |
| Ramas | El usuario decidió trabajar solo en `main`; `production` se usa únicamente para desplegar |

**Lo que falta respecto al objetivo de abajo:** proxy inverso con HTTPS (hoy se publica el puerto 3000), respaldos, paso de migración aparte, `healthcheck` y espera de `ready`, y que el nombre de la imagen del `docker-compose.prod.yml` coincida con el que publica el flujo. Están listados como observaciones en [03](03-fase-0-fundaciones.md), "Estado actual".

**Objetivo.** Un servidor con Docker Compose:

```
Internet ──▶ proxy inverso (TLS, HSTS, compresión)
                 │
                 ▼
              api (1 réplica) ──▶ db (PostgreSQL, volumen)
                                      │
                                      ▼
                              respaldos fuera del servidor
```

**`docker-compose.prod.yml`** (el archivo ya existe con el servicio `api`; esto es a lo que debe llegar)

| Servicio | Detalle |
|---|---|
| `proxy` | Único servicio con puertos publicados (80 y 443). Certificados automáticos |
| `api` | Imagen versionada, sin puertos publicados, `healthcheck`, límites de CPU y memoria, reinicio automático, sistema de archivos de solo lectura |
| `migrate` | Misma imagen; ejecuta `prisma migrate deploy` con el rol migrador y termina. El `api` no arranca hasta que acaba bien |
| `db` | PostgreSQL con volumen con nombre, sin puertos publicados, `healthcheck`, configuración ajustada |
| `backup` | Tarea programada de respaldo |

Redes: el `proxy` y el `api` comparten una; el `api` y la `db` comparten otra. El proxy no puede hablar con la base.

## 3. Despliegue

1. El pipeline construye la imagen y la etiqueta con el identificador del cambio.
2. Se ejecuta `migrate`. Si falla, el despliegue se detiene y la versión anterior sigue en servicio.
3. Se arranca la versión nueva y se espera a que `health/ready` responda.
4. Se retira la anterior.

Para que esto funcione sin corte, **toda migración debe ser compatible con la versión anterior del código** (expandir primero, contraer después; ver [02](02-modelo-de-datos.md), sección 7). Con una sola réplica hay unos segundos de solapamiento; con dos o más, el relevo es escalonado.

**Vuelta atrás:** se redespliega la imagen anterior. Como las migraciones son compatibles hacia atrás, no hace falta deshacerlas.

**Apagado ordenado:** al recibir la señal de parada, el API deja de aceptar conexiones, termina las peticiones en curso (con un tope de 20 s) y cierra la base.

## 4. Observabilidad

### Registros

JSON estructurado a la salida estándar, recogido por Docker. Cada línea lleva `requestId`, y `userId` cuando existe. Reglas de contenido en [11](11-seguridad.md), sección 10.

### Métricas

Un endpoint interno de métricas, no accesible desde internet:

| Métrica | Para qué |
|---|---|
| Peticiones por ruta y estado | Tráfico y tasa de error |
| Duración por ruta (percentiles 50, 95, 99) | Latencia |
| Conexiones de base en uso y en espera | Saturación del grupo de conexiones |
| Duración de consultas | Consultas lentas |
| Inicios de sesión correctos y fallidos, `429` emitidos | Abuso |
| Generaciones de PDF en curso y en espera | Saturación de la exportación |
| Última ejecución correcta del tipo de cambio | Tarea diaria |
| CPU, memoria y retraso del bucle de eventos | Salud del proceso |

En PostgreSQL: `pg_stat_statements` activado, para saber qué consultas consumen más tiempo en total.

### Alertas

| Señal | Umbral de partida |
|---|---|
| Tasa de `5xx` | Más del 1 % durante 5 minutos |
| Latencia, percentil 95 | Más de 1 s durante 10 minutos |
| `health/ready` | Fallando 2 minutos |
| Conexiones en espera | Sostenidas 5 minutos |
| Disco de la base | Más del 80 % |
| Respaldo | Ninguno correcto en 26 horas |
| Tipo de cambio | Ninguna ejecución correcta en 48 horas |
| Reutilización de tokens de renovación | Cualquier pico |

### Sondas

- `health/live`: el proceso responde. Si falla, se reinicia el contenedor.
- `health/ready`: la base responde. Si falla, el proxy deja de enviarle tráfico pero no se reinicia.

## 5. Respaldos y recuperación

Con Supabase los respaldos los daba el proveedor. Ahora son responsabilidad propia, y **un respaldo que nunca se ha restaurado no es un respaldo**.

| Elemento | Plan |
|---|---|
| Respaldo lógico diario | `pg_dump` en formato comprimido, cifrado, enviado a un almacenamiento **fuera del servidor** |
| Retención | 7 diarios, 4 semanales, 12 mensuales |
| Recuperación a un instante | Archivado continuo del registro de transacciones (WAL), cuando haya usuarios reales |
| Prueba de restauración | Automática, semanal: se restaura el último respaldo en un contenedor desechable y se comprueban conteos y una consulta de saldos |
| Antes de cada migración en producción | Respaldo adicional |

Objetivos de partida: perder como máximo 24 horas de datos con el respaldo diario (minutos con WAL) y volver a estar en servicio en menos de 2 horas. El procedimiento de recuperación se escribe paso a paso y se ensaya antes de la salida a producción.

## 6. Configuración de PostgreSQL

Partiendo de un servidor pequeño, ajustar al menos:

- `shared_buffers` en torno al 25 % de la memoria del contenedor y `effective_cache_size` en torno al 50–75 %.
- `max_connections` dimensionado junto con el grupo de conexiones del API (sección 7).
- `statement_timeout`, `lock_timeout` e `idle_in_transaction_session_timeout` para el rol `orbita_app`.
- `log_min_duration_statement` en 500 ms, para registrar las consultas lentas.
- `pg_stat_statements` cargado.
- Limpieza automática (*autovacuum*) vigilada en `transactions`, que es la tabla que más crece.

## 7. Conexiones

Cada réplica del API mantiene un grupo de conexiones de tamaño `DATABASE_POOL_MAX`. La regla que no se puede romper:

```
réplicas × DATABASE_POOL_MAX + conexiones de migración y respaldo < max_connections
```

Punto de partida: 1 réplica con 10 conexiones. Un grupo más grande no es más rápido: pasado cierto punto las conexiones compiten entre sí dentro de PostgreSQL.

Cuando el producto `réplicas × grupo` se acerque al máximo, se coloca **PgBouncer** delante de la base en modo transacción. Antes de hacerlo hay que verificar la compatibilidad del adaptador de Prisma con ese modo.

## 8. Camino de crecimiento

Cada paso se da cuando su señal aparece en las métricas, no antes.

| Paso | Señal que lo dispara | Qué se hace |
|---|---|---|
| 1. Más recursos al servidor | CPU o memoria sostenidas por encima del 70 % | Subir de tamaño. Es el paso más barato y suele bastar mucho tiempo |
| 2. Separar la base | El API y la base compiten por CPU o disco | PostgreSQL a su propio servidor o a un servicio gestionado |
| 3. Varias réplicas del API | CPU del API saturada con la base holgada; o se quiere desplegar sin corte | 2 o más réplicas detrás del proxy |
| 4. Redis | En cuanto hay más de una réplica | Contadores del límite de peticiones compartidos |
| 5. PgBouncer | Las conexiones se acercan a `max_connections` | Grupo de conexiones delante de la base |
| 6. Caché de reportes de meses cerrados | `GET /reports/summary` domina el tiempo de base | Guardar el resultado e invalidarlo al modificar un movimiento de ese periodo |
| 7. Réplica de lectura | Las lecturas saturan la base pese a los índices | Reportes y PDF contra una réplica |
| 8. PDF en segundo plano | Ver [10](10-fase-7-exportar-pdf.md), sección 5 | Cola y proceso trabajador |
| 9. Índice de trigramas | La búsqueda `q` sin rango de fechas es lenta | `pg_trgm` sobre descripción y nota |
| 10. Particionar `transactions` | Cientos de millones de filas, o mantenimiento de la tabla demasiado lento | Partición por *hash* de `user_id` |

**Sobre el paso 3 y Argon2:** verificar contraseñas consume CPU a propósito. Si los inicios de sesión se acumulan, se dimensiona el grupo de hilos de Node y se respeta el límite de peticiones de las rutas de autenticación, que es lo que protege al resto del API de un pico de inicios de sesión.

**Sobre el saldo calculado:** "el saldo no se guarda" es una regla del proyecto y se mantiene. Con el índice que cubre `(account_id) include (kind, amount)`, sumar los movimientos de una persona es rápido incluso con décadas de historial. Solo si las mediciones demostraran lo contrario se plantearía un saldo acumulado por periodos cerrados, como caché reconstruible y nunca como fuente de verdad.

## 9. Estimación de volumen

| Dato | Supuesto |
|---|---|
| Movimientos por usuario activo | 50 a 150 al mes |
| Tamaño aproximado de una fila de `transactions` con sus índices | Unos cientos de bytes |
| 10,000 usuarios durante 5 años | Del orden de 60 millones de filas, decenas de GB |
| Peticiones por usuario activo al día | Decenas |

A ese tamaño, una sola instancia de PostgreSQL con los índices previstos trabaja con holgura. La partición (paso 10) queda muy lejos.

## 10. Costos y sencillez

- Un solo servidor al inicio. Sin orquestador de contenedores: Docker Compose basta hasta que hagan falta varias máquinas.
- Sin Redis, sin colas y sin réplicas hasta que su señal aparezca.
- Cada pieza que se añade es una pieza que hay que vigilar, actualizar y respaldar. Se justifica con una métrica, no con una suposición.

## 11. Tareas

- [ ] `compose.prod.yaml` con `proxy`, `api`, `migrate`, `db` y `backup`, redes separadas y secretos
- [ ] Configuración de PostgreSQL de la sección 6
- [ ] Guion de despliegue con migración previa y espera de `ready`
- [ ] Guion de vuelta atrás
- [ ] Endpoint interno de métricas y panel básico
- [ ] Alertas de la sección 4
- [ ] Respaldo diario cifrado fuera del servidor
- [ ] Prueba automática de restauración
- [ ] Procedimiento de recuperación escrito y ensayado
- [ ] Guiones de carga y conjunto de datos generado ([12](12-pruebas.md), sección 8)
- [ ] Primera medición de referencia en el servidor de destino
- [ ] Documento breve de operación: cómo desplegar, volver atrás, rotar claves, restaurar

## 12. Criterios de aceptación

- Se despliega una versión nueva con un comando y sin intervención manual en la base.
- Se puede volver a la versión anterior en minutos.
- Una restauración completa desde el respaldo se ha hecho con éxito al menos una vez.
- Cada alerta se ha disparado a propósito una vez para comprobar que llega.
- Los objetivos de carga de [12](12-pruebas.md) se cumplen en el servidor de destino.
