# 04 · Fase 1 — Autenticación propia

**Objetivo:** que una persona pueda registrarse, iniciar sesión, mantener la sesión abierta entre usos de la app, cerrarla, recuperar su contraseña si la olvida y eliminar su cuenta. Con correo y contraseña, sin depender de un tercero.

**Tamaño:** grande. **Depende de:** Fase 0. **Bloquea:** todas las fases de negocio.

Reemplaza a `SupabaseAuthRepository` de Android. El diseño deja listo el camino para agregar Google después ([15](15-futuro.md)).

## Alcance

**Fase 1a:** registro, inicio de sesión, renovación, cierre de sesión, datos de la cuenta, cambio de contraseña, **eliminar cuenta**, alta de datos iniciales.

**Fase 1b:** **recuperación de contraseña** y verificación de correo. Necesita un servicio de correo (sección 7).

**Decisión del usuario (7 oct 2026): recuperar la contraseña y eliminar la cuenta son requisito de salida a producción.** No se publica la app sin ambos. La Fase 1b puede desarrollarse después de la 1a para no bloquear las fases de negocio, pero debe estar terminada antes del lanzamiento.

No entra: Google, segundo factor, biometría (esta última es del cliente).

## 1. Modelo de sesión

Dos tokens con papeles distintos:

| | Token de acceso | Token de renovación |
|---|---|---|
| Forma | JWT firmado con ES256 | Cadena aleatoria opaca de 256 bits |
| Vida | 15 minutos | 30 días deslizantes, con tope absoluto de 90 días por sesión |
| Dónde se valida | En cada petición, sin consultar la base | En `POST /auth/refresh`, contra la base |
| Dónde se guarda en el servidor | En ningún lado | Solo su hash SHA-256 |
| Contenido | `sub` (usuario), `sid` (sesión), `iat`, `exp`, `iss`, `aud`, cabecera `kid` | Nada legible |

**Rotación.** Cada renovación entrega un token de renovación nuevo y marca el anterior como usado.

**Detección de reutilización.** Si llega un token de renovación ya usado, alguien más lo tiene: se revoca la sesión completa y se registra el evento. El usuario legítimo tendrá que volver a iniciar sesión.

**Revocación.** Cerrar sesión revoca la sesión en la base. Un token de acceso ya emitido sigue siendo válido hasta 15 minutos; es el costo aceptado de no consultar la base en cada petición. Cambiar o recuperar la contraseña y eliminar la cuenta revocan todas las sesiones.

**Claves de firma.** Par ES256. El servidor firma con la clave privada actual y verifica contra un conjunto de claves públicas identificadas por `kid`, lo que permite rotar la clave sin cerrar las sesiones abiertas.

## 2. Contraseñas

- Algoritmo: **Argon2id** con `@node-rs/argon2`. Parámetros de partida: 64 MiB de memoria, 3 iteraciones, paralelismo 1. Se ajustan midiendo en el servidor real hasta que una verificación tarde entre 150 y 300 ms.
- Longitud: mínimo 8 (regla de Android), máximo 128 (evita que una contraseña gigante agote la CPU).
- No se imponen reglas de composición (mayúsculas, símbolos); sí se rechaza una lista corta de contraseñas triviales.
- Si los parámetros cambian, el hash se recalcula en el siguiente inicio de sesión correcto.
- El correo se normaliza (sin espacios, en minúsculas) antes de guardar y de comparar.
- Al iniciar sesión con un correo que no existe, se verifica igualmente contra un hash ficticio, para que el tiempo de respuesta no revele si el correo está registrado.

## 3. Endpoints

