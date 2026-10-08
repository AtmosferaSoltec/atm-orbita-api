# 02 · Modelo de datos (PostgreSQL + Prisma)

Parte del esquema que Android tenía pensado para Supabase y lo adapta a una base propia.

**Las migraciones de Prisma de este repositorio (`prisma/schema.prisma` y `prisma/migrations/`) son la única fuente del esquema.** No existe ningún otro SQL de esquema: la migración `supabase/migrations/0001_init.sql` del repositorio de Android se eliminó el 7 oct 2026, y su `docs/03` ya no contiene SQL. Si este documento y `prisma/` no coinciden, manda `prisma/` y se corrige el documento.

## 1. Qué cambia respecto al esquema de Supabase

| Antes (Supabase) | Ahora (API propio) | Motivo |
|---|---|---|
| `auth.users` (interna de Supabase) | Tabla `users` propia | Autenticación propia |
| — | Tablas `sessions` y `refresh_tokens` | Sesiones renovables y revocables |
| `profiles` sin moneda secundaria | `profiles.secondary_currency` y `profiles.timezone` | Pendiente de Android (H3) y zona horaria (H2) |
| Sin tarjetas de crédito | Tabla `credit_cards` | Pendiente de Android (H3) |
| `credit_purchases` sin tarjeta | `credit_purchases.card_id` | Cada compra pertenece a una tarjeta |
| Políticas RLS | Filtro por `user_id` en el API + claves foráneas compuestas | No hay PostgREST; ver [11](11-seguridad.md) |
| Trigger `handle_new_user` | Servicio de alta en una transacción | Lógica en el API, probada |
| Trigger `validate_transaction` | Clave foránea compuesta `(category_id, user_id, kind)` | Declarativo, sin trigger |
| Trigger `validate_transfer` | Validación en el servicio | La moneda de una cuenta es inmutable, así que no hay carrera |
| Trigger `set_updated_at` | `@updatedAt` de Prisma | |
| Vista `account_balances` | Consulta SQL parametrizada en el repositorio | Evita depender de vistas en Prisma |
| Funciones `report_*` | Consultas SQL parametrizadas | |
| Funciones `pay_credit_purchase` / `unpay_…` | Transacción en el servicio de crédito | |
| `default current_date` en `occurred_on`, `purchase_date` y `rate_date` | **Sin valor por defecto.** La fecha la calcula el API con la zona horaria del usuario y la guarda en una columna `DATE` | H2: en un servidor en UTC, `current_date` ya es "mañana" desde las 19:00 de Lima |
| Nombre de categoría único por `lower(name)` | Columna `name_normalized` (minúsculas, sin tildes, calculada en el backend) con `@@unique([userId, kind, nameNormalized])` | Decisión del usuario: no distinguir mayúsculas ni tildes; [05](05-fase-2-cuentas-categorias-ajustes.md) §5 |

Se conserva sin cambios: tipos `NUMERIC(14,2)` y `NUMERIC(18,6)`, monedas de 3 letras, borrado lógico, `created_at` / `updated_at` / `deleted_at`, y la idea central de las **claves foráneas compuestas `(id, user_id)`**.

## 2. Tablas

### Identidad y sesión

| Tabla | Columnas principales |
|---|---|
| `users` | `id`, `email` (único, guardado en minúsculas y sin espacios), `password_hash`, `email_verified_at`, `last_login_at`, `created_at`, `updated_at` |
| `sessions` | `id`, `user_id`, `created_at`, `last_used_at`, `expires_at` (tope absoluto), `revoked_at`, `revoked_reason`, `user_agent`, `ip` |
| `refresh_tokens` | `id`, `session_id`, `token_hash` (único, SHA-256), `created_at`, `expires_at`, `rotated_at` |
| `email_tokens` (Fase 1b) | `id`, `user_id`, `purpose` (`verify_email`, `reset_password`), `token_hash`, `expires_at`, `used_at` |

Nunca se guarda un token en claro: solo su hash.

### Datos del usuario

