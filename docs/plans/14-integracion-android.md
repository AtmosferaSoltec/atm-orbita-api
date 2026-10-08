# 14 · Integración con Android

Qué tiene que cambiar en `atm-orbita-android` para dejar Supabase y consumir este API. Este documento vive en el repositorio del API porque el contrato lo define el API; los cambios de código se hacen en el repositorio de Android.

**Estado al 7 oct 2026.** En Android ya se aplicó: `allowBackup="false"` con sus reglas de respaldo (sección 7), la corrección de los documentos y **la retirada completa de Supabase** del repositorio (sección 8). Además, **las reglas decididas ese día ya están en el código de Android**, funcionando sobre el modo demo (detalle en la sección 5). Sigue pendiente lo que necesita el API: el cliente HTTP, los repositorios remotos y las pantallas de recuperar contraseña, eliminar cuenta y moneda al registrarse.

## 1. Lo que no cambia

La arquitectura de Android ya estaba pensada para esto. Se conservan tal cual:

- Todas las pantallas y los ViewModels.
- `domain/model` y `domain/repository` (salvo los ajustes de la sección 5).
- `data/demo`: el modo demo sigue siendo útil para vistas previas, pruebas y para abrir la app sin servidor.
- `core/Formatters.kt`, `core/AmountEntry.kt`, el teclado de monto y el calendario.
- `BigDecimalSerializer`: ya lee números y textos como decimal exacto y escribe texto, que es justo lo que pide el API.

El trabajo se concentra en `data/remote` y `di/AppModule.kt`.

## 2. Dependencias y configuración

| Antes | Ahora | Estado |
|---|---|---|
| `supabase-bom`, `auth-kt`, `postgrest-kt` | Cliente HTTP Ktor con negociación de contenido JSON, autenticación *bearer* y tiempos de espera | Dependencias de Supabase **retiradas**. `ktor-client-okhttp` se conserva; faltan los módulos de Ktor para JSON y autenticación |
| `SUPABASE_URL`, `SUPABASE_ANON_KEY` en `local.properties` y `BuildConfig` | `API_BASE_URL` | **Hecho** |
| `AppConfig.usesSupabase` | `AppConfig.usesApi` (verdadero si `API_BASE_URL` no está vacío; si lo está, modo demo) | **Hecho** |
| `provideSupabaseClient` | `provideHttpClient` y `provideTokenStore` | Retirado el primero; los nuevos, pendientes |
| `SupabaseAuthRepository` | Repositorio de sesión contra el API | Retirado el primero; el nuevo, pendiente. Mientras tanto la sesión es la de demostración |

Perfiles: `debug` apunta al API local o de pruebas; `release`, al de producción. En `release` solo se admite HTTPS.

## 3. Sesión y tokens

Reemplaza a `SupabaseAuthRepository`. Con Supabase la biblioteca guardaba y renovaba la sesión sola; ahora lo hace la app.

**Almacenamiento.** `TokenStore` guarda el token de renovación cifrado con una clave del Android Keystore. El token de acceso se mantiene solo en memoria.

**Flujo.**

| Momento | Qué hace la app |
|---|---|
| Arranque | Si hay token de renovación, `SessionState.Loading` → `POST /auth/refresh` → `SignedIn`. Si falla con `401`, borra el token y pasa a `SignedOut`. Si falla por red, mantiene la sesión y reintenta |
| Cada petición | Envía `Authorization: Bearer <token de acceso>` |
| Respuesta `401` | Renueva una vez y repite la petición. Si la renovación da `401`, cierra la sesión |
| Cerrar sesión | `POST /auth/logout` sin esperar el resultado; borra los tokens siempre, aunque no haya red (como hoy) |

**Renovación de una en una.** Si varias peticiones reciben `401` a la vez, **solo una** renueva y las demás esperan su resultado. Si dos renovaran en paralelo con el mismo token, el servidor lo interpretaría como un robo y cerraría la sesión ([04](04-fase-1-autenticacion.md), sección 15). Se resuelve con un `Mutex` alrededor de la renovación.

**Registro.** `POST /auth/register` envía también la zona horaria del dispositivo (`TimeZone.getDefault().id`).

## 4. Repositorios remotos

Los repositorios del dominio exponen `Flow` que reemiten cuando los datos cambian. Un API REST no avisa de los cambios, así que cada repositorio remoto mantiene su propia caché observable:

```kotlin
class ApiAccountsRepository(private val api: OrbitaApi) : AccountsRepository {
    private val cache = MutableStateFlow<List<Account>?>(null)

    override fun observeAccounts(): Flow<List<Account>> =
        cache.onStart { if (cache.value == null) refresh() }.filterNotNull()

    override suspend fun create(draft: AccountDraft) {
        api.createAccount(draft.toRequest(id = UUID.randomUUID()))
        refresh()
    }

    suspend fun refresh() { cache.value = api.getAccounts().toDomain() }
}
```

Reglas:

- Tras cada escritura correcta se recargan los datos afectados. Un movimiento nuevo cambia saldos, la lista, el reporte del mes y el Inicio: un `DataInvalidator` compartido avisa a los repositorios que deben recargar.
- Al volver la app a primer plano y al entrar en una pestaña se recarga, enviando `If-None-Match` para que el servidor responda `304` si nada cambió.
- El identificador se genera en el cliente (`UUID.randomUUID()`) y se envía al crear. Si la red falla y se reintenta, el servidor no duplica.
- Las fechas van como `YYYY-MM-DD` y los montos como texto con `toPlainString()`.
- Se configuran `ignoreUnknownKeys = true` y `explicitNulls = false` en JSON, para que un campo nuevo del API no rompa versiones antiguas de la app.

| Repositorio | Endpoints |
|---|---|
| `AuthRepository` | `/auth/register`, `/auth/login`, `/auth/refresh`, `/auth/logout` |
| `SettingsRepository` | `GET /settings`, `PUT /settings/fx`, `PATCH /settings` |
| `AccountsRepository` | `GET/POST /accounts`, `PATCH /accounts/{id}`, `POST …/archive` |
| `CategoriesRepository` | `GET/POST /categories`, `PATCH /categories/{id}`, `POST …/archive` |
| `EntriesRepository` | `GET /entries`, `/transactions`, `/transfers` |
| `ReportsRepository` | `GET /reports/summary` |
| `CreditRepository` | `/credit-cards`, `/credit-purchases`, `…/pay` |

`HomeViewModel` hoy combina cinco flujos. Puede seguir así (cada repositorio con su caché) o leer `GET /home` de una vez; lo segundo abre la app con una sola petición y es lo recomendado.

## 5. Cambios en el dominio

| Cambio | Motivo |
|---|---|
| `DateProvider` real: `LocalDate.now()` del dispositivo; `DemoDateProvider` solo en modo demo | Hoy "hoy" está fijo en 2 oct 2026 para todos |
| `FxPair.rate` pasa de `BigDecimal` a **`BigDecimal?`** | Decidido: un usuario nuevo no tiene tipo de cambio (detalle abajo) |
| `DataError`: `NAME_TAKEN`, `FX_NOT_CONFIGURED`, `PURCHASE_ALREADY_PAID` y `CARD_HAS_PENDING_PURCHASES` | **Hecho.** Nombre repetido, otra moneda sin tipo de cambio, compra ya pagada, tarjeta con deuda |
| `DataError`: agregar `RATE_LIMITED` y `SESSION_EXPIRED` | Pendiente, con el cliente del API: límite de peticiones, sesión revocada |
| Nuevos textos en `strings.xml` | Ver la tabla de la sección 6 |
| `CreditRepository`: agregar `updatePurchase` y `deletePurchase` | Editar y eliminar compras pendientes, aprobado |
| `Movement` gana `creditPurchaseId: String?` | Saber si un egreso es el pago de una compra con tarjeta. Al eliminarlo, la compra vuelve a pendiente, y la app debe avisarlo antes: "Este egreso es el pago de una compra con tarjeta. Al eliminarlo, la compra volverá a estar pendiente." |
| `DemoEntriesRepository.deleteMovement` debe devolver la compra a pendiente | Hoy borra el egreso y deja la compra pagada; el modo demo tiene que comportarse como el backend |
| Archivar una tarjeta con compras pendientes deja de permitirse | `409 CARD_HAS_PENDING_PURCHASES` → "Esta tarjeta tiene pagos pendientes. Págalos o elimínalos antes de archivarla." También en `DemoCreditRepository.archiveCard` |
| `Credentials` o el registro llevan `mainCurrency` y `timezone` | La moneda principal se elige al registrarse: la app propone la de la región del teléfono (`Currency.getInstance(Locale.getDefault())`) si está entre las 9 admitidas, o soles; el usuario puede cambiarla en la pantalla de registro |
| `AuthRepository`: agregar `requestPasswordReset`, `resetPassword` y `deleteAccount` | Requisitos de salida a producción |

