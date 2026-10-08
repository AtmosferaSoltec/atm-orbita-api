# 11 · Seguridad

Plan transversal. Orbita guarda la vida financiera de las personas; un fallo aquí no es un error más. Cada control de este documento tiene una prueba automática asociada en [12](12-pruebas.md).

## 1. Qué se protege y de quién

| Activo | Daño si se compromete |
|---|---|
| Movimientos, saldos, deudas | Exposición de la situación financiera de una persona |
| Credenciales y sesiones | Suplantación |
| Integridad de los datos | Saldos y reportes falsos |
| Disponibilidad | La app no funciona (solo opera con internet) |

| Amenaza | Ejemplo |
|---|---|
| Un usuario registrado curioso o malicioso | Cambia un identificador en la URL para ver datos de otro |
| Un atacante sin cuenta | Fuerza contraseñas, prueba listas de credenciales filtradas |
| Un cliente modificado | Envía campos que la app oficial nunca enviaría |
| Abuso de recursos | Miles de peticiones, rangos enormes, PDF masivos |
| Compromiso del servidor o de una dependencia | Fuga de la base de datos o de las claves |
| Un proveedor externo defectuoso | Tipo de cambio absurdo |

## 2. Aislamiento entre usuarios

Con Supabase lo imponía la base de datos (RLS). Ahora lo impone el API, en **cuatro capas independientes**: si una falla, las otras siguen en pie.

| Capa | Mecanismo |
|---|---|
| 1. Identidad | El `userId` sale **solo** del token verificado (`@CurrentUser()`). Ningún DTO, parámetro de ruta ni cabecera lo acepta |
| 2. Repositorio | Todo método recibe `userId` como primer parámetro obligatorio y lo incluye en el `where`. No existe `findById(id)` |
| 3. Base de datos | Claves foráneas compuestas `(id, user_id)`: un movimiento no puede apuntar a la cuenta de otra persona aunque el código lo intente |
| 4. Pruebas | Una matriz automática recorre **todas** las rutas con el usuario B intentando acceder a recursos de A |

Reglas derivadas:

- Un recurso ajeno responde `404`, igual que uno inexistente. Nunca `403`, que confirmaría que existe.
- Las referencias dentro del cuerpo (`accountId`, `categoryId`, `cardId`) se comprueban igual que las de la ruta.
- Una regla de lint prohíbe usar el cliente de Prisma fuera de los archivos `*.repository.ts`.
- La revisión de código de cualquier consulta nueva empieza por preguntar: ¿dónde está el `userId`?

**Row Level Security como quinta capa (endurecimiento posterior).** PostgreSQL puede aplicar además políticas por fila si el API fija el usuario en cada transacción (`SET LOCAL app.user_id`). Obliga a ejecutar cada petición dentro de una transacción y complica el uso de Prisma, así que no se incluye al inicio. Se reconsidera si el equipo crece o tras una auditoría externa.

## 3. Autenticación

El diseño completo está en [04](04-fase-1-autenticacion.md). Controles clave:

| Control | Detalle |
|---|---|
| Hash de contraseñas | Argon2id, con parámetros calibrados y recálculo al cambiar |
| Token de acceso | JWT ES256 de 15 minutos; se valida algoritmo, firma, emisor, audiencia y vencimiento |
| Token de renovación | Opaco, guardado como hash, rotado en cada uso, con detección de reutilización |
| Fuerza bruta | Límite por IP y por correo, con espera creciente |
| Enumeración | Mismo mensaje y mismo tiempo de respuesta para "no existe" y "contraseña incorrecta" |
| Revocación | Cierre de sesión, cierre en todos los dispositivos, cambio de contraseña |
| Seguro por defecto | Todas las rutas exigen token salvo las marcadas `@Public()` |

**Rotación de la clave de firma.** Se genera un par nuevo con un `kid` nuevo; el servidor firma con la nueva y sigue verificando la anterior durante 15 minutos (la vida de un token de acceso); después se retira. Ante una sospecha de fuga se retira de inmediato, lo que obliga a todas las apps a renovar su token.

## 4. Validación de entrada

- `ValidationPipe` global con `whitelist` y `forbidNonWhitelisted`: un campo que el DTO no declara provoca `400`. Así no se puede asignar `userId`, `isArchived`, `status` ni `deletedAt` por el cuerpo.
- Cada DTO declara tipo, formato y longitud máxima de todos sus campos.
- Montos: solo cadenas con el formato exacto. Nunca se interpreta un número JSON como dinero.
- Fechas: `YYYY-MM-DD` reales (se rechaza `2026-02-30`).
- Identificadores: UUID válidos; cualquier otra cosa da `400` antes de tocar la base.
- Monedas: del catálogo.
- Texto libre: se recortan espacios y se rechazan caracteres de control. No se "limpia" HTML: el API guarda texto y los clientes lo muestran como texto.
- Cursores de paginación: se validan al decodificar; uno manipulado da `400`.
- Cuerpo máximo de 100 KB; profundidad y número de parámetros de consulta acotados.

