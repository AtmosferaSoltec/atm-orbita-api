# 20 · Real monthly services and shared payments

Implemented on 24 October 2026, on the user's request to connect the existing web mockup to the API and the server database. Scope: web and API only. The user explicitly approved locking the currency once payments exist.

## Behavior and UI

- Create, list, open, edit and delete a service. It has a name, color, PEN/USD currency, monthly total, billing day, start date, status, optional last charge date, sharing flag and optional note. Categories have been removed entirely.
- The currency control has a sliding selection indicator, native radio keyboard behavior and reduced-motion support. Its currency is disabled when recorded member payments exist; the API enforces the same rule.
- Billing day uses an accessible popover with a grid from 1 to 31. Short months use their last day.
- Start, last-charge and reimbursement dates use a styled Bits UI calendar with Spanish localization, month/year navigation and keyboard/Escape support. The popover stays in its component tree so it also works inside the native payment dialog.
- Sharing retains manually entered names, monthly amounts and start months. There is no automatic equal split. Other people's shares cannot exceed the monthly service total.
- A service supports 1–100 members when shared. Member names have 1–60 characters; member start months cannot precede the service start month. Notes allow 500 characters. Dates are bounded to 2000–2100.
- Mark one or multiple due months as paid, choose the date received, and undo an individual payment. The API calculates amounts from the stored member share; a client cannot supply a different amount. Existing payment amounts remain historical snapshots if the current monthly share changes.
- A payment is allowed only for a known member and a due month within the member/service dates and cancellation cutoff, while the service is not paused. Its receipt date must lie between service start and the profile-local current date. No future month can be marked paid. Repeated requests do not duplicate or overwrite an existing payment.
- The original mockup's status behavior is preserved: paused services have no upcoming charge and suppress unpaid-month reminders while paused; there is no separate pause-period history. Cancelled services stop at their last-charge date. Owner spending is the computed recurring charge history, while recorded payments represent members' reimbursements.
- Removing a member or turning off sharing removes that member's payment records, matching the prototype. The form warns before saving those changes.
- Lists, details, forms, member payments and dashboard summaries read the API. Services are no longer loaded from or written to localStorage. Previous demo records are not imported as real user data. Only loans retain demo persistence and reset behavior.
- Session loss/logout clears in-memory services, and responses from a previous session cannot repopulate the store. Save failures keep form values. Failed loads show an explicit retry state, rather than a misleading empty list. Mutations update the store only after API success.

## Contract

Every endpoint is protected by Orbita session authentication, under `/api/v1`. See `docs/openapi.yaml` for schemas. Amounts travel as strings and are stored as exact decimals. User ownership comes from the token.

| Method and route | Purpose |
| --- | --- |
| `GET /services` | All services, members and payments for the current user, plus `meta.today` from the profile timezone |
| `POST /services` | Create using the client's stable UUID; an already-owned ID returns the existing record |
| `GET /services/:id` | Open a user's service |
| `PATCH /services/:id` | Replace editable fields and member list atomically |
| `DELETE /services/:id` | Delete the service and cascade its members/payments |
| `PUT /services/:id/payments` | Atomically mark a batch of `{member_id, month, paid_on}` items paid |
| `DELETE /services/:id/members/:member_id/payments/:month` | Undo one reimbursement |

Success returns `{data: service}`; list returns `{data: service[], meta: {today}}`; delete returns 204. The collection is intentionally unpaginated because the dashboard needs the whole user's subscription set, as with the existing receipts collection. An ID owned by another account is hidden as 404 for access and 409 `ID_CONFLICT` for creation. Currency changes with payments return 409 `SERVICE_CURRENCY_LOCKED`. Input errors use `VALIDATION_FAILED` with stable field codes.

## Architecture and storage

New independent module `internal/modules/services`: plain domain validation/calendar rules, application use cases and repository/profile/transaction ports, Gin and sqlc/pgx adapters, and a public module constructor wired in `cmd/api/main.go`. It depends only on the public `profile.Reader` for the local date. It does not write ledger transactions or require FX configuration: PEN and USD are tracked separately.

Migration `00014_services_create_subscriptions.sql` adds:

- `services`: UUID, owner, subscription fields, timestamps and schema constraints.
- `service_members`: owner/service, name, amount, first month and display order.
- `service_member_payments`: owner/service/member, month, received date, exact snapshot amount and creation time; unique `(member_id, month)`.

Composite foreign keys keep service, member and owner aligned. User deletion cascades through all three tables. Every read/write is scoped by authenticated owner. Service-row locks serialize edits, deletions and payment changes. Transactions cover the entire service/member update and the entire payment batch. A foreign member ID cannot be moved between services, even within the same user's account.

## Validation and deployment

The owner authorized development and integration tests against the PostgreSQL database on the VPS through the existing localhost:15432 SSH tunnel. Tests create unique `services-…@e2e.orbita.test` and `services-ui-…@e2e.orbita.test` accounts and remove only those accounts afterwards. No personal test account credentials are required.

Migration Up/Down was rehearsed in an isolated schema inside a transaction that is rolled back on the same server. The forward migration was then applied to the normal Orbita schema through the integration harness. No existing migration was edited, and no existing user records were changed by the migration.

Verification covers domain limits, short/leap months, cancellation cutoff, ownership isolation, cross-service member forgery, atomic rollback, duplicate/concurrent payments, historical amounts, currency locking and account-deletion cascade. Browser tests run against the real local API and server database: create/reload/edit/delete, manual shares, record/undo payments, mobile day/calendar pickers, failed saves and load/retry recovery. Existing Google authentication browser tests continue to run with a mocked provider.

Completed checks: full Go test suite (without a database target for unrelated integration packages), vet, golangci-lint and build; targeted services integration and account-deletion cascade tests against the authorized server database with `-race`; 49 frontend unit tests; six Playwright flows (three real services flows and three Google regression flows); formatting, ESLint and Svelte/TypeScript checks; production web build; OpenAPI parsing and reference resolution. An additional check on the actual development server at localhost:5173 loaded the service form, day grid and calendar without browser errors and removed its temporary account afterwards. Mobile screenshots were reviewed after the popover animation completed.

Bits UI's helper dependency `runed` is pinned to 0.38.0 in `pnpm-workspace.yaml`, which declares SvelteKit 3 support. Type checks, production builds and browser flows validate compatibility. No peer dependency warnings remain.

No new environment variables or external accounts are required. API and web remain on `develop`; the user handles pushes. Deploy API first, wait for a healthy deployment, then deploy the web using `develop → main → production`. Migration 00014 is already recorded on the shared database, so deployment skips it. Android has not been modified.
