# 12 · Estrategia de pruebas

Plan transversal. Cada fase añade sus pruebas siguiendo estas reglas. La meta no es un porcentaje: es que **ningún error de dinero, de aislamiento entre usuarios o de atomicidad pueda llegar a producción sin que una prueba falle**.

## 1. Niveles

| Nivel | Qué prueba | Con qué | Base de datos | Cuándo corre |
|---|---|---|---|---|
| Unitarias | Reglas puras: dinero, fechas, validaciones, cursores, formato | Vitest | No | En cada guardado y en el pipeline |
| Integración | Servicios y repositorios, restricciones y consultas | Vitest + PostgreSQL real | Sí | Pipeline |
| e2e | El API completo por HTTP: flujos, errores, seguridad | Vitest + Supertest + PostgreSQL real | Sí | Pipeline |
| Contrato | Que el OpenAPI no cambie de forma incompatible | Comparación de `openapi.json` | No | Pipeline |
| Carga | Tiempos y consumo con datos voluminosos | k6 | Sí, con datos generados | Antes de cada versión y bajo demanda |
| Seguridad | Dependencias, imagen, análisis dinámico | `pnpm audit`, analizador de imagen | — | Pipeline y antes de producción |

**No se simula la base de datos.** Las reglas más importantes del proyecto (claves foráneas compuestas, restricciones `CHECK`, índices únicos, transacciones, sumas `NUMERIC`) viven en PostgreSQL; una base falsa en memoria las daría por buenas sin comprobarlas. Las pruebas de integración y e2e usan un PostgreSQL real levantado con Testcontainers, con las migraciones aplicadas.

Sí se sustituyen: el reloj (`Clock`), el envío de correo y el proveedor de tipo de cambio.

## 2. Organización

```
src/**/*.spec.ts            # unitarias, junto al código
test/
  integration/**/*.int-spec.ts
  e2e/**/*.e2e-spec.ts
  security/                 # matriz de aislamiento, autenticación, abuso
  fixtures/
    sample-data.ts          # la semilla de ejemplo de Android
    golden-vectors.json     # vectores compartidos entre plataformas
  helpers/
    app.ts                  # arranca el API de pruebas
    db.ts                   # contenedor, migraciones, limpieza
    users.ts                # crea usuarios y devuelve sus tokens
    builders.ts             # constructores de datos de prueba
  load/                     # guiones de k6
```

Reglas:

- Un contenedor de PostgreSQL por ejecución; las migraciones se aplican una vez.
- Cada prueba parte de datos limpios: se vacían las tablas entre pruebas o cada archivo usa usuarios nuevos. Las pruebas no dependen del orden.
- El reloj se fija en **2 oct 2026**, el "hoy" de los datos de ejemplo.
- Los datos se crean con constructores (`aUser()`, `anAccount({ currency: 'USD' })`), no copiando objetos gigantes.
- Las pruebas e2e entran por HTTP con un token real, como lo haría la app. No llaman a los servicios por dentro.

## 3. Vectores obligatorios de dinero

Son los de la tabla "Pruebas unitarias obligatorias" de `docs/06` de Android, más los de `MoneyRulesTest.kt`. Deben dar **exactamente** lo mismo en el API.