| Tabla | Columnas principales | Reglas en la base |
|---|---|---|
| `profiles` | `user_id` (PK), `main_currency`, `secondary_currency`, `display_currency`, `fx_mode`, `timezone` | `main <> secondary`; `display` es una de las dos |
| `exchange_rates` | `id`, `user_id` (nulo = global), `from_currency`, `to_currency`, `rate`, `source`, `rate_date`, `created_at` | `rate > 0`; `from <> to`; global ⇒ `source = 'api'` |
| `accounts` | `id`, `user_id`, `name`, `type`, `currency`, `initial_balance`, `include_in_savings`, `is_archived`, `sort_order` | nombre 1–60; `unique (id, user_id)` |
| `categories` | `id`, `user_id`, `name`, `name_normalized`, `kind`, `icon`, `color`, `is_archived` | nombre 1–60; color `#RRGGBB`; `unique (id, user_id)` y `unique (id, user_id, kind)`; **`unique (user_id, kind, name_normalized)`** |
| `transactions` | `id`, `user_id`, `account_id`, `category_id`, `kind`, `amount`, `occurred_on`, `description` | `amount > 0`; descripción ≤ 500 |
| `transfers` | `id`, `user_id`, `from_account_id`, `to_account_id`, `from_amount`, `to_amount`, `exchange_rate`, `occurred_on`, `note` | montos `> 0`; cuentas distintas; cambio nulo o `> 0` |
| `credit_cards` | `id`, `user_id`, `name`, `currency`, `is_archived` | nombre 1–60; `unique (id, user_id)` |
| `credit_purchases` | `id`, `user_id`, `card_id`, `category_id`, `description`, `amount`, `currency`, `purchase_date`, `due_date`, `status`, `paid_on`, `paid_account_id`, `paid_amount`, `transaction_id` | `amount > 0`; `due_date >= purchase_date`; campos de pago nulos salvo `status = 'paid'` |

Todas las tablas de datos tienen además `created_at`, `updated_at` y `deleted_at`.

Notas de diseño:

- `credit_purchases.currency` es una **copia** de la moneda de la tarjeta en el momento de la compra. Android permite cambiar la moneda de una tarjeta; las compras anteriores conservan la suya.
- `transactions.kind` se guarda (no se deriva) para que el saldo se calcule sin unir con `categories`. La clave foránea compuesta con `kind` impide que se desalinee.
- **`accounts.initial_balance` no tiene restricción de signo en la base.** Que sea `>= 0` es una regla de entrada: la valida el API al crear una cuenta, porque es lo que ofrece el formulario. No va como `CHECK` porque los datos de ejemplo de Android necesitan un saldo inicial negativo: `SampleData.kt` fija el saldo final de cada cuenta y deduce el inicial, y a "Débito principal" le sale **−4,065.80** (1,245.80 menos los 5,311.60 que suman sus movimientos). Con el `CHECK`, la semilla que reproduce esos datos no se podría insertar.
- `accounts.currency` y `accounts.initial_balance` **no se actualizan** después de crear la cuenta. Es lo que hace Android y lo que mantiene coherentes las transferencias.
- `is_archived` (ocultar de las listas) y `deleted_at` (borrado lógico) son cosas distintas. La app solo archiva cuentas, categorías y tarjetas; `deleted_at` queda para una futura sincronización.
- **Fechas.** `occurred_on`, `purchase_date`, `due_date`, `paid_on` y `rate_date` son columnas `DATE` (`@db.Date`), sin hora, sin zona y **sin valor por defecto**. Llegan ya calculadas desde el servicio.
- **Tipo de cambio ausente.** Un usuario puede no tener tipo de cambio. Eso se representa como **ausencia de fila** en `exchange_rates` para su par de monedas, no como una columna que admita nulo: `exchange_rates.rate` sigue siendo obligatoria y `> 0`, porque una fila sin valor no significa nada. Lo que sí es opcional es el campo de salida (`fx.rate: string | null` en el API, `BigDecimal?` en Android). **Confirmado por el usuario:** no hay columna `Decimal?` en `profiles`; duplicaría el dato del historial.
- **Nombre de categoría.** `name` guarda el nombre tal como lo escribió el usuario y es lo único que se muestra. `name_normalized` (minúsculas, sin tildes) la calcula el backend y existe solo para la restricción única `(user_id, kind, name_normalized)`. Admite nulo: se anula al archivar, y así una categoría archivada deja libre su nombre (confirmado por el usuario). Al tener la columna, la restricción se declara con `@@unique` en Prisma y no hace falta un índice escrito a mano.
- **Eliminación de cuenta.** Toda tabla con `user_id` lo referencia con `onDelete: Cascade`. `DELETE /me` borra al usuario dentro de una transacción y la cascada se lleva el resto ([04](04-fase-1-autenticacion.md) §6).