| Método y ruta | Público | Cuerpo | Respuesta |
|---|---|---|---|
| `POST /auth/register` | Sí | `email`, `password`, `timezone?`, `mainCurrency?` | `201` con `user` y tokens. Si la verificación está activa: `201` con `user`, sin tokens |
| `POST /auth/login` | Sí | `email`, `password` | `200` con `user` y tokens |
| `POST /auth/refresh` | Sí | `refreshToken` | `200` con tokens nuevos |
| `POST /auth/logout` | Sí | `refreshToken` | `204` (siempre, aunque el token no exista) |
| `POST /auth/logout-all` | No | — | `204` |
| `POST /auth/forgot-password` | Sí | `email` | `204` **siempre**, exista o no el correo |
| `POST /auth/reset-password` | Sí | `token`, `newPassword` | `204`; cambia la contraseña y revoca todas las sesiones |
| `POST /auth/verify-email` | Sí | `token` | `204` |
| `POST /auth/resend-verification` | Sí | `email` | `204` siempre |
| `GET /me` | No | — | `id`, `email`, `emailVerified`, `createdAt` |
| `PATCH /me/password` | No | `currentPassword`, `newPassword` | `204`; revoca las demás sesiones |
| `DELETE /me` | No | `password` | `204`; borra al usuario y todos sus datos |
| `GET /me/sessions` | No | — | Sesiones activas (dispositivo, último uso) |
| `DELETE /me/sessions/{id}` | No | — | `204` |

Respuesta de tokens:

```json
{
  "user": { "id": "…", "email": "ana@correo.com", "emailVerified": false },
  "accessToken": "eyJ…",
  "refreshToken": "b64url…",
  "expiresIn": 900
}
```

## 4. Errores

| Situación | HTTP | `code` | Mensaje en Android |
|---|---|---|---|
| Correo con formato inválido | 400 | `VALIDATION_FAILED` / `EMAIL_INVALID` | "Escribe un correo válido." |
| Contraseña corta | 400 | `VALIDATION_FAILED` / `PASSWORD_TOO_SHORT` | "La contraseña debe tener al menos 8 caracteres." |
| Credenciales incorrectas | 401 | `INVALID_CREDENTIALS` | "Correo o contraseña incorrectos." |
| Correo ya registrado | 409 | `EMAIL_ALREADY_REGISTERED` | "Ese correo ya tiene una cuenta. Inicia sesión." |
| Correo sin confirmar | 403 | `EMAIL_NOT_CONFIRMED` | "Revisa tu correo y confirma tu cuenta…" |
| Token de recuperación inválido, usado o vencido | 422 | `RESET_TOKEN_INVALID` | "El enlace ya no es válido. Pide uno nuevo." |
| Token de acceso ausente, inválido o vencido | 401 | `UNAUTHENTICATED` | Renovar; si falla, volver a iniciar sesión |
| Token de renovación inválido, vencido o reutilizado | 401 | `UNAUTHENTICATED` | Volver a iniciar sesión |
| Demasiados intentos | 429 | `RATE_LIMITED` | Mensaje nuevo en Android |

"Correo o contraseña incorrectos" es deliberadamente el mismo mensaje para "no existe" y "contraseña equivocada". El registro sí revela que un correo existe (H15): es la experiencia que pide la especificación y se compensa con el límite de intentos.

## 5. Recuperación de contraseña

Requisito de salida a producción.

**Flujo**

1. En Iniciar sesión, "¿Olvidaste tu contraseña?" → la app pide el correo → `POST /auth/forgot-password`.
2. El servidor responde `204` **siempre**. Si el correo existe, genera un token y envía un correo con un enlace. Si no existe, no hace nada (y tarda lo mismo).
3. El enlace abre la app (enlace profundo) o, si no está instalada, una página web mínima. En ambos casos se pide la contraseña nueva.
4. `POST /auth/reset-password` con el token y la contraseña nueva.
5. El servidor, en una transacción: comprueba el token, guarda el hash de la contraseña nueva, marca el token como usado, invalida los demás tokens de recuperación del usuario y **revoca todas sus sesiones**.
6. La app vuelve a Iniciar sesión. No se inicia sesión automáticamente: quien recupera debe demostrar que sabe la contraseña nueva.

**El token**

| Propiedad | Valor |
|---|---|
| Generación | 32 bytes aleatorios de un generador criptográfico, codificados en base64url |
| Guardado | **Solo su hash SHA-256**, en `email_tokens`. El valor en claro viaja únicamente en el correo |
| Un solo uso | Se marca `used_at` en la misma transacción que cambia la contraseña. La marca se hace con una actualización condicionada (`WHERE used_at IS NULL AND expires_at > now()`), de modo que dos peticiones simultáneas no pueden usarlo dos veces |
| Vida | **30 minutos** |
| Pedir otro | Invalida los anteriores del mismo usuario: solo el último enlace funciona |
| Tras usarse | Deja de servir aunque no haya vencido |