### Aplicado en Android el 7 oct 2026

Sobre el modo demo, verificado con `./gradlew :app:assembleDebug :app:testDebugUnitTest` (31 pruebas en verde, 11 de ellas nuevas). No se probó en un dispositivo.

| Regla | Dónde quedó |
|---|---|
| Tipo de cambio opcional | `FxPair.rate: BigDecimal?`; enlace "Configura tu tipo de cambio ›" en Inicio y Cuentas; moneda fija en Nueva cuenta y Nueva/Editar tarjeta; campo vacío en Ajustes y en Transferir al cambiar de par |
| Un par solo se guarda con su tipo de cambio | `SettingsScreen` retiene el par nuevo sin guardar; `SettingsViewModel.setFx` valida; `DemoSettingsRepository.setFx` rechaza |
| Nombre de categoría único | `normalizeName` en `domain/model/Names.kt`; el formulario lo comprueba al guardar y muestra el mensaje bajo el campo; `DemoCategoriesRepository` lo impone |
| Editar y eliminar una compra pendiente | `CreditRepository.updatePurchase` y `deletePurchase`; se abre tocando la compra en Crédito y reutiliza `MovementFormScreen` |
| Eliminar el egreso de un pago | `Movement.creditPurchaseId`; `DemoEntriesRepository.deleteMovement` devuelve la compra a pendiente; el diálogo lo avisa |
| No archivar una tarjeta con pagos pendientes | `DemoCreditRepository.archiveCard` lo rechaza; el formulario muestra el motivo |

Con el API real, el `409 CATEGORY_NAME_TAKEN` deberá llegar también bajo el campo; hoy esa ruta la cubre la comprobación local del formulario, y el error del repositorio cae al aviso general.

### Tipo de cambio opcional

Decisión del usuario. El flujo completo y el criterio de bloqueo están en [05](05-fase-2-cuentas-categorias-ajustes.md) §3; aquí va lo que toca en Android.

En `domain/model/FxPair.kt`:

- `rate: BigDecimal?`. Se agrega `val isConfigured: Boolean get() = rate != null`.
- `convert` y `rateBetween` ya devuelven un valor opcional; con `rate` nulo devuelven `null` para monedas distintas y siguen devolviendo el mismo monto para la misma moneda.
- `savingsTotal`: sin tipo de cambio suma solo las cuentas que ya están en la moneda pedida.
- `swapped()` no puede invertir un cambio nulo: intercambia las monedas y deja `rate` en nulo.
- `withMain` y `withSecondary`: al elegir una moneda fuera del par, `rate` pasa a `null` en lugar de `1.00`. **Decidido:** el campo queda vacío hasta que el usuario escriba.
- La pantalla de Ajustes deja de guardar en cada cambio de desplegable. Con un par nuevo y el campo vacío **no llama** a `setFx`; lo hace cuando hay un valor mayor que cero. Si el usuario sale antes, se descarta el par nuevo. El backend rechaza igualmente un par sin tipo de cambio (`RATE_REQUIRED`).
- En Transferir, una moneda fuera del par también arranca con el tipo de cambio vacío, por la misma razón.

En las pantallas:

| Pantalla | Cambio |
|---|---|
| Inicio y Cuentas | Con `rate` nulo: selector `S/ \| US$` desactivado y, en lugar de "o US$ …", el enlace "Configura tu tipo de cambio ›" hacia Ajustes |
| Nueva cuenta, Nueva/Editar tarjeta | Desplegable de moneda fijo en la principal, con la ayuda "Configura tu tipo de cambio en Ajustes para usar otras monedas." |
| Ajustes | Campo del tipo de cambio vacío y resaltado con "Configura tu tipo de cambio"; el inverso muestra "—"; no se guarda hasta escribir un valor mayor que cero |
| Transferir | Sin cambios: sin tipo de cambio no pueden existir dos cuentas de monedas distintas |