## 3. Integridad entre usuarios

Es la defensa que no depende de que el código esté bien escrito.

```
transactions (account_id, user_id)        → accounts   (id, user_id)
transactions (category_id, user_id, kind) → categories (id, user_id, kind)
transfers    (from_account_id, user_id)   → accounts   (id, user_id)
transfers    (to_account_id, user_id)     → accounts   (id, user_id)
credit_purchases (card_id, user_id)       → credit_cards (id, user_id)
credit_purchases (category_id, user_id)   → categories (id, user_id)
credit_purchases (paid_account_id, user_id) → accounts (id, user_id)
credit_purchases (transaction_id)         → transactions (id)  ON DELETE SET NULL
```

Con esto, aunque un servicio olvidara comprobar a quién pertenece una cuenta, la base rechazaría un movimiento que apunte a la cuenta de otra persona.

Borrado en cascada: `users → todo` con `ON DELETE CASCADE` (para `DELETE /me`). Las referencias entre tablas de datos usan `NO ACTION`, que impide borrar físicamente una cuenta con movimientos pero no estorba la cascada desde `users`. Hay una prueba que borra un usuario con datos y comprueba que no queda nada.

## 4. Esquema Prisma (extracto)

Extracto ilustrativo de los modelos con más reglas. El archivo completo se escribe en la Fase 0 y la sintaxis debe verificarse contra la versión de Prisma instalada.

```prisma
generator client {
  provider = "prisma-client"
  output   = "../src/generated/prisma"
}

datasource db {
  provider = "postgresql"
}

enum EntryKind      { income expense }
enum AccountType    { cash debit savings other }
enum FxMode         { manual auto }
enum FxSource       { manual api }
enum PurchaseStatus { pending paid }

model Account {
  id               String      @id @default(dbgenerated("gen_random_uuid()")) @db.Uuid
  userId           String      @map("user_id") @db.Uuid
  name             String      @db.VarChar(60)
  type             AccountType @default(debit)
  currency         String      @db.Char(3)
  initialBalance   Decimal     @default(0) @map("initial_balance") @db.Decimal(14, 2)
  includeInSavings Boolean     @default(true) @map("include_in_savings")
  isArchived       Boolean     @default(false) @map("is_archived")
  sortOrder        Int         @default(0) @map("sort_order")
  createdAt        DateTime    @default(now()) @map("created_at") @db.Timestamptz(6)
  updatedAt        DateTime    @updatedAt @map("updated_at") @db.Timestamptz(6)
  deletedAt        DateTime?   @map("deleted_at") @db.Timestamptz(6)

  user User @relation(fields: [userId], references: [id], onDelete: Cascade)

  @@unique([id, userId])
  @@index([userId])
  @@map("accounts")
}

model Transaction {
  id          String    @id @default(dbgenerated("gen_random_uuid()")) @db.Uuid
  userId      String    @map("user_id") @db.Uuid
  accountId   String    @map("account_id") @db.Uuid
  categoryId  String    @map("category_id") @db.Uuid
  kind        EntryKind
  amount      Decimal   @db.Decimal(14, 2)
  occurredOn  DateTime  @map("occurred_on") @db.Date
  description String    @default("") @db.VarChar(500)
  createdAt   DateTime  @default(now()) @map("created_at") @db.Timestamptz(6)
  updatedAt   DateTime  @updatedAt @map("updated_at") @db.Timestamptz(6)
  deletedAt   DateTime? @map("deleted_at") @db.Timestamptz(6)

  user     User     @relation(fields: [userId], references: [id], onDelete: Cascade)
  account  Account  @relation(fields: [accountId, userId], references: [id, userId], onDelete: NoAction)
  category Category @relation(fields: [categoryId, userId, kind], references: [id, userId, kind], onDelete: NoAction)

  @@map("transactions")
}
```

Con Prisma 7 la URL de conexión se declara en `prisma.config.ts`, no en el `schema.prisma`, y el cliente se instancia con el adaptador `@prisma/adapter-pg`.

## 5. Lo que Prisma no expresa y va como SQL en la migración