Un token inválido, usado o vencido da siempre la misma respuesta (`422 RESET_TOKEN_INVALID`), sin decir cuál de las tres cosas pasó.

**Contenido del correo:** en español, con el enlace, la vigencia ("válido por 30 minutos") y la frase "si no lo pediste, ignora este mensaje; tu contraseña no cambia". Sin datos financieros. El enlace lleva el token en el fragmento o en la ruta de la app, nunca en un parámetro que quede en registros de servidores ajenos.

**Después de cambiar la contraseña** se envía un segundo correo de aviso ("tu contraseña se cambió"), para que el dueño de la cuenta se entere si no fue él.

La **verificación de correo** usa el mismo mecanismo y la misma tabla, con `purpose = verify_email` y vida de 24 horas. **Decisión del usuario: no es obligatoria al lanzar.** El usuario entra de inmediato tras registrarse. Queda implementada y apagada (`AUTH_REQUIRE_EMAIL_VERIFICATION=false`), lista para activarse por configuración; al activarla hay que revisar el plan de Resend, porque cada registro pasa a enviar un correo (sección 7.2).

## 6. Eliminar la cuenta

Requisito de salida a producción y de Google Play, que exige que una app con registro permita eliminar la cuenta **desde la propia app** y además mediante un enlace web.

**En la app:** Ajustes → "Eliminar cuenta" (acción destructiva, en rojo) → diálogo de confirmación que explica que se borran todos los datos y que no se puede deshacer → se pide la contraseña → `DELETE /me`.

**Fuera de la app:** una página web donde la persona inicia sesión y elimina su cuenta con el mismo endpoint. Es la URL que se declara en la ficha de Google Play.

**En el servidor**

- Exige la contraseña actual en el cuerpo, aunque el token sea válido: un teléfono desbloqueado en manos ajenas no basta para borrar la cuenta.
- **Borrado real, no lógico.** Dentro de `prisma.$transaction`:
  1. Se verifica la contraseña.
  2. Se elimina la fila de `users`.
  3. La base elimina en cascada todo lo demás: toda tabla con `user_id` lo referencia con `onDelete: Cascade` (`sessions`, `refresh_tokens`, `email_tokens`, `profiles`, `exchange_rates` propias, `accounts`, `categories`, `transactions`, `transfers`, `credit_cards`, `credit_purchases`).
- Las filas **globales** de tipo de cambio (`user_id` nulo) no se tocan.
- Si algo falla, la transacción se revierte y la cuenta queda intacta. No existe el estado "medio borrada".
- Se registra el evento de seguridad con el identificador del usuario, sin su correo.

Se elige la cascada declarada en el esquema en lugar de una lista de borrados escrita a mano porque la lista se queda corta el día que alguien agrega una tabla. Para que la cascada tampoco se quede corta hay una prueba que **descubre sola las tablas**: consulta el catálogo de PostgreSQL, toma todas las que tienen columna `user_id`, borra un usuario con datos en todas y comprueba que no queda ninguna fila suya. Una tabla nueva sin cascada hace fallar esa prueba.

**Efectos a tener presentes**

- El token de acceso del usuario eliminado sigue firmado hasta 15 minutos. Sus lecturas devuelven vacío y sus escrituras fallan por clave foránea; ese fallo se traduce a `401`.
- Los respaldos conservan los datos hasta que caducan según la retención ([13](13-escalabilidad-y-operacion.md) §5). Debe decirse en la política de privacidad.
- El correo queda libre: la misma persona puede registrarse de nuevo y empieza de cero.
- No hay periodo de gracia ni papelera. Si más adelante se quiere uno, es una decisión de producto.

## 7. Servicio de correo

**Decisión del usuario (7 oct 2026): Resend como proveedor inicial, detrás de una interfaz `MailService` para poder cambiarlo.**

El API envía muy poco correo: recuperación de contraseña, aviso de cambio y, si se activa, verificación. Lo que importa no es el precio sino que el mensaje **llegue a la bandeja de entrada en segundos**.

### 7.1 Integración

```ts
interface MailService {
  send(message: { to: string; subject: string; text: string; html: string }): Promise<void>;
}
```

| Entorno | Implementación | Destino |
|---|---|---|
| Producción | `SmtpMailService` (`nodemailer`) | SMTP de Resend |
| Desarrollo | `SmtpMailService` | Mailpit, el contenedor del `compose.yaml`: captura los correos y los muestra en el navegador |
| Pruebas | `InMemoryMailService` | Guarda los mensajes para comprobarlos |