`SampleData` conserva su cambio de 3.20, así que el modo demo y las pruebas actuales no cambian. Se agregan pruebas unitarias de `FxPair` con `rate` nulo.

### Categorías: nombre repetido

El formulario de categoría es el primero que muestra un error **bajo el campo** en vez de en el aviso flotante. `OrbitaTextField` necesita un parámetro de error (texto en rojo bajo el campo y borde rojo), que se limpia cuando el usuario cambia el texto.

La comparación no distingue mayúsculas ni tildes, y la hace el backend sobre una columna propia (`name_normalized`) que la app **nunca envía ni recibe**. `Category.name` sigue siendo el nombre tal como se escribió y es lo único que se muestra. El modelo de dominio no cambia. Si la app quiere avisar antes de enviar, necesita la misma normalización que el backend (minúsculas, sin tildes, `ñ` conservada), comprobada con los vectores compartidos; no es obligatorio.

### Otros cambios

| Cambio | Motivo |
|---|---|
| Leer `savings` del servidor en lugar de calcularlo solo con `FxPair` | En modo de cambio automático el servidor convierte también terceras monedas ([09](09-fase-6-tipo-de-cambio-automatico.md)) |
| `Account` embebida en un movimiento llega sin saldo | La lista no necesita el saldo; al editar se toma de `observeAccounts()` |
| `CreditCard` gana `isArchived` | Una compra pendiente puede pertenecer a una tarjeta archivada (H7) |

## 6. Traducción de errores

La función `translating { }` de `SupabaseAuthRepository` se reescribe para leer el `code` del cuerpo `problem+json`:

| Respuesta del API | En Android |
|---|---|
| Sin conexión, tiempo agotado, `502`, `503`, `504` | `DataError.NETWORK` |
| `401 INVALID_CREDENTIALS` | `DataError.INVALID_CREDENTIALS` |
| `401 UNAUTHENTICATED` tras fallar la renovación | `DataError.SESSION_EXPIRED` y cierre de sesión |
| `403 EMAIL_NOT_CONFIRMED` | `DataError.EMAIL_NOT_CONFIRMED` |
| `404 NOT_FOUND` | `DataError.NOT_FOUND` |
| `409 EMAIL_ALREADY_REGISTERED` | `DataError.EMAIL_ALREADY_REGISTERED` |
| `409 CATEGORY_NAME_TAKEN` | `DataError.NAME_TAKEN`, mostrado **bajo el campo Nombre** |
| `422 FX_RATE_NOT_CONFIGURED` | `DataError.FX_NOT_CONFIGURED` |
| `409 PURCHASE_ALREADY_PAID` | `DataError.CONFLICT` |
| `409 CARD_HAS_PENDING_PURCHASES` | Mensaje propio en Editar tarjeta |
| `422 RESET_TOKEN_INVALID` | Mensaje propio en la pantalla de nueva contraseña |
| `400` / `422 VALIDATION_FAILED` | `UiMessage.Invalid(ValidationError)` con el primer `errors[].code`, si coincide con el enumerado; si no, `DataError.UNKNOWN` |
| `429 RATE_LIMITED` | `DataError.RATE_LIMITED` |
| Cualquier otro | `DataError.UNKNOWN` |

Textos nuevos en `strings.xml`:

| Caso | Texto |
|---|---|
| Nombre de categoría repetido (bajo el campo) | "Ya tienes una categoría de este tipo con ese nombre." |
| Operación en otra moneda sin tipo de cambio | "Configura tu tipo de cambio en Ajustes para usar otra moneda." |
| Estado en Inicio, Cuentas y Ajustes | "Configura tu tipo de cambio" |
| Compra ya pagada | "Esta compra ya está pagada." |
| Archivar una tarjeta con pagos pendientes | "Esta tarjeta tiene pagos pendientes. Págalos o elimínalos antes de archivarla." |
| Confirmación al eliminar el egreso de un pago | "Este egreso es el pago de una compra con tarjeta. Al eliminarlo, la compra volverá a estar pendiente." |
| Enlace de recuperación inválido | "El enlace ya no es válido. Pide uno nuevo." |
| Límite de peticiones | "Demasiados intentos. Espera un momento." |
| Sesión terminada | "Tu sesión terminó. Inicia sesión de nuevo." |

Las validaciones de los borradores (`MovementDraft.validate()` y demás) **se mantienen** en la app: dan respuesta inmediata sin red. El servidor vuelve a validar todo; es la autoridad.