Prisma no modela restricciones `CHECK` ni índices con expresión, y el soporte de índices parciales depende de la versión. Todo eso se escribe a mano dentro del archivo de migración generado (`prisma migrate dev --create-only`, editar, aplicar). Cada bloque lleva un comentario que explica la regla.

**Restricciones `CHECK`**

```sql
alter table transactions     add constraint transactions_amount_positive check (amount > 0);
alter table transfers        add constraint transfers_amounts_positive   check (from_amount > 0 and to_amount > 0);
alter table transfers        add constraint transfers_distinct_accounts  check (from_account_id <> to_account_id);
alter table transfers        add constraint transfers_rate_positive      check (exchange_rate is null or exchange_rate > 0);
alter table accounts         add constraint accounts_name_not_blank      check (length(btrim(name)) between 1 and 60);
alter table accounts         add constraint accounts_currency_format     check (currency ~ '^[A-Z]{3}$');
alter table categories       add constraint categories_color_format      check (color is null or color ~ '^#[0-9A-Fa-f]{6}$');
alter table exchange_rates   add constraint exchange_rates_rate_positive check (rate > 0);
alter table exchange_rates   add constraint exchange_rates_distinct      check (from_currency <> to_currency);
alter table profiles         add constraint profiles_distinct_currencies check (main_currency <> secondary_currency);
alter table profiles         add constraint profiles_display_in_pair     check (display_currency in (main_currency, secondary_currency));
alter table credit_purchases add constraint purchases_due_after_purchase check (due_date >= purchase_date);
alter table credit_purchases add constraint purchases_paid_fields check (
  (status = 'paid' and paid_on is not null and paid_account_id is not null and paid_amount > 0)
  or (status = 'pending' and paid_on is null and paid_account_id is null
      and paid_amount is null and transaction_id is null));
```

**Índices**

```sql
-- Listas de movimientos y paginación por cursor
create index transactions_feed_idx on transactions (user_id, occurred_on desc, created_at desc, id desc)
  where deleted_at is null;
create index transfers_feed_idx on transfers (user_id, occurred_on desc, created_at desc, id desc)
  where deleted_at is null;

-- Saldo por cuenta sin leer la tabla (index-only scan)
create index transactions_balance_idx on transactions (account_id) include (kind, amount)
  where deleted_at is null;
create index transfers_from_idx on transfers (from_account_id) include (from_amount) where deleted_at is null;
create index transfers_to_idx   on transfers (to_account_id)   include (to_amount)   where deleted_at is null;

-- Reportes por categoría
create index transactions_category_idx on transactions (user_id, category_id, occurred_on)
  where deleted_at is null;

-- El nombre de categoría único NO va aquí: se declara en schema.prisma con
-- @@unique([userId, kind, nameNormalized]) sobre la columna name_normalized.

-- Tipo de cambio vigente
create index exchange_rates_lookup_idx on exchange_rates
  (user_id, from_currency, to_currency, rate_date desc, created_at desc);
-- La tarea diaria no duplica filas globales
create unique index exchange_rates_global_uidx on exchange_rates (from_currency, to_currency, rate_date)
  where user_id is null;

-- Crédito: pendientes por vencimiento
create index credit_purchases_pending_idx on credit_purchases (user_id, due_date)
  where status = 'pending' and deleted_at is null;

-- Sesiones
create index refresh_tokens_session_idx on refresh_tokens (session_id);
create index sessions_user_active_idx on sessions (user_id) where revoked_at is null;
```

## 6. Consultas clave

Se escriben como SQL parametrizado con las plantillas etiquetadas de Prisma (`$queryRaw`), que envían los valores como parámetros. **Nunca** se concatena texto ni se usa `$queryRawUnsafe`.

**Saldos de todas las cuentas de un usuario** (una sola consulta, sin subconsultas por fila):

```sql
select a.id,
       a.initial_balance
         + coalesce(t.net, 0) + coalesce(ti.total, 0) - coalesce(tf.total, 0) as balance
from accounts a
left join (select account_id,
                  sum(case kind when 'income' then amount else -amount end) as net
             from transactions
            where user_id = $1 and deleted_at is null
            group by account_id) t  on t.account_id = a.id
left join (select to_account_id as account_id, sum(to_amount) as total
             from transfers where user_id = $1 and deleted_at is null
            group by to_account_id) ti on ti.account_id = a.id
left join (select from_account_id as account_id, sum(from_amount) as total
             from transfers where user_id = $1 and deleted_at is null
            group by from_account_id) tf on tf.account_id = a.id
where a.user_id = $1 and a.deleted_at is null;
```