El resto del API solo conoce `MailService`. Se conecta a Resend por **SMTP estándar** y no con su biblioteca propia, de modo que cambiar de proveedor es cambiar variables de entorno, sin tocar código. Si algún día se quiere usar la API HTTP de Resend (por ejemplo, para recibir avisos de rebote), se añade otra implementación de la misma interfaz.

### 7.2 Datos de Resend verificados

Consultados el **7 oct 2026** en las páginas oficiales indicadas. Son condiciones comerciales que cambian: hay que **volver a leerlas al contratar** y actualizar esta tabla.

| Dato | Valor leído | Fuente |
|---|---|---|
| Plan gratuito | US$ 0 al mes · **3,000 correos al mes** · **100 correos al día** · hasta 3 dominios | `resend.com/pricing` |
| Plan Pro (entrada) | US$ 20 al mes · 50,000 correos al mes · sin límite diario · hasta 10 dominios | `resend.com/pricing` |
| Excedente en Pro | US$ 0.90 por cada 1,000 correos adicionales | `resend.com/pricing` |
| Qué cuenta para la cuota | Correos enviados **y recibidos**; cada destinatario (Para, CC, CCO) cuenta como un correo | `resend.com/docs/knowledge-base/account-quotas-and-limits` |
| Límite de la API | 10 peticiones por segundo por equipo | Misma página |
| Umbrales de reputación | Tasa de rebote por debajo del 4 %; tasa de quejas por correo no deseado por debajo del 0.08 %. Superarlos puede suspender el envío | Misma página |
| Servidor SMTP | `smtp.resend.com` · usuario `resend` · contraseña: la clave de API | `resend.com/docs/send-with-smtp` |
| Puertos SMTP | `465` y `2465` con TLS implícito; `25`, `587` y `2587` con STARTTLS | Misma página |
| Verificación del dominio | Registros DKIM y SPF (`TXT` y `MX`, o `CNAME`). Suele verificarse en unos 15 minutos; la propagación puede tardar hasta 72 horas | `resend.com/docs/add-a-domain` |
| Ruta de retorno | Por defecto `send.<dominio>` | Misma página |
| Subdominio | Resend recomienda enviar desde un subdominio y no desde el dominio raíz | Misma página |
| Seguimiento de aperturas y clics | **Desactivado por defecto**; al activarlo, reescribe los enlaces del correo | `resend.com/docs/dashboard/domains/tracking` |

Lo que **no** quedó confirmado en esas páginas y hay que comprobar al contratar: si los correos enviados por SMTP cuentan para la cuota igual que los enviados por la API (es de esperar que sí), y qué ocurre exactamente al superar el límite diario del plan gratuito.

**¿Alcanza el plan gratuito?** El límite que manda es el **diario**, no el mensual.

| Escenario | Correos por operación | Tope con 100 al día |
|---|---|---|
| Recuperar contraseña | 2 (el enlace y el aviso de cambio) | Unas 50 recuperaciones al día |
| Verificación de correo activada | 1 por registro | Menos de 100 registros al día, y compitiendo con las recuperaciones |

Para el lanzamiento, con la verificación de correo apagada, el plan gratuito es suficiente. Hay que pasar al plan Pro al activar la verificación de correo o cuando el uso diario se acerque al tope. Por eso se añade una alerta al llegar al 70 % del límite diario: si se alcanza, los correos de recuperación dejarían de salir sin que el API lo note.

### 7.3 Verificación del dominio

Sin esto, los correos de recuperación caen en correo no deseado y la función no sirve. Se hace una vez, antes de la primera prueba real.

1. **Elegir el subdominio de envío.** El dominio es **`atmosferast.com`** (decisión del usuario). Se propone enviar desde el subdominio `mail.atmosferast.com`, con remitente `no-reply@mail.atmosferast.com`: un subdominio separa la reputación del correo transaccional de la del dominio principal, que es lo que recomienda Resend. La dirección exacta la fija el usuario en `MAIL_FROM`.
2. **Añadir el dominio en el panel de Resend.** El panel muestra los registros DNS exactos (nombre, tipo y valor) para ese dominio. **Se copian de ahí**: no se escriben de memoria ni se toman de este documento.
3. **Crear los registros en el proveedor de DNS:**

   | Registro | Para qué sirve |
   |---|---|
   | **SPF** (`TXT`) | Declara qué servidores pueden enviar correo en nombre del dominio |
   | **MX** en la ruta de retorno (`send.<subdominio>`) | Recibe los avisos de rebote; acompaña al SPF |
   | **DKIM** (`TXT`, o `CNAME` según indique el panel) | Publica la clave con la que Resend firma cada correo, para que el receptor compruebe que no fue alterado |