| Caso | Esperado |
|---|---|
| `0.1 + 0.2` | `0.3` |
| Convertir US$ 20.00 a PEN con 3.20 | `64.00` |
| Inversa: S/ 64.00 a USD con 1 US$ = 3.20 | `20.00` |
| Convertir desde una moneda fuera del par | Sin resultado |
| Ahorro total en PEN con los datos de ejemplo | `7166.30` |
| Ahorro total en USD con los datos de ejemplo | `2239.47` (7,166.30 ÷ 3.20 = 2,239.46875, *half up*) |
| Solo "Billetera digital" (excluida de ahorros) | `0.00` |
| Transferencia: sale 20.00, cambio 3.20 | entra `64.00` |
| Transferencia: sale 20.00, entra 63.40 | cambio `3.17` |
| Transferencia en la misma moneda | cambio nulo; entra = sale |
| Monto `"45.52"` | Válido, `45.52` |
| Monto `"45.523"`, `"0"`, `"-5"`, `"45,52"`, `"1e3"`, `""`, `" 45.52"` | Inválido |
| Formato de `1245.8` en PEN | `S/ 1,245.80` |
| Formato de `250` en USD | `US$ 250.00` |
| Porcentajes de gasto de septiembre | `37.2 / 28.5 / 10.0 / 8.8 / 8.5 / 6.9` |
| Porcentaje con total cero | `0` |
| Redondeo de `2.345` y `2.355` a 2 decimales | `2.35` y `2.36` (*half up*) |
| Un monto leído de la base y devuelto en JSON | El mismo texto, sin pérdida |

**Normalización de nombres** (para la unicidad de categorías; `common/text/normalizeName`):

| Entrada | Esperado |
|---|---|
| `Alimentación`, `alimentacion`, `ALIMENTACIÓN` | Las tres dan `alimentacion` |
| `  Otros   gastos ` | `otros gastos` |
| `Pingüinos` | `pinguinos` |
| `Año nuevo` | `año nuevo` (la `ñ` se conserva) |
| `Ano` y `Año` | Resultados distintos |
| `ÁÉÍÓÚ áéíóú` | `aeiou aeiou` |
| Texto ya normalizado | El mismo texto (aplicarla dos veces no cambia nada) |
| Texto escrito con tildes como caracteres combinados (forma NFD) | El mismo resultado que con caracteres precompuestos |

**Vectores compartidos.** Se guardan también en `test/fixtures/golden-vectors.json`, en un formato neutro (entradas y salida esperada). El mismo archivo puede alimentar las pruebas de Android, de iOS y de la web, de modo que las cuatro plataformas demuestran que calculan igual.

## 4. Flujos e2e

Los de `docs/06` de Android, trasladados al API:

1. Registro → cuentas (solo "Efectivo", saldo 0) → crear una cuenta → registrar un ingreso → el saldo es el esperado.
2. Egreso con descripción y categoría → aparece en `GET /entries`, en `GET /home` y en `GET /reports/summary`.
3. Cambiar `displayCurrency` → `GET /home` lo refleja.
4. Apagar `includeInSavings` en una cuenta → baja el ahorro total y el conteo pasa a "3 de 5".
5. Transferencia de USD a PEN con el monto recibido editado → ambos saldos cuadran y ningún reporte cambia.
6. Compra con tarjeta → queda pendiente y no cambia saldos → pagar → aparece como egreso.
7. Reporte por mes, por año y por rango.
8. Editar y eliminar un movimiento → saldos y reportes se recalculan.
9. Cambiar el tipo de cambio → cambian el ahorro convertido y el cambio sugerido; las transferencias guardadas conservan el suyo.
10. Cerrar sesión → el token de renovación deja de servir.

Las cifras esperadas de cada fase están en la sección "Pruebas" de su plan.

## 5. Pruebas de seguridad

### Matriz de aislamiento

Es la prueba más importante del proyecto. Se crean dos usuarios, A y B, cada uno con datos completos. Para **cada ruta** del API, B intenta operar sobre los recursos de A:

| Intento de B | Esperado |
|---|---|
| Leer un recurso de A por su `id` | `404` |
| Editarlo, archivarlo o eliminarlo | `404`, y el recurso de A queda intacto |
| Crear un movimiento en una cuenta de A | `404` |
| Crear un movimiento con una categoría de A | `404` |
| Transferir desde o hacia una cuenta de A | `404` |
| Registrar una compra en una tarjeta de A | `404` |
| Pagar una compra de A, o pagar una propia desde una cuenta de A | `404` |
| Listar (`/accounts`, `/entries`, `/reports/summary`, `/home`…) | Solo datos de B |
| Descargar el PDF | Solo datos de B |