## 7. Correcciones de seguridad en Android

### Respaldos del dispositivo — aplicado el 7 oct 2026

Decisión del usuario. Estaba en `allowBackup="true"`, contra lo que decía `docs/02` (H11): un respaldo podía llevarse la sesión a otro dispositivo.

| Archivo | Cambio |
|---|---|
| `app/src/main/AndroidManifest.xml` | `android:allowBackup="false"`. Se conservan `android:dataExtractionRules` y `android:fullBackupContent` |
| `app/src/main/res/xml/data_extraction_rules.xml` | **Android 12+ (API 31).** Excluye todos los dominios (`root`, `file`, `database`, `sharedpref`, `external` y sus variantes `device_*`) tanto en `<cloud-backup>` como en `<device-transfer>` |
| `app/src/main/res/xml/backup_rules.xml` | **Android 11 o inferior.** Excluye todos los dominios |

Por qué hacen falta las reglas además del atributo: en Android 12 o superior, `allowBackup="false"` desactiva el respaldo en la nube pero **no** la transferencia directa de un dispositivo a otro. Esa transferencia solo se controla con `<device-transfer>` en las reglas de extracción.

Excluirlo todo no cuesta nada al usuario: los datos viven en el backend y vuelven al iniciar sesión en el teléfono nuevo.

Verificado con `./gradlew :app:lintDebug`: ningún aviso sobre el manifiesto de respaldo ni sobre los dos XML. (El lint del proyecto falla por un error anterior y ajeno a este cambio, en `OrbitaApp.kt:108`.)

`docs/02-arquitectura-android.md` quedó alineado con el manifiesto.

### Pendiente

| Qué | Dónde | Por qué |
|---|---|---|
| Solo HTTPS en `release` | Configuración de seguridad de red | Los tokens no deben viajar en claro |
| No registrar tokens ni cuerpos de autenticación | Configuración de registro del cliente HTTP | Evitar fugas por los registros del dispositivo |
| Token de renovación cifrado con el Keystore | `TokenStore` | Protección en reposo |
| "Eliminar cuenta" en Ajustes: confirmación, contraseña y `DELETE /me` | Pantalla de Ajustes | Requisito de salida a producción y de Google Play ([04](04-fase-1-autenticacion.md) §6) |
| "¿Olvidaste tu contraseña?" en Iniciar sesión, y pantalla de nueva contraseña abierta desde el enlace del correo | Pantallas de autenticación, enlace profundo | Requisito de salida a producción ([04](04-fase-1-autenticacion.md) §5) |

## 8. Documentos de Android

Todos describían Supabase como backend. **Se corrigieron el 7 oct 2026**, junto con las decisiones de la segunda ronda (zona horaria, tipo de cambio opcional, categorías, compras pendientes, recuperación de contraseña, eliminación de cuenta y respaldos).

| Archivo | Qué se corrigió |
|---|---|
| `docs/CLAUDE.md` | Backend, stack, reglas, comandos y estado |
| `docs/01-vision-y-alcance.md` | Reglas de negocio 5, 9 y las nuevas; alcance; registro de decisiones |
| `docs/02-arquitectura-android.md` | Reescrito: backend propio, diagrama, stack, sesión con tokens, `FxPair.rate` opcional, respaldos alineados con el manifiesto |
| `docs/03-modelo-de-datos.md` | Reescrito como resumen; el esquema y el SQL viven en `docs/plans/02-modelo-de-datos.md` de este repositorio. Conserva los cálculos (sección 5) y los datos de ejemplo (sección 7) |
| `docs/04-guia-supabase.md` → `docs/04-guia-api.md` | Renombrado y reescrito: cómo levantar el API en local y conectar la app |
| `docs/05-pantallas-y-flujos.md` | Referencias a tablas y funciones SQL cambiadas por endpoints; estados nuevos |
| `docs/06-plan-y-pruebas.md` | Las fases pasan a depender de las del API; las pruebas de base de datos se mueven al API |
| `docs/ORBITA_SPEC.md` | Secciones 1, 2, 3, 4.1, los apartados "Backend" de cada pantalla, 7.11, 7.12, 9, 10, 12 y el registro de cambios |

### Supabase retirado del repositorio — 7 oct 2026