**Reporte por categoría y moneda** (equivale a `report_by_category` y `report_period_summary`):

```sql
select a.currency, t.kind, c.id, c.name, c.color, sum(t.amount) as total, count(*) as movements
from transactions t
join accounts a   on a.id = t.account_id  and a.user_id = t.user_id
join categories c on c.id = t.category_id and c.user_id = t.user_id
where t.user_id = $1 and t.deleted_at is null and a.deleted_at is null
  and t.occurred_on between $2::date and $3::date
group by a.currency, t.kind, c.id, c.name, c.color;
```

**Lista mezclada de movimientos y transferencias:** `UNION ALL` de ambas tablas con las mismas tres columnas de orden, filtrado por cursor, y `LIMIT`. Detalle en [06](06-fase-3-movimientos-transferencias.md).

## 7. Migraciones

| # | Migración | Fase | Contenido |
|---|---|---|---|
| 1 | `init_identity` | 0–1 | `users`, `sessions`, `refresh_tokens` |
| 2 | `init_finance` | 1 | `profiles`, `exchange_rates`, `accounts`, `categories`. Va en la Fase 1 porque el alta de usuario ya las llena; sus módulos llegan en la Fase 2 |
| 3 | `entries` | 3 | `transactions`, `transfers` |
| 4 | `credit` | 5 | `credit_cards`, `credit_purchases` |
| 5 | `email_tokens` | 1b | `email_tokens` |
| 6 | `fx_runs` | 6 | Registro de ejecuciones de la tarea diaria |

Reglas:

- Una migración aplicada **no se edita**. Se corrige con otra.
- En desarrollo: `prisma migrate dev`. En despliegue: `prisma migrate deploy`, ejecutado como paso aparte **antes** de arrancar la nueva versión del API, con el rol `orbita_migrator`.
- Cambios compatibles hacia atrás en dos pasos (expandir, luego contraer): primero se agrega la columna nueva y el código la escribe; cuando ninguna versión desplegada usa la antigua, otra migración la elimina. Así se despliega sin corte.
- El pipeline falla si `schema.prisma` y las migraciones no coinciden.
- Toda migración se prueba contra una copia con los datos de ejemplo antes de llegar a producción.

## 8. Datos iniciales y de ejemplo

**Alta de usuario** (lo que hacía `handle_new_user`), en una sola transacción:

- Perfil: `main_currency` = la moneda elegida al registrarse (por defecto `PEN`); `secondary_currency = USD`, o `PEN` si la principal es `USD`; `display_currency` = la principal; `fx_mode = manual`; `timezone` enviada por el cliente o `America/Lima`.
- Categorías de egreso: Alimentación `#0E7490`, Transporte `#B45309`, Vivienda `#1D4ED8`, Salud `#7C3AED`, Ocio `#BE185D`, Otros `#64748B`.
- Categorías de ingreso: Sueldo `#0B7A5A`, Freelance `#0E7490`, Otros ingresos `#64748B`.
- Cuenta "Efectivo", tipo `cash`, en la moneda principal, saldo inicial 0.
- Cada categoría inicial lleva su `name_normalized`.

**Semilla de ejemplo** (`prisma/seed.ts` y `test/fixtures/sample-data.ts`): reproduce exactamente `SampleData.kt` de Android. Escribe con Prisma directamente, no por los endpoints, porque necesita cosas que el API no deja hacer a un usuario: un saldo inicial negativo y fechas de compra pasadas. Cinco cuentas, 21 entradas de septiembre y octubre de 2026, dos tarjetas, cuatro compras pendientes y el cambio 1 US$ = S/ 3.20. Se usa en desarrollo y como base de las pruebas de reglas ([12](12-pruebas.md)). La semilla **no se ejecuta en producción**.

## 9. Volumen esperado

Una persona registra del orden de 50 a 150 movimientos al mes. Con 10,000 usuarios activos durante 5 años son unos 60 millones de filas en `transactions`, que PostgreSQL maneja sin particionar gracias a que toda consulta entra por `user_id`. El plan de crecimiento está en [13](13-escalabilidad-y-operacion.md).