La matriz **se genera a partir de la lista de rutas registradas** en la aplicación. Si alguien añade una ruta y no la incluye en la matriz, la prueba falla. Así una ruta nueva no puede quedar sin verificar.

### Capa de base de datos

Pruebas de integración que se saltan los servicios y escriben directamente, para demostrar que la base rechaza por sí sola:

- Un movimiento que apunta a la cuenta de otro usuario (clave foránea compuesta).
- Un egreso con una categoría de ingreso.
- Un monto en cero o negativo.
- Una transferencia con la misma cuenta en ambos lados.
- Una compra pagada sin datos de pago, y una pendiente con ellos.
- Una fecha límite anterior a la fecha de compra.
- Dos categorías del mismo usuario y tipo con el mismo `name_normalized`. Con otro tipo, o con `name_normalized` nulo (archivada), sí se acepta.
- Un perfil con la misma moneda como principal y secundaria.
- Un movimiento, una compra o un tipo de cambio insertados **sin fecha**: la base los rechaza, porque ninguna columna de fecha tiene valor por defecto.
- El rol `orbita_app` intentando `DROP TABLE` o `ALTER TABLE`.
- Borrar un usuario elimina todas sus filas y ninguna de otros. La prueba **descubre las tablas** con columna `user_id` en el catálogo de PostgreSQL, de modo que una tabla nueva sin cascada la hace fallar.

### Reglas decididas el 7 oct 2026

Cada una tiene su tabla de casos en el plan de su fase:

| Regla | Dónde están los casos |
|---|---|
| "Hoy" según la zona horaria del perfil | [06](06-fase-3-movimientos-transferencias.md) §5 |
| Recuperación de contraseña: un solo uso, 30 minutos, guardado como hash | [04](04-fase-1-autenticacion.md) §13 |
| Eliminación real de la cuenta | [04](04-fase-1-autenticacion.md) §13 |
| Sin tipo de cambio no se opera en otra moneda | [05](05-fase-2-cuentas-categorias-ajustes.md) §7 |
| Nombre de categoría único por usuario y tipo, sin distinguir mayúsculas ni tildes; se devuelve siempre el original | [05](05-fase-2-cuentas-categorias-ajustes.md) §7 y la tabla de normalización de la sección 3 |
| Un par de monedas solo se guarda con un tipo de cambio mayor que cero | [05](05-fase-2-cuentas-categorias-ajustes.md) §7 |
| La moneda principal se elige al registrarse; "Efectivo" nace en ella | [04](04-fase-1-autenticacion.md) §13 |
| Eliminar el egreso de un pago devuelve la compra a pendiente; una compra pagada no se elimina | [08](08-fase-5-tarjetas-de-credito.md) §7 |
| No se archiva una tarjeta con compras pendientes | [08](08-fase-5-tarjetas-de-credito.md) §7 |
| El PDF es solo el resumen | [10](10-fase-7-exportar-pdf.md) §8 |
| Correo: se envía por `MailService`; en pruebas se captura en memoria y nunca sale a internet | [04](04-fase-1-autenticacion.md) §13 |
| Editar y eliminar compras pendientes sin tocar saldos | [08](08-fase-5-tarjetas-de-credito.md) §7 |
| Versiones de Prisma alineadas y sin `^` | Comprobación del pipeline ([03](03-fase-0-fundaciones.md) §7) |

### Autenticación y abuso

- Token ausente, mal formado, vencido, con firma alterada, con `alg: none`, firmado con otra clave, con otro emisor.
- Reutilización de un token de renovación ya rotado.
- Campos no declarados en el cuerpo (`userId`, `status`, `deletedAt`, `isArchived`) → `400`.
- Texto de inyección SQL en `q`, en nombres y en descripciones → se guarda o se busca como texto, sin efecto.
- Cuerpo de más de 100 KB → `413`.
- Límites de peticiones → `429` con `Retry-After`.
- Ningún error incluye trazas ni mensajes de la base.
- Tras ejecutar todas las pruebas, se revisa que la salida de registros no contenga ninguna contraseña ni token de los usados.