4. **Verificar en el panel.** Suele tardar unos 15 minutos; puede llegar a 72 horas. Si no verifica, se revisa que el proveedor de DNS no haya añadido el dominio dos veces al nombre del registro.
5. **DMARC** (recomendado, aunque no lo exige la verificación): un registro `TXT` en `_dmarc.<dominio>` que dice a los receptores qué hacer con el correo que no pasa SPF ni DKIM. Empezar con la política `p=none` y una dirección para recibir informes; cuando los informes confirmen que todo el correo legítimo pasa, subir a `quarantine`.
6. **Crear la clave de API** para el servidor, con el permiso mínimo que ofrezca el panel (solo envío, restringida a ese dominio). Una clave distinta por entorno.
7. **Confirmar que el seguimiento de aperturas y clics está desactivado** en el dominio. Activado, reescribiría el enlace de recuperación para pasarlo por un servidor de seguimiento.
8. **Prueba real.** Enviar un correo de recuperación a una cuenta de Gmail y a una de Outlook, comprobar que llega a la bandeja de entrada y revisar en las cabeceras del mensaje que `SPF`, `DKIM` y `DMARC` figuran como `pass`.

El dominio de pruebas y el de producción se verifican por separado.

### 7.4 Variables de entorno

| Variable | Producción (Resend) | Desarrollo (Mailpit) | Secreto |
|---|---|---|---|
| `MAIL_TRANSPORT` | `smtp` | `smtp` | No |
| `MAIL_SMTP_HOST` | `smtp.resend.com` | `mailpit` | No |
| `MAIL_SMTP_PORT` | `465` | `1025` | No |
| `MAIL_SMTP_SECURE` | `true` (TLS implícito) | `false` | No |
| `MAIL_SMTP_USER` | `resend` | *(vacío)* | No |
| `MAIL_SMTP_PASSWORD` | La clave de API de Resend | *(vacío)* | **Sí** |
| `MAIL_FROM` | Por ejemplo `Orbita <no-reply@mail.atmosferast.com>` | `Orbita <no-reply@orbita.local>` | No |
| `MAIL_REPLY_TO` | Opcional: un buzón de soporte | *(vacío)* | No |
| `PASSWORD_RESET_URL` | URL base del enlace de recuperación | La de desarrollo | No |
| `PASSWORD_RESET_TTL_MINUTES` | `30` | `30` | No |
| `EMAIL_VERIFY_URL`, `EMAIL_VERIFY_TTL_HOURS` | URL base y `24` | Igual | No |

**Estado (7 oct 2026):** las ocho variables `MAIL_*` ya están en `.env.example`, con los valores de Resend y con `MAIL_SMTP_PASSWORD` y `MAIL_FROM` **vacíos**: el usuario pondrá la clave de API y el remitente cuando cree la cuenta. Ningún código las lee todavía; `MailService` se escribe en la Fase 1b. Las variables de los enlaces (`PASSWORD_RESET_URL` y demás) se añaden entonces.

En las pruebas, `MAIL_TRANSPORT=memory` y no hace falta ninguna de las demás.

Se validan al arrancar, como el resto de la configuración ([01](01-arquitectura-y-convenciones.md) §10): con `MAIL_TRANSPORT=smtp` son obligatorios el servidor, el puerto y el remitente; en producción además el usuario y la contraseña, y **no se admite `memory`**. `MAIL_FROM` debe pertenecer al dominio verificado. `.env.example` las incluye todas sin valores reales.

Cambiar de proveedor: nuevos valores para `MAIL_SMTP_*` y `MAIL_FROM`, y verificar el dominio en el proveedor nuevo. Nada más.

### 7.5 Reglas de envío

