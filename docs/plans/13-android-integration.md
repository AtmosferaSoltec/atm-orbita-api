# 13 · Integración con Android (transversal)

## Objetivo

Describir la base común que Android necesita para hablar con el API (cliente HTTP, sesión, errores, mapeo de datos) y cómo cada plan pasa su repositorio del modo demo al API. **No es una entrega aparte**: la base se construye en el plan 01 y cada plan conecta su parte.

Sigue las convenciones de Android (`docs/CLAUDE.md` de ese proyecto): código en inglés, textos en `strings.xml`, `domain` en Kotlin puro, un `ViewModel` por pantalla, y cambiar de demo a API es **una línea** en `di/AppModule.kt`.

## Lo que ya existe en Android

- `domain/repository` con las interfaces que ven las pantallas (`AuthRepository`, `AccountsRepository`, `CategoriesRepository`, `EntriesRepository`, `ReportsRepository`, `CreditRepository`, `SettingsRepository`) y `DataError` / `ValidationError`.
- `data/demo` con todas las reglas en memoria. Se queda: sin `API_BASE_URL` la app sigue arrancando en modo demo.
- `AppConfig.usesApi` (`BuildConfig.API_BASE_URL` desde `local.properties`), `ktor-client-okhttp` en las dependencias y `data/remote/BigDecimalSerializer.kt`.
- `allowBackup="false"` y reglas de extracción que lo excluyen todo.

## Base común (se construye en el plan 01)

| Pieza | Qué hace |
|---|---|
| `data/remote/ApiClient` | Ktor con OkHttp, JSON (`ignoreUnknownKeys = true`, para que una app vieja siga funcionando cuando el API agregue campos), tiempos de espera, `X-Request-Id` por petición, base `API_BASE_URL` |
| `TokenStore` | Refresh token cifrado con el Android Keystore (DataStore + AES/GCM con una clave del Keystore); el access token solo en memoria |
| Renovación | Ante un `401`, **una sola** renovación a la vez (`Mutex`) y se repite la petición; si `refresh` responde `401 SESSION_EXPIRED`, se limpia la sesión y se vuelve a Iniciar sesión |
| `ApiErrors` | Lee `{ error: { code, message, details } }` y lo traduce a `DataException(DataError)`, o al primer `ValidationError` de `details` |
| DTOs | `@Serializable` con `@SerialName` en `snake_case`; todo monto y tipo de cambio con `BigDecimalSerializer`; fechas `YYYY-MM-DD` ↔ `LocalDate` |
| Caché observable | REST no avisa de cambios: cada repositorio remoto guarda su última lectura en un `MutableStateFlow`, la recarga tras cada escritura propia y al volver a primer plano. Las pantallas siguen recibiendo `Flow` como hoy |
| Sin conexión | `IOException` → `DataError.NETWORK` ("Sin conexión. Revisa tu internet e inténtalo de nuevo.") |
| Cerrar sesión | Siempre funciona, también sin red: borra los tokens locales y luego intenta `POST /auth/logout` |

**`BigDecimalSerializer`**: su comentario todavía habla de PostgREST; el API manda los montos como texto. Se actualiza el comentario; el código ya lee texto y número.

## Mapeo de errores (`code` del API → Android)

| `code` | Android |
|---|---|
| `VALIDATION_FAILED` | El primer `details[].code` → `ValidationError` del mismo nombre (`AMOUNT_REQUIRED`, `FUTURE_DATE`, …). Si no existe en Android, `DataError.UNKNOWN` |
| `INVALID_CREDENTIALS` | `DataError.INVALID_CREDENTIALS` |
| `EMAIL_ALREADY_REGISTERED` | `DataError.EMAIL_ALREADY_REGISTERED` |
| `NOT_FOUND` | `DataError.NOT_FOUND` |
| `CATEGORY_NAME_TAKEN` | `DataError.NAME_TAKEN` (mensaje bajo el campo) |
| `FX_RATE_NOT_CONFIGURED` | `DataError.FX_NOT_CONFIGURED` |
| `PURCHASE_ALREADY_PAID` | `DataError.PURCHASE_ALREADY_PAID` |
| `CARD_HAS_PENDING_PURCHASES` | `DataError.CARD_HAS_PENDING_PURCHASES` |
| `PAYMENT_EXPENSE_LOCKED` | `DataError.PAYMENT_LOCKED` |
| `SESSION_EXPIRED`, `UNAUTHORIZED` (tras renovar) | **Nuevo** `DataError.SESSION_EXPIRED`: "Tu sesión terminó. Inicia sesión de nuevo." |
| `TOO_MANY_REQUESTS` | **Nuevo** `DataError.TOO_MANY_REQUESTS`: "Demasiados intentos. Espera un momento." |
| `RESET_CODE_INVALID` | **Nuevo** `DataError.RESET_CODE_INVALID`: "El código no es válido o ya venció. Pide uno nuevo." |
| `ACCOUNT_ARCHIVED`, `CATEGORY_ARCHIVED`, `CARD_ARCHIVED`, `ID_CONFLICT`, `INTERNAL_ERROR`, otros | `DataError.UNKNOWN` ("Algo salió mal. Inténtalo de nuevo.") |

