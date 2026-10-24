# Services

Monthly subscriptions and manually assigned member shares for the web. Public construction and routes are in `module.go`. API contract and decisions: [plan 20](../../../docs/plans/20-services.md).

- `domain`: service validation, member shares, due dates and payment eligibility.
- `app`: transactional CRUD and batch reimbursement recording, depending on repository, profile date, clock and transaction ports.
- `adapters/http`: authenticated JSON endpoints and stable error codes.
- `adapters/postgres`: owner-scoped sqlc queries; composite foreign keys and service-row locks.

Every money value is an exact decimal stored as NUMERIC(14,2) and serialized as a string. Date-only values and month keys do not depend on the server timezone. Payments use the stored member amount; currency cannot change while payments exist. User deletion cascades without additional external-resource hooks.

Run `go test ./internal/modules/services/...` for domain tests. Set `TEST_DATABASE_URL` to the authorized test target to include HTTP/Postgres integration and isolated migration round-trip tests. The tests register unique accounts and delete them on cleanup.