- El envío no bloquea la respuesta: `forgot-password` responde de inmediato y el correo sale después. Si el envío falla se reintenta con espera creciente y se registra; la respuesta al cliente no cambia.
- No se registra el contenido del correo, ni el enlace, ni el token.
- Cada correo va a **un solo destinatario**, sin copias.
- Solo se envía a direcciones de cuentas existentes: `forgot-password` no envía nada a un correo que no está registrado, lo que además protege la tasa de rebote.
- Tiempo máximo de conexión y de envío de 10 segundos.
- Alertas: fallo de envío repetido y uso del 70 % del límite diario.

### 7.6 Alternativas

| Proveedor | Cuándo pasar a él |
|---|---|
| Amazon SES | Si el volumen crece mucho o la infraestructura termina en AWS |
| Postmark | Si la entrega en bandeja de entrada resulta un problema |

En ambos casos el cambio es de configuración, por la interfaz y por usar SMTP.

## 8. Alta de datos iniciales

`POST /auth/register` hace, **en una sola transacción**:

1. Crea el usuario.
2. Crea el perfil: moneda principal, moneda secundaria, visualización en la principal, cambio manual y zona horaria.
3. Crea las 9 categorías iniciales, con su `name_normalized`.
4. Crea la cuenta "Efectivo" **en la moneda principal**.
5. Crea la sesión y su primer token de renovación.

Si cualquier paso falla, no queda nada. Es el reemplazo del trigger `handle_new_user`, y la lista exacta de datos está en [02](02-modelo-de-datos.md), sección 8.

**Monedas con las que nace el usuario** (decisión del usuario, 7 oct 2026): la moneda principal **se elige al registrarse**.

| Dato | Regla |
|---|---|
| `mainCurrency` en el cuerpo | Opcional. Debe estar en el catálogo de 9 monedas; si no, `400 CURRENCY_NOT_SUPPORTED`. Si no se envía, **soles (PEN)** |
| Qué envía la app | La moneda de la región del teléfono, si está en el catálogo; el usuario puede cambiarla en la pantalla de registro. Si la región no corresponde a ninguna de las 9, soles |
| Moneda secundaria | **Dólares (USD)**; si la principal ya es dólares, **soles (PEN)**. Las dos nunca coinciden |
| Moneda de visualización | La principal |
| Cuenta "Efectivo" | En la moneda principal |

Así quien no usa soles no arranca con una cuenta en una moneda ajena, ni tiene que escribir un tipo de cambio solo para poder cambiar su moneda principal. Antes todo usuario nacía con PEN/USD y "Efectivo" en soles.

El usuario nace **sin tipo de cambio** (salvo que exista uno global que sembrar, Fase 6). Lo que eso implica está en [05](05-fase-2-cuentas-categorias-ajustes.md) §3.

## 9. Protección contra abuso

| Ruta | Límite de partida |
|---|---|
| `POST /auth/login` | 10 por minuto por IP, y 5 fallos por correo cada 15 minutos |
| `POST /auth/register` | 5 por hora por IP |
| `POST /auth/refresh` | 30 por minuto por IP |
| `POST /auth/forgot-password`, `resend-verification` | 3 por hora por correo y 10 por hora por IP |
| `POST /auth/reset-password` | 10 por hora por IP |
| `PATCH /me/password`, `DELETE /me` | 5 por hora por usuario |

Tras 5 fallos seguidos para un mismo correo se aplica una espera creciente (1, 5, 15 minutos) en lugar de bloquear la cuenta, para que nadie pueda dejar fuera a otra persona a propósito.

El límite por correo en `forgot-password` evita además que alguien inunde la bandeja de otra persona.

Se registran como eventos de seguridad: registro, inicio correcto y fallido, renovación, reutilización de token, cierre de sesión, cambio y recuperación de contraseña, eliminación de cuenta. Con `userId` cuando existe, IP y agente de usuario; nunca la contraseña ni los tokens.

## 10. Guard global

- Todo endpoint exige token de acceso salvo los marcados `@Public()`. Es **seguro por defecto**: olvidar una anotación deja la ruta protegida, no abierta.
- El guard verifica firma, `exp`, `iss`, `aud` y que el algoritmo sea exactamente ES256.
- `@CurrentUser()` entrega `{ userId, sessionId }`. **Es la única fuente de identidad**: ningún DTO acepta `userId`.
- Con `AUTH_REQUIRE_EMAIL_VERIFICATION=true`, las rutas de negocio responden `403 EMAIL_NOT_CONFIRMED` hasta verificar.