**Inyección SQL.** Prisma parametriza sus consultas. Las consultas escritas a mano usan solo plantillas etiquetadas, que envían los valores como parámetros. Está prohibido `$queryRawUnsafe` y `$executeRawUnsafe` (regla de lint). En la búsqueda `q` se escapan `%`, `_` y `\`.

## 5. Límites de consumo

| Recurso | Límite |
|---|---|
| Peticiones por usuario autenticado | 120 por minuto |
| Peticiones por IP sin autenticar | 30 por minuto |
| Rutas de autenticación | Las de [04](04-fase-1-autenticacion.md), sección 9 |
| PDF | 10 por hora por usuario; 4 simultáneos por réplica |
| Tamaño de página | Máximo 100 |
| Rango de un reporte | Máximo 10 años |
| Duración de una sentencia SQL | 5 s |
| Duración de una petición | 15 s (PDF: 20 s) |
| Tamaño del cuerpo | 100 KB |

Las respuestas `429` llevan `Retry-After`. Con una sola réplica el contador vive en memoria; al pasar a varias se mueve a Redis para que el límite sea global ([13](13-escalabilidad-y-operacion.md)).

El API confía en la IP que le pasa el proxy solo según `TRUST_PROXY`; mal configurado, cualquiera podría falsear su IP con una cabecera y saltarse los límites.

## 6. Transporte, cabeceras y cliente web

- **HTTPS obligatorio**, terminado en el proxy inverso, con HSTS. El API no se expone directamente.
- `helmet` con sus valores por defecto. El API solo sirve JSON y PDF, así que la política de contenido es la más estricta.
- **CORS** con lista blanca exacta (`CORS_ORIGINS`). Nunca `*`. Las apps móviles no usan CORS.
- Las respuestas con datos de usuario llevan `Cache-Control: private`.

Cliente web:

- El token de renovación va en una cookie `HttpOnly`, `Secure`, `SameSite=Strict`, restringida a la ruta de autenticación. El código de la página nunca puede leerlo.
- El token de acceso se mantiene solo en memoria de la página, no en `localStorage`.
- Con `SameSite=Strict` y una lista blanca de orígenes, las rutas que usan la cookie quedan protegidas de peticiones entre sitios; además se verifica la cabecera `Origin` en ellas.

## 7. Secretos

| Secreto | Dónde vive |
|---|---|
| Contraseñas de la base de datos | Secretos de Docker o del gestor de la plataforma |
| Clave privada de firma | Ídem; nunca en la imagen ni en el repositorio |
| Clave del proveedor de tipo de cambio | Ídem |
| Credenciales de correo | Ídem |

- El repositorio solo contiene `.env.example`, sin valores reales.
- El pipeline analiza cada cambio en busca de secretos y falla si encuentra uno.
- Los secretos nunca se registran. La configuración se imprime al arrancar solo con los nombres, no con los valores.
- Cada entorno (desarrollo, pruebas, producción) tiene secretos distintos.
- Si un secreto llega a publicarse, se rota: borrarlo del historial no basta.

## 8. Base de datos

- **Dos roles:** `orbita_migrator` (dueño del esquema, solo en migraciones) y `orbita_app` (lectura y escritura de datos, sin permisos de esquema).
- El puerto de PostgreSQL **no se publica** en producción: solo es alcanzable desde la red interna de Docker.
- Contraseñas largas y aleatorias; autenticación `scram-sha-256`.
- Volumen de datos en disco cifrado.
- Respaldos cifrados, guardados fuera del servidor, con restauración probada ([13](13-escalabilidad-y-operacion.md)).
- Tiempos máximos de sentencia, de bloqueo y de transacción inactiva.
- En producción no se ejecuta la semilla ni existe ningún usuario de ejemplo.

## 9. Dependencias y contenedor

- Versiones fijadas con `pnpm-lock.yaml`; instalación con `--frozen-lockfile`.
- `pnpm audit` en el pipeline; falla con severidad alta o crítica.
- Actualizaciones automáticas de dependencias propuestas como cambios revisables.
- Antes de agregar una dependencia: mantenimiento activo, número de dependencias que arrastra y si de verdad hace falta.
- Imagen mínima, **usuario sin privilegios**, sistema de archivos de solo lectura, sin capacidades adicionales.
- Análisis de vulnerabilidades de la imagen en el pipeline.
- La imagen de producción no contiene dependencias de desarrollo, código fuente TypeScript ni pruebas.

## 10. Registros y auditoría

- Se registra: petición, estado, duración, `requestId`, `userId`.
- **Nunca** se registra: contraseñas, tokens, cabecera `Authorization`, cookies, cuerpos con montos o descripciones.
- Eventos de seguridad (inicios de sesión, fallos, reutilización de token, cambios de contraseña, eliminación de cuenta) en un flujo identificable.
- Alertas: pico de `401` o `429`, reutilización de tokens, tasa de `500`, tarea de tipo de cambio fallida.
- Los errores devueltos al cliente no incluyen trazas, nombres de tablas ni mensajes de la base.

## 11. Privacidad

- **Eliminar la cuenta** (`DELETE /me`) borra físicamente al usuario y todos sus datos, en una transacción y con cascada ([04](04-fase-1-autenticacion.md) §6). Es requisito de salida a producción y de Google Play: debe poder hacerse desde la app y también desde una página web.
- **Recuperar la contraseña** es requisito de salida a producción: token de un solo uso, de 30 minutos, guardado como hash ([04](04-fase-1-autenticacion.md) §5).
- **Respaldos del dispositivo:** la app Android tiene `allowBackup="false"` y reglas de extracción que lo excluyen todo en Android 12+, para que la sesión no salga del teléfono.
- **Exportar mis datos:** el PDF cubre el resumen; un volcado completo en JSON o CSV se deja para después ([15](15-futuro.md)).
- Se guarda lo mínimo: correo y datos financieros que el usuario escribe. Sin nombre, teléfono ni ubicación.
- La IP y el agente de usuario de las sesiones se conservan solo mientras la sesión existe.
- Los respaldos que contienen datos de una cuenta eliminada caducan según la política de retención.
- La política de privacidad de la app debe actualizarse: el encargado de los datos ya no es Supabase, sino la infraestructura propia.

## 12. Correspondencia con OWASP API Security Top 10 (2023)

| Riesgo | Cómo se cubre |
|---|---|
| API1 Autorización a nivel de objeto rota | Sección 2: cuatro capas y matriz de pruebas |
| API2 Autenticación rota | Sección 3 y [04](04-fase-1-autenticacion.md) |
| API3 Autorización a nivel de propiedad rota | DTO con lista blanca; DTO de salida explícitos (nunca se devuelve una entidad de Prisma tal cual) |
| API4 Consumo de recursos sin restricción | Sección 5 |
| API5 Autorización a nivel de función rota | No hay roles ni rutas de administración; las tareas internas no tienen endpoint |
| API6 Acceso sin restricción a flujos sensibles | Límites en registro, recuperación de contraseña y PDF |
| API7 Falsificación de peticiones del lado del servidor | La única llamada saliente va a una URL fija de configuración |
| API8 Configuración de seguridad incorrecta | `helmet`, CORS con lista blanca, Swagger apagado en producción, errores sin trazas, entorno validado |
| API9 Gestión de inventario inadecuada | Versionado `/api/v1`, contrato OpenAPI generado y comparado en el pipeline |
| API10 Consumo inseguro de APIs | Validación de la respuesta del proveedor de tipo de cambio ([09](09-fase-6-tipo-de-cambio-automatico.md), sección 5) |

## 13. Lista de verificación antes de producción

- [ ] La matriz de aislamiento entre usuarios cubre el 100 % de las rutas y pasa
- [ ] Recuperación de contraseña operativa de principio a fin con correo real (requisito de salida)
- [ ] Dominio de correo con SPF, DKIM y DMARC; el correo de recuperación llega a la bandeja de entrada
- [ ] `DELETE /me` verificado: no queda ninguna fila del usuario (requisito de salida)
- [ ] "Eliminar cuenta" disponible en Ajustes de la app y en una página web declarada en Google Play
- [ ] HTTPS con HSTS; el API y PostgreSQL no son alcanzables desde internet directamente
- [ ] Secretos de producción distintos de los de desarrollo y fuera del repositorio
- [ ] Rol `orbita_app` sin permisos de esquema
- [ ] Swagger deshabilitado en producción
- [ ] Límites de peticiones verificados detrás del proxy real (IP correcta)
- [ ] Respaldo restaurado con éxito en un entorno de prueba
- [ ] `pnpm audit` y el análisis de la imagen sin hallazgos altos ni críticos
- [ ] Registros revisados: ninguna contraseña ni token en ellos
- [ ] Alertas configuradas y probadas
- [ ] Procedimiento de rotación de claves ensayado una vez
- [ ] Revisión de seguridad externa o, como mínimo, una pasada con una herramienta de análisis dinámico
- [ ] Política de privacidad actualizada y página web para eliminar la cuenta
