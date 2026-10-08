# 15 · Después: Google y otras extensiones

Lo que **no** se construye en las fases 0 a 7, pero para lo que el diseño deja el camino libre. Cada apartado explica qué habría que hacer y qué decisión actual lo facilita. Nada de esto debe iniciarse sin pedirlo.

## 1. Acceso con Google

Decidido para después del lanzamiento con correo y contraseña.

**Flujo.** La app obtiene un token de identidad de Google (en Android, con Credential Manager) y lo envía al API. El API lo verifica y entrega **sus propios tokens**, los mismos de siempre. A partir de ahí nada cambia: el resto del API no sabe cómo entró el usuario.

`POST /auth/google` con `idToken` → `200` con `user`, `accessToken` y `refreshToken`.

**Verificación del token de Google**, en el servidor:

- Firma válida contra las claves públicas de Google.
- Emisor de Google.
- Audiencia igual al identificador de cliente de la app (uno por plataforma).
- No vencido.
- Correo marcado como verificado por Google.

**Cambios de esquema** (una migración que solo agrega):

| Cambio | Detalle |
|---|---|
| Tabla `auth_identities` | `id`, `user_id`, `provider` (`google`), `provider_user_id`, `email`, `created_at`. Único por `(provider, provider_user_id)` |
| `users.password_hash` pasa a admitir nulo | Un usuario que solo entra con Google no tiene contraseña |

**Reglas de vinculación**, que conviene decidir antes de implementar:

| Caso | Comportamiento recomendado |
|---|---|
| El correo de Google no existe | Se crea el usuario con el alta inicial de siempre |
| Ya existe una cuenta con ese correo y contraseña, **con el correo verificado** | Se vincula la identidad de Google a esa cuenta |
| Ya existe, pero **sin verificar** | No se vincula sola: se pide iniciar sesión con contraseña primero. De lo contrario, alguien que registró un correo ajeno se quedaría con acceso a la cuenta del dueño real |
| Usuario de Google que quiere contraseña | Se le ofrece el flujo de "recuperar contraseña" |
| Usuario con ambas formas que quiere desvincular Google | Solo si le queda otra forma de entrar |

Por esto la **verificación de correo (Fase 1b) es requisito previo** de Google.

**Qué lo facilita hoy:** las sesiones y los tokens son independientes de cómo se autentica el usuario; el alta inicial es un servicio reutilizable; la identidad sale siempre del token propio del API.

## 2. Sincronización sin internet

Quedó fuera de alcance por decisión del usuario. El diseño ya tiene lo necesario del lado del servidor:

| Ya existe | Para qué servirá |
|---|---|
| Identificadores UUID generados en el cliente | Crear registros sin conexión |
| `updated_at` en todas las tablas | Saber qué cambió desde la última sincronización |
| `deleted_at` (borrado lógico) | Propagar eliminaciones |
| Creación idempotente por `id` | Reintentar una cola de operaciones sin duplicar |

Lo que faltaría: un endpoint de cambios desde un instante (`GET /sync?since=`), una política de conflictos cuando dos dispositivos editan lo mismo, y del lado de Android, una base local y una cola de operaciones pendientes.

El punto delicado será el orden: un movimiento creado sin conexión puede referirse a una cuenta también creada sin conexión, y debe llegar después que ella.

## 3. Cliente web

El repositorio `atm-orbita-web` existe y está vacío. El API ya contempla:

- CORS con lista blanca de orígenes.
- Token de renovación en una cookie `HttpOnly`, `Secure`, `SameSite=Strict` ([11](11-seguridad.md), sección 6).
- Un contrato OpenAPI del que se puede generar el cliente tipado.
- Los vectores de prueba compartidos para dinero ([12](12-pruebas.md), sección 3).

En la web no debe usarse `number` para montos: hace falta una biblioteca de decimales, igual que en el servidor.

## 4. iOS

`ORBITA_SPEC.md` ya está escrito para portar la app. Con el API, iOS solo necesita un cliente HTTP y el mismo esquema de tokens que Android, guardando el token de renovación en el llavero del sistema. El acceso con Apple se añadiría como otro proveedor en `auth_identities`, con el mismo patrón que Google. Las reglas de la tienda de Apple ponen condiciones a las apps que ofrecen acceso con terceros como Google; hay que revisar la norma vigente antes de publicar.

## 5. Notificaciones de vencimiento

Las compras con tarjeta tienen fecha límite, y hoy la app solo avisa si se abre.

- Tabla de dispositivos con su token de notificaciones.
- Tarea diaria (con el mismo candado de PostgreSQL que la de tipo de cambio) que busca compras que vencen en 3 días, mañana u hoy.
- Envío por el servicio de notificaciones de cada plataforma.
- Preferencia en Ajustes para activarlas.

La hora de envío debe respetar la zona horaria del perfil, que ya se guarda.

## 6. Otras ideas del plan original

| Idea | Nota |
|---|---|
| Presupuestos por categoría | Tabla nueva; se calcula con la misma consulta de reportes |
| Gastos e ingresos recurrentes | Plantillas y una tarea que crea los movimientos; cuidado con la zona horaria |
| Cuotas y fecha de corte de tarjeta | Amplía `credit_cards` y `credit_purchases`; hoy fuera de alcance por decisión de producto |
| Subcategorías | Cambia el modelo de categorías y los reportes |
| Conversión histórica por fecha | `exchange_rates` ya guarda el historial; faltaría elegir el tipo de la fecha del movimiento |
| Exportar todos los datos (JSON o CSV) | Complementa el PDF y el derecho de portabilidad |
| Segundo factor de autenticación | Código temporal, sobre el esquema de sesiones actual |
| Hora editable del movimiento | Se decidió "solo fecha". Añadir `occurred_at` después es una migración que solo agrega |
| Deshacer el pago desde la app | El endpoint `unpay` ya queda hecho en la Fase 5 |

## 7. Criterio para priorizar

Antes de empezar cualquiera de estas extensiones:

1. ¿Lo pidió el usuario o es una suposición?
2. ¿Cambia alguna regla que hoy está garantizada (dinero exacto, saldo calculado, aislamiento)?
3. ¿Se puede hacer con una migración que solo agrega?
4. ¿Qué habría que vigilar, respaldar o mantener que hoy no existe?
