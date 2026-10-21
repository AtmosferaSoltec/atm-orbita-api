# 14 · Tipo de cambio automático (fase tardía)

## Objetivo

Que el usuario pueda elegir "Automático" en Ajustes y su tipo de cambio se actualice solo, una vez al día, desde un proveedor externo.

> **Bloqueado: falta elegir el proveedor** (guía §11: "pregunta al usuario al llegar ahí"). Este plan se completa cuando se decida. No se implementa antes de los planes 00 a 12.

## Pantallas de Android que cubre

- **Ajustes → Tipo de cambio: Manual | Automático** (hoy "Automático · pronto", desactivado; `ORBITA_SPEC` §7.12).

## Módulo

`profile`, con el proveedor detrás de un puerto `RateProvider` en `internal/integrations/fx`.

## Endpoints (borrador)

| Método y ruta | Request | Respuesta |
|---|---|---|
| `PATCH /settings` | `{ "fx_mode": "auto" \| "manual" }` | `200 { data: settings }` |

## Tablas y migraciones

Usa `exchange_rates` con `user_id` nulo (global) y `source = 'api'` (ya previsto en el plan 02). Una migración nueva agrega el índice único `(from_currency, to_currency, (created_at::date)) WHERE user_id IS NULL`, para que la tarea diaria no duplique filas, y una tabla `fx_runs` con el resultado de cada ejecución.

## Reglas (borrador)

- Una tarea diaria (con candado en Postgres, para que corra una sola vez aunque haya varias réplicas) trae los pares que usan los usuarios en modo automático y guarda una fila global por par.
- En modo automático, el tipo de cambio vigente es la última fila global del par; en manual, la del usuario.
- Si el proveedor falla o devuelve un valor absurdo (fuera de un margen respecto al anterior), no se guarda y se registra; se sigue usando el último bueno.
- La llamada externa nunca bloquea una petición del usuario.

## Preguntas antes de empezar

1. ¿Qué proveedor? (gratuito con límite diario, de pago, o el BCRP para PEN/USD).
2. ¿A qué hora local corre?
3. ¿Qué pasa con un par que el proveedor no cubre?

## Variables nuevas para el servidor

Las del proveedor (por ejemplo `FX_PROVIDER_API_KEY`); se definen al elegirlo.