Decisión del usuario. Se buscó `supabase` en todo el repositorio (código, Gradle, recursos, propiedades y documentos) y se eliminó en la rama `chore/remove-supabase`, en dos commits separados y sin publicar:

| Commit | Contenido |
|---|---|
| `f03269f` | Elimina `supabase/migrations/0001_init.sql` y, con él, la carpeta `supabase/` |
| `b81bdc0` | Elimina el código, las dependencias y la configuración |

Detalle del segundo:

| Archivo | Cambio |
|---|---|
| `gradle/libs.versions.toml` | Fuera la versión `supabase` y las bibliotecas `supabase-bom`, `supabase-auth` y `supabase-postgrest` |
| `app/build.gradle.kts` | Fuera las tres dependencias y los campos `SUPABASE_URL` y `SUPABASE_ANON_KEY`; entra `API_BASE_URL` |
| `data/remote/SupabaseAuthRepository.kt` | Eliminado |
| `di/AppModule.kt` | Fuera el cliente de Supabase; `AppConfig` queda con `apiBaseUrl` y `usesApi`; la sesión usa la implementación de demostración |
| `ui/navigation/AppViewModel.kt` | `usesSupabase` → `usesApi` |
| Comentarios en `DemoRepositories.kt`, `Repositories.kt`, `OrbitaApp.kt` y `DemoRepositoriesTest.kt` | "Supabase" → "the API" |

Verificado después del cambio: `./gradlew :app:assembleDebug :app:testDebugUnitTest` termina bien, con las 20 pruebas en verde, y la búsqueda de `supabase` en código, Gradle y recursos no devuelve nada.

Variables de entorno: `local.properties` solo tenía `sdk.dir`; no había claves de Supabase guardadas. No existían archivos `.env` ni configuración de la CLI de Supabase.

Consecuencias:

- **La app queda entera en modo demo**, también la sesión. Antes, con las claves puestas, el inicio de sesión era real contra Supabase; eso ya no existe.
- `ktor-client-okhttp` queda sin uso hasta que se escriba el cliente del API. Se conserva porque es la biblioteca prevista para él.
- En los documentos de Android quedan menciones a Supabase **solo como historia**: la nota que explica el cambio de backend al inicio de cada documento y las filas antiguas del registro de cambios de `ORBITA_SPEC.md`. Ninguna lo describe como algo vigente.
- En este repositorio (`atm-orbita-api`) nunca hubo código ni dependencias de Supabase; los planes lo mencionan solo para explicar qué reemplaza cada pieza.

**Las migraciones de Prisma de `atm-orbita-api` son la única fuente del esquema.**

## 9. Orden de migración

Sigue el orden de las fases del API. Cada paso es **una línea** en `AppModule.kt`, y la app funciona entre un paso y otro porque los módulos aún no migrados siguen en modo demo.

| Paso | Repositorio a `data/remote` | Requiere del API |
|---|---|---|
| 1 | `AuthRepository`, `TokenStore`, cliente HTTP, traducción de errores | Fase 1 |
| 2 | `SettingsRepository`, `AccountsRepository`, `CategoriesRepository`, `DateProvider` real | Fase 2 |
| 3 | `EntriesRepository` | Fase 3 |
| 4 | `ReportsRepository`, `GET /home` | Fase 4 |
| 5 | `CreditRepository` | Fase 5 |
| 6 | Activar "Automático" en Ajustes | Fase 6 |
| 7 | Botón de PDF en Reportes | Fase 7 |

Advertencia para los pasos 2 a 4: mientras unos repositorios sean reales y otros de demostración, los datos no cuadran entre pantallas (cuentas reales con movimientos de ejemplo). Es aceptable en desarrollo; **no se publica** una versión en ese estado.

## 10. Pruebas en Android

- Los repositorios remotos se prueban contra un servidor HTTP simulado con respuestas grabadas del API real.
- `DemoRepositoriesTest` y `MoneyRulesTest` siguen en pie y pasan a leer además `golden-vectors.json` ([12](12-pruebas.md), sección 3), para demostrar que la app y el API calculan igual.
- Prueba de la renovación de una en una: varias peticiones simultáneas con el token vencido producen una sola llamada a `/auth/refresh`.
- Los flujos de interfaz de `docs/06` se ejecutan contra un API de pruebas con la semilla de ejemplo.