## 6. Concurrencia

Los casos donde dos peticiones a la vez podrían romper una regla:

| Caso | Esperado |
|---|---|
| Dos pagos simultáneos de la misma compra | Uno `200`, otro `409`, un solo egreso |
| Dos renovaciones simultáneas con el mismo token | Una funciona; la otra dispara la detección de reutilización |
| Dos creaciones simultáneas con el mismo `id` | Una sola fila |
| Dos registros simultáneos con el mismo correo | Un usuario; el otro recibe `409` |
| Dos categorías con el mismo nombre y tipo creadas a la vez | Una se crea; la otra recibe `409 CATEGORY_NAME_TAKEN` |
| Editar y pagar la misma compra pendiente a la vez | El egreso coincide siempre con la compra, o la edición recibe `409` |
| Eliminar y pagar la misma compra pendiente a la vez | Nunca queda un egreso de una compra eliminada |
| Dos usos simultáneos del mismo token de recuperación de contraseña | Uno funciona; el otro recibe `422` |
| Dos ejecuciones simultáneas de la tarea de tipo de cambio | Solo una escribe |

Se prueban lanzando las peticiones en paralelo y repitiendo el caso varias veces.

## 7. Contrato

- El pipeline genera `openapi.json` y lo compara con el de `main`.
- Quitar un campo, cambiar un tipo, volver obligatorio un parámetro o eliminar una ruta se marca como **cambio incompatible** y bloquea la integración, salvo que vaya a `/api/v2`.
- Agregar campos o rutas es compatible.
- Las apps deben ignorar los campos que no conocen ([14](14-integracion-android.md)).

## 8. Carga

Conjunto de datos generado: 10,000 usuarios, de los cuales uno "pesado" con 100,000 movimientos en 10 años y el resto con actividad típica.

| Escenario | Objetivo (percentil 95) |
|---|---|
| `GET /home` | Menos de 150 ms |
| `GET /accounts` del usuario pesado | Menos de 150 ms |
| `GET /entries` de un mes | Menos de 100 ms |
| `GET /reports/summary` de un año del usuario pesado | Menos de 300 ms |
| `POST /transactions` | Menos de 100 ms |
| PDF de resumen de un año | Menos de 2 s |
| Carga sostenida de uso mixto | Tasa de error por debajo del 0.1 % |

Los objetivos son de partida y se ajustan con las primeras mediciones en el servidor real. Además, se revisa con `EXPLAIN ANALYZE` que las consultas principales usan sus índices y no recorren tablas enteras.

## 9. Cobertura y calidad

| Ámbito | Mínimo |
|---|---|
| `common/money`, `common/dates` | 100 % de líneas y ramas |
| Servicios de negocio | 90 % |
| Global | 80 % |

El pipeline falla por debajo de estos mínimos. La cobertura es una alarma, no el objetivo: una regla de negocio sin una prueba que la nombre no está cubierta aunque sus líneas se ejecuten.

Para `common/money` se añaden pruebas basadas en propiedades: para montos y tasas generados al azar, convertir de ida y vuelta difiere como mucho en un céntimo; la suma no depende del orden; el resultado siempre tiene 2 decimales.

## 10. Qué hace falta para integrar un cambio

1. Lint, formato y tipos en verde.
2. Unitarias, integración y e2e en verde.
3. Cobertura por encima de los mínimos.
4. Si hay una ruta nueva: está en la matriz de aislamiento.
5. Si hay un cambio de esquema: migración probada contra la semilla.
6. Contrato sin cambios incompatibles.
7. Revisión de otra persona.

## 11. Mantenimiento

- Una prueba que falla de forma intermitente se arregla o se retira ese mismo día; no se deja "reintentando".
- Cada error encontrado en producción genera primero una prueba que lo reproduce y después el arreglo.
- El conjunto completo debe correr en pocos minutos; si crece demasiado, se paralelizan los archivos e2e, cada uno con su propia base.
