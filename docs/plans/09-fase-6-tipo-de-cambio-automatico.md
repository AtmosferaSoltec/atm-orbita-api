# 09 · Fase 6 — Tipo de cambio automático diario

**Objetivo:** que el usuario pueda elegir "Automático" en Ajustes y la app use un tipo de cambio actualizado cada día, sin escribirlo a mano. Hoy esa opción aparece como "Automático · pronto".

**Tamaño:** mediano. **Depende de:** Fase 2. Es independiente de las Fases 3 a 5 y puede hacerse en paralelo.

El esquema ya estaba preparado: `exchange_rates` admite filas globales (`user_id` nulo, `source = 'api'`) y `profiles.fx_mode` admite `auto`.

## 1. Cómo funciona

```
Tarea diaria ──▶ Proveedor externo ──▶ Validación ──▶ exchange_rates (filas globales)
                                                              │
Usuario en modo auto ──▶ effectiveRate() ─────────────────────┘
```

- Una tarea programada consulta una vez al día a un proveedor externo y guarda una fila global por moneda.
- Las filas globales son de **todos** los usuarios: se consulta al proveedor una vez, no una vez por persona.
- `effectiveRate` (definido en [05](05-fase-2-cuentas-categorias-ajustes.md), sección 2) pasa a leer las filas globales cuando el usuario está en modo automático.
- El modo manual sigue funcionando exactamente igual. El usuario puede volver a él cuando quiera.

## 2. Elección del proveedor (decisión abierta D5)

Se decide al iniciar la fase. El proveedor queda detrás de una interfaz, así que cambiarlo no toca el resto del sistema.

```ts
interface FxProvider {
  readonly name: string;
  /** Unidades de cada moneda por 1 unidad de `base`, para la fecha más reciente disponible. */
  fetchLatest(base: string, symbols: string[]): Promise<{ date: LocalDate; rates: Map<string, Decimal> }>;
}
```

Criterios para elegir, en este orden:

1. **Cobertura de las 9 monedas** del catálogo: PEN, USD, EUR, MXN, COP, CLP, ARS, BOB, BRL. Las fuentes basadas en las tasas de referencia del Banco Central Europeo no publican varias monedas latinoamericanas, así que no bastan por sí solas.
2. **Licencia** que permita mostrar el dato en una app comercial.
3. **Límite de peticiones** del plan gratuito (basta con una al día).
4. **Disponibilidad** y que entregue la fecha del dato.
5. Costo.

Punto a tener presente con el usuario: en países con control de cambios (Argentina, Bolivia), el tipo oficial que publican estas fuentes puede diferir mucho del que la gente realmente consigue. El modo manual debe seguir a la mano, y la interfaz debería indicar de dónde viene el valor.

Para Perú existe además la referencia oficial de la SBS y la SUNAT; se puede evaluar como segunda fuente para el par PEN/USD.

## 3. Almacenamiento

Se guarda **una moneda base (USD) contra todas las demás**: 8 filas por día.

| `user_id` | `from_currency` | `to_currency` | `rate` | `source` | `rate_date` |
|---|---|---|---|---|---|
| nulo | USD | PEN | 3.412000 | api | 2026-10-07 |
| nulo | USD | EUR | 0.921500 | api | 2026-10-07 |
| … | | | | | |

Cualquier par se obtiene cruzando por la base:

```
cambio(A→B) = cambio(USD→B) ÷ cambio(USD→A)
```

La división usa 10 decimales intermedios y el resultado se redondea a 6, *half up*.

Guardar base contra todas (8 filas) en lugar de todos los pares (72 filas) evita inconsistencias entre pares y hace trivial agregar una moneda.

El índice único `(from_currency, to_currency, rate_date) WHERE user_id IS NULL` hace que repetir la tarea el mismo día **actualice** en lugar de duplicar.

Nueva tabla `fx_runs` para saber qué pasó en cada ejecución: `id`, `started_at`, `finished_at`, `provider`, `status` (`ok`, `failed`, `rejected`), `rate_date`, `rows_written`, `error`.

## 4. La tarea diaria

- Programada con `@nestjs/schedule` según `FX_CRON` (por ejemplo, una vez al día tras el cierre de mercados).
- **Una sola réplica la ejecuta.** Antes de empezar toma un candado consultivo de PostgreSQL (`pg_try_advisory_lock`); si otra réplica ya lo tiene, se retira. No hace falta Redis para esto.
- Pasos: tomar candado → pedir al proveedor → validar → guardar en una transacción → registrar en `fx_runs` → soltar candado.
- Si falla: reintenta con espera creciente (3 intentos); si sigue fallando, registra `failed` y **no toca los datos**. Se sigue usando el último valor bueno.
- Se puede lanzar a mano con un comando de consola, para la primera carga y para recuperar un día perdido. No hay endpoint público para dispararla.

## 5. Validación de lo que responde el proveedor

Un proveedor externo es una entrada no confiable. Antes de guardar:

| Comprobación | Si falla |
|---|---|
| La respuesta tiene la forma esperada y trae todas las monedas pedidas | Se rechaza la ejecución completa |
| Cada tasa es un número positivo y finito | Se rechaza |
| La fecha del dato no es futura ni tiene más de 7 días | Se rechaza |
| Cada tasa varía menos de un 20 % respecto al último valor guardado | Se rechaza y se alerta |
| La petición responde en menos de 10 s | Se cancela y se reintenta |