## 11. Cliente web

Las apps móviles envían y reciben el token de renovación en el cuerpo y lo guardan en almacenamiento cifrado. Para la web, el mismo endpoint lo entrega además como cookie `HttpOnly`, `Secure`, `SameSite=Strict`, con ruta `/api/v1/auth`, de modo que el código de la página nunca lo ve. El modo se elige con una cabecera del cliente. Detalle en [11](11-seguridad.md), sección 6.

## 12. Tareas

**1a**

- [ ] Migración `init_identity`: `users`, `sessions`, `refresh_tokens`
- [ ] Migración `init_finance`: `profiles`, `exchange_rates`, `accounts`, `categories`, con sus `CHECK` e índices (el alta las necesita)
- [ ] `PasswordHasher` (Argon2id) con calibración de parámetros
- [ ] `TokenService`: firmar y verificar el token de acceso; generar, guardar y rotar el de renovación
- [ ] Generación y carga del par de claves ES256; soporte de varias claves públicas
- [ ] `AuthService`: registro con alta inicial, inicio de sesión, renovación con detección de reutilización, cierre de sesión
- [ ] `OnboardingService` (perfil, categorías, cuenta)
- [ ] Guard global, `@Public()`, `@CurrentUser()`
- [ ] Endpoints de `/me` y de sesiones
- [ ] `DELETE /me` dentro de `prisma.$transaction`, con verificación de contraseña
- [ ] `onDelete: Cascade` en toda relación hacia `users`, y la prueba que descubre las tablas
- [ ] Traducir a `401` las escrituras de un usuario ya eliminado
- [ ] Límites de peticiones por ruta y espera creciente por correo
- [ ] Registro de eventos de seguridad
- [ ] Tarea de limpieza de sesiones y tokens vencidos

**1b**

- [ ] Migración `email_tokens`
- [ ] Releer precios y límites de Resend en su web y actualizar la tabla de la sección 7.2
- [ ] Crear la cuenta de Resend, añadir el subdominio de envío y verificarlo (SPF, MX de la ruta de retorno y DKIM); añadir DMARC
- [ ] Clave de API de solo envío por entorno, guardada como secreto
- [ ] Interfaz `MailService`, `SmtpMailService` (Resend en producción, Mailpit en desarrollo) e `InMemoryMailService` (pruebas)
- [ ] Variables `MAIL_*` y de enlaces en la validación del entorno y en `.env.example`
- [ ] Alerta de fallos de envío y del 70 % del límite diario
- [ ] `forgot-password` con respuesta uniforme y envío fuera de la petición
- [ ] `reset-password` transaccional, con token de un solo uso y revocación de sesiones
- [ ] Correo de aviso tras el cambio de contraseña
- [ ] Verificación de correo y reenvío
- [ ] Plantillas de correo en español, en texto y HTML
- [ ] Página web mínima para cambiar la contraseña y para eliminar la cuenta
- [ ] Limpieza de tokens de correo vencidos

## 13. Pruebas

Unitarias:

- El hash verifica la contraseña correcta y rechaza la incorrecta; dos hashes de la misma contraseña son distintos.
- Un token de acceso vencido, con firma alterada, con `alg: none` o firmado con otra clave se rechaza.
- La rotación invalida el token anterior.

e2e de sesión:

- Registro crea perfil, 9 categorías y la cuenta "Efectivo"; si se fuerza un fallo a mitad, no queda ni el usuario.
- Registrar dos veces el mismo correo (también con otras mayúsculas) da `409`.
- Registro sin `mainCurrency`: perfil PEN/USD y "Efectivo" en PEN. Con `MXN`: perfil MXN/USD y "Efectivo" en MXN. Con `USD`: perfil USD/PEN y "Efectivo" en USD. Con `XXX`: `400 CURRENCY_NOT_SUPPORTED` y no se crea el usuario.
- Inicio de sesión correcto; con contraseña errónea y con correo inexistente la respuesta es idéntica.
- Renovar entrega tokens nuevos; reutilizar el token anterior da `401` **y** deja inservible el token nuevo.
- Cerrar sesión impide renovar.
- Cambiar la contraseña revoca las otras sesiones y la contraseña vieja deja de servir.
- Toda ruta protegida devuelve `401` sin token.
- El sexto intento fallido seguido devuelve `429` con `Retry-After`.