Los textos ya están en `ORBITA_SPEC.md` §4.1; los tres nuevos ya tienen su mensaje ahí.

## Qué cambia en `domain` de Android

| Cambio | Plan | Motivo |
|---|---|---|
| `DataError` + `SESSION_EXPIRED`, `TOO_MANY_REQUESTS`, `RESET_CODE_INVALID` | 01, 11 | Errores que el modo demo no podía dar |
| `ValidationError` + los códigos de campo nuevos del API que la app pueda mostrar (`CURRENCY_NOT_SUPPORTED`, `AMOUNTS_MUST_MATCH`…) o `UNKNOWN` | 02, 06 | Para no mostrar un error genérico cuando la app ya sabe decir qué pasó |
| `HomeRepository` | 08 | Inicio en una petición (decisión del 21 oct 2026) |
| `AuthRepository.verifyResetCode(email, code)` y `resetPassword(email, code, password)` | 11 | Recuperación con código de 6 dígitos (decisión del 21 oct 2026) |
| `DateProvider` con el "hoy" del API | 08 | "Hoy lo decide el API" (regla 9 de `ORBITA_SPEC`); el reloj del teléfono sirve de respaldo |

## Orden de conexión (una línea en `di/AppModule.kt` por plan)

| Plan del API | Repositorio de Android que pasa a `data/remote` |
|---|---|
| 01 | `AuthRepository` (`signIn`, `signUp`, `signOut`, `session`) y la base común |
| 02 | `SettingsRepository` |
| 03 | `CategoriesRepository` |
| 04 | `AccountsRepository` |
| 05, 06 | `EntriesRepository` |
| 07 | `ReportsRepository` |
| 08 | `HomeRepository` (nuevo) |
| 09, 10 | `CreditRepository` |
| 11, 12 | `AuthRepository` (`requestPasswordReset`, `resetPassword`, `deleteAccount`) |

Mientras unos repositorios sean reales y otros demo, los datos no cuadran entre pantallas: sirve para desarrollar, **no se publica** una versión así (`docs/04`).

## Datos de ejemplo en el API

Para probar la app contra el API con las mismas cifras del modo demo, y para las pruebas de integración de los planes 05 a 10, el API tiene un cargador de los datos de `SampleData.kt` (5 cuentas, 9 categorías, 21 entradas de septiembre y octubre de 2026, 2 tarjetas, 4 compras, 1 US$ = S/ 3.20, "hoy" = 2 oct 2026). Escribe directo en la base, no por los endpoints, porque necesita un saldo inicial negativo y fechas de compra pasadas. Solo en desarrollo y en pruebas (`api seed-dev`, que se niega a correr con `APP_ENV=production`).

## Rama y documentos de Android

- **Rama:** todo el trabajo de Android va en una rama `develop` de `atm-orbita-android`, creada desde `main` (decisión del 21 oct 2026). Pasar a `main` lo decide el usuario.
- **Documentos:** el usuario borró `atm-orbita-android/docs/` a propósito, para **generarlos de nuevo** con esta forma de trabajo (decisión del 21 oct 2026). Los 8 anteriores describían NestJS + Prisma y remitían a planes que ya no existen. Se regeneran en `develop` junto con el plan 01: `docs/CLAUDE.md` (reglas y estado, con el backend en Go y estos planes como contrato) y `docs/ORBITA_SPEC.md` (especificación funcional, a partir del contenido vigente del anterior y del código). A partir de ahí, cada plan actualiza `ORBITA_SPEC.md` en el mismo cambio (guía §0.8). La versión anterior sigue en el historial de git de Android.

## Pruebas (Android)

- Repositorios remotos con un servidor HTTP simulado (Ktor MockEngine): mapeo de DTO, traducción de errores, renovación única con 5 peticiones simultáneas y el token vencido.
- Los flujos de UI de `docs/06` contra el API local con los datos de ejemplo.

## Desviaciones

(Se completan al implementar cada plan.)