Una ejecución rechazada se registra como `rejected` con el motivo. Es preferible un cambio de ayer a uno absurdo: un valor disparatado alteraría el ahorro total de todos los usuarios en modo automático.

Otras protecciones:

- La URL del proveedor es fija en la configuración; ningún dato del usuario forma parte de ella.
- La clave del proveedor va en variables de entorno y nunca se registra.
- Las tasas se convierten de texto a `Decimal` directamente, sin pasar por `number`.

## 6. Cambios en el API

| Elemento | Cambio |
|---|---|
| `PATCH /settings` | `fxMode: "auto"` pasa a aceptarse |
| `GET /settings` | En modo automático, `fx.source` es `"api"` y `fx.rateDate` es la fecha del dato |
| `effectiveRate` | Modo automático: fila global más reciente, directa o cruzada. Si no hay dato global, cae al último manual del usuario; si tampoco, `null` |
| `GET /exchange-rates/latest` | Agrega `source`, `rateDate` y `stale` |
| `GET /accounts`, `GET /home` | En modo automático, **todas** las cuentas se pueden convertir, también las que están en una tercera moneda |
| Alta de usuario | Siembra el cambio manual del par del usuario con el valor global del día, si existe (resuelve H4) |

`stale: true` cuando el dato tiene más de 3 días. El cliente puede avisar "tipo de cambio del 4 oct".

**Efecto sobre una limitación actual de Android.** Hoy solo hay cambio entre la moneda principal y la secundaria, y una cuenta en una tercera moneda no entra en el ahorro total. En modo automático el API sí puede convertirla. Android necesitará leer el total que devuelve el servidor en lugar de calcularlo solo con `FxPair` ([14](14-integracion-android.md)).

**Lo que no cambia:** las transferencias ya guardadas conservan el cambio con que se hicieron. El automático solo afecta al cambio *sugerido* en una transferencia nueva y a las conversiones de visualización.

## 7. Tareas

- [ ] Elegir proveedor según los criterios de la sección 2 y registrar la decisión
- [ ] Interfaz `FxProvider` y su implementación; una implementación falsa para pruebas
- [ ] Migración `fx_runs`
- [ ] `FxSyncService`: candado, petición, validación, guardado, registro
- [ ] Programación con `@nestjs/schedule` y comando manual
- [ ] Ampliar `effectiveRate` con el modo automático y el cruce por la base
- [ ] Aceptar `fxMode: auto` y exponer `source`, `rateDate`, `stale`
- [ ] Ampliar el ahorro total a terceras monedas en modo automático
- [ ] Sembrar el cambio del usuario nuevo
- [ ] Alerta si pasan más de 48 h sin una ejecución correcta
- [ ] Carga inicial del histórico reciente

## 8. Pruebas

Unitarias:

- Cruce por la base: con USD→PEN 3.20 y USD→EUR 0.80, EUR→PEN da 4.000000 y PEN→EUR da 0.250000.
- Una respuesta con una moneda faltante, una tasa negativa, una tasa en cero, una fecha futura o un salto del 50 % se rechaza.
- Una tasa con muchos decimales se guarda sin pérdida de precisión.

Integración (con el proveedor falso):

- Una ejecución correcta escribe 8 filas y un registro `ok`.
- Ejecutar dos veces el mismo día deja 8 filas, no 16.
- Dos ejecuciones simultáneas: solo una escribe.
- Si el proveedor falla tres veces, no se modifica nada y queda un registro `failed`.
- Un tiempo de espera agotado se trata como fallo, no cuelga el proceso.

e2e:

- Un usuario en modo manual no ve ningún cambio de comportamiento.
- Al pasar a automático, `GET /settings` devuelve el valor global y `source: "api"`.
- En modo automático, una cuenta en EUR con par PEN/USD entra en el ahorro total.
- Sin ningún dato global, el modo automático cae al último valor manual.
- Un usuario no puede escribir filas globales por ninguna ruta.

Contrato:

- Una prueba aparte, fuera del pipeline normal y programada, llama al proveedor real y verifica que su respuesta sigue teniendo la forma esperada.

## 9. Criterios de aceptación

- La tarea corre sola cada día y deja constancia en `fx_runs`.
- Con dos réplicas del API, la tarea se ejecuta una sola vez.
- Una caída del proveedor no afecta a ningún endpoint: se sigue respondiendo con el último valor.
- El modo manual no cambia en nada.

## 10. Riesgos

| Riesgo | Mitigación |
|---|---|
| El proveedor cambia su formato, sube precios o desaparece | Interfaz `FxProvider`; prueba de contrato programada; alerta de 48 h |
| Un valor erróneo distorsiona el ahorro total de todos | Validación de variación máxima y de forma; ejecución rechazada antes que dato malo |
| El tipo oficial no refleja el real en algunos países | Modo manual siempre disponible; mostrar la fuente |
| Depender de una sola moneda base | Si falta la base, no hay ningún par: la validación exige la respuesta completa |