e2e de recuperación de contraseña:

| Caso | Esperado |
|---|---|
| `forgot-password` con un correo existente | `204` y un correo capturado con un enlace |
| `forgot-password` con un correo inexistente | `204`, ningún correo, y el mismo tiempo de respuesta |
| La base tras pedir la recuperación | Contiene el hash del token, nunca el token |
| `reset-password` con el token del correo | `204`; la contraseña nueva sirve y la vieja no |
| Usar el mismo token otra vez | `422 RESET_TOKEN_INVALID` |
| Dos `reset-password` simultáneos con el mismo token | Uno `204`, otro `422` |
| Token con el reloj adelantado 31 minutos | `422 RESET_TOKEN_INVALID` |
| Pedir dos recuperaciones y usar el primer enlace | `422`; solo el segundo funciona |
| Tras recuperar | Todas las sesiones anteriores dejan de poder renovar |
| Token inventado | `422`, con la misma respuesta que uno vencido |
| Cuarta petición en una hora para el mismo correo | `429` |
| Contraseña nueva de 7 caracteres | `400 PASSWORD_TOO_SHORT` y el token **no** se consume |

e2e de eliminación de cuenta:

| Caso | Esperado |
|---|---|
| `DELETE /me` con la contraseña correcta | `204` |
| Tras eliminar, todas las tablas con `user_id` | Cero filas del usuario (prueba que descubre las tablas) |
| Los datos de otro usuario | Intactos, fila por fila |
| Las filas globales de tipo de cambio | Intactas |
| `DELETE /me` con contraseña incorrecta | `401` y no se borra nada |
| Fallo forzado a mitad de la transacción | El usuario y sus datos siguen completos |
| Iniciar sesión o renovar después | `401` |
| Usar el token de acceso aún vigente para escribir | `401` |
| Registrarse de nuevo con el mismo correo | `201`, cuenta nueva con solo los datos iniciales |

## 14. Criterios de aceptación

- Un usuario se registra, cierra la app, la reabre días después y sigue con sesión (renovación), y al cerrar sesión no puede renovar.
- Un usuario que olvidó su contraseña la recupera de principio a fin con un correo real, y el enlace no sirve una segunda vez ni pasados 30 minutos.
- Un usuario elimina su cuenta desde la app y no queda ninguna fila suya en la base.
- Ninguna contraseña ni token (de sesión o de correo) aparece en claro en la base ni en los registros.
- Un correo de recuperación enviado a Gmail y a Outlook llega a la bandeja de entrada, no a correo no deseado.
- La verificación de una contraseña tarda entre 150 y 300 ms en el servidor de destino.

## 15. Riesgos

| Riesgo | Mitigación |
|---|---|
| Dos peticiones simultáneas de la app renuevan a la vez y la segunda dispara la detección de reutilización | El cliente serializa la renovación (una sola a la vez). Documentado en [14](14-integracion-android.md) |
| Argon2 consume CPU y memoria: muchos inicios de sesión a la vez saturan el proceso | Límite de peticiones en las rutas de autenticación y tamaño del grupo de hilos dimensionado ([13](13-escalabilidad-y-operacion.md)) |
| Fuga de la clave privada de firma | Clave fuera del repositorio, rotación por `kid`, procedimiento documentado en [11](11-seguridad.md) |
| Los correos de recuperación caen en correo no deseado | SPF, DKIM y DMARC obligatorios; prueba real contra los proveedores más usados antes de lanzar |
| Se alcanza el límite diario del plan gratuito de Resend y los correos dejan de salir | Alerta al 70 % del límite; pasar al plan de pago al activar la verificación de correo |
| Resend cambia sus condiciones o deja de convenir | Interfaz `MailService` y SMTP estándar: cambiar de proveedor es configuración |
| El servicio de correo se cae | La recuperación deja de funcionar, pero el resto del API no se ve afectado; reintentos y alerta |
| Una tabla nueva queda fuera del borrado de cuenta | Prueba que descubre las tablas con `user_id` en el catálogo |
| Eliminación accidental de la cuenta | Contraseña obligatoria y diálogo de confirmación explícito |
