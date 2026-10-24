# 19 · Google sign-in and long web sessions

Implemented on 24 October 2026. The owner's OAuth web client is configured locally; full sign-in with the owner's account and production deployment are pending. Scope: API and web only; Android files and native authentication contracts are unchanged.

## Behavior

- Login and registration offer the official Google Identity Services button when configured. The web fetches the public client ID from the API, so no frontend environment variables or image build arguments are needed.
- A new Google identity creates an Orbita account without a password. The same registration hooks create the profile, categories and cash account in one transaction. Currency defaults to PEN; registration respects the currency selected in the web form. The browser supplies its timezone, with America/Lima as API fallback. Name comes from verified Google claims, normalized and limited to 60 characters. Photos are not stored.
- A returning identity is looked up by Google `sub`, independent of later email changes.
- An existing email/password account confirms its existing password once before linking. No automatic email-only linking. Existing data, user ID, name and password remain intact. Only one Google identity can be linked per user.
- A Google-only account can explicitly establish a password through the existing email-code recovery flow, including session revocation and the password change notification. It can then also use email/password clients. Password login and deletion attempts without an established password return the normal invalid credentials response.
- Google is used at sign-in only. Orbita issues and renews its own access and refresh tokens.

## Endpoints

All paths are under `/api/v1`. See `docs/openapi.yaml` for the complete schemas.

| Endpoint | Request | Result |
| --- | --- | --- |
| `GET /auth/google/config` | None | `{data: {client_id}}`; empty disables the button |
| `POST /auth/google/challenge` | `X-Session-Transport: cookie` | `{data: {nonce}}` and browser-bound HttpOnly challenge cookie |
| `POST /auth/google` | Same header, JSON `{credential, password?, main_currency?, timezone?}`, challenge cookie | Existing `{data: {user, tokens}}` contract; refresh cookie only |

All three responses use `Cache-Control: no-store`. Challenge and exchange use the login IP rate limiter. Password confirmation also uses the existing per-account attempt limiter.

Errors: `GOOGLE_UNAVAILABLE` (503), `GOOGLE_INVALID` (401), `GOOGLE_CHALLENGE_EXPIRED` (401), `GOOGLE_LINK_REQUIRED` (409), `GOOGLE_IDENTITY_CONFLICT` (409), plus existing validation, credentials and throttling errors.

## Security and storage

Migration `00013_identity_google_and_web_sessions.sql`:

- `users.password_hash` becomes nullable. Repository reads map absence to an empty domain hash; password checks still run a dummy hash and reject absent credentials.
- `external_identities` stores provider, subject, user ID and creation time, with unique provider/subject and user/provider constraints. User deletion cascades to external identities.
- `google_login_challenges` stores SHA-256 nonce hashes and five-minute expirations. Issuing a challenge removes expired rows. Consuming it is atomic and is rolled back if linking, account creation or a registration hook fails.
- `sessions.is_web` records the session policy. It defaults to false for compatibility with already-issued sessions and older inserts.

Google's maintained `google.golang.org/api/idtoken` verifier checks signatures with cached public keys and a server-configured audience. The adapter also verifies issuer, expiry boundary, issue time, verified email, subject and browser nonce. Google validation has a five-second timeout. Tokens and passwords are never logged or stored in browser storage.

The browser nonce travels in an HttpOnly, SameSite=Strict cookie restricted to `/api/v1/auth/google`; `Secure` is enabled outside development. A custom request header and JSON content type prevent simple cross-site credential submissions. The successful exchange clears the cookie. Failed linking may retry until expiration. Starting Google on another tab replaces the shared pending challenge; the older attempt must restart.

New web sessions have a 90-day renewable refresh lifetime; native sessions retain their configured 30-day lifetime. Each rotation extends the window. Already-issued sessions retain their previous policy until a new sign-in, since historical records cannot reliably identify browser sessions. Access tokens retain the existing configured 900-second lifetime. Refresh reuse still revokes the entire session. The web uses the Web Locks API to serialize refresh and authentication cookie mutations across tabs; older browsers without Web Locks retain per-tab serialization only.

## Owner setup and deployment

Follow [Google's setup guide](https://developers.google.com/identity/gsi/web/guides/get-google-api-clientid).

1. Open Google Cloud Console using the account that should own Orbita. Create or select a project, then open Google Auth Platform.
2. Configure Branding with Orbita's application name, support email, developer contact, authorized domain `atmosferast.com`, homepage and a real published privacy-policy URL. Terms of service are optional. Complete any domain/brand verification the console requests.
3. Configure Audience for external users. Add intended test accounts during testing; before public rollout select the production publishing status and resolve any verification requirements shown by Google.
4. Use only the basic authentication data (`openid`, `email`, `profile`). No Gmail, Drive or other sensitive API scopes are needed.
5. Under Clients create an OAuth client of type **Web application**. Authorized JavaScript origins:

   ```text
   http://localhost
   http://localhost:5173
   https://orbita.atmosferast.com
   ```

   Origins have no path. Add another exact development origin only if intentionally using another port. This integration uses the JavaScript popup callback, so Authorized redirect URIs can remain empty; `/api/v1/auth/google` is an internal JSON exchange, not a redirect URI.

6. Copy the **Client ID** ending in `.apps.googleusercontent.com`. It is public. No client secret, API key, downloaded service-account JSON or Google refresh token is needed for this flow.
7. Set these API environment variables locally and in `/opt/orbita-api/.env`:

   ```dotenv
   GOOGLE_WEB_CLIENT_ID=<the-web-client-id>.apps.googleusercontent.com
   WEB_REFRESH_TOKEN_TTL_DAYS=90
   ```

   `GOOGLE_WEB_CLIENT_ID` is optional at startup; empty disables Google cleanly. A nonempty malformed ID fails startup. `WEB_REFRESH_TOKEN_TTL_DAYS` is optional, defaults to 90 and accepts 1–365. Keep `ACCESS_TOKEN_TTL_SECONDS=900` and native `REFRESH_TOKEN_TTL_DAYS=30`. No GitHub secret or web runtime variable is needed.

8. Apply migrations to an isolated local database first. Never use the shared VPS database for automated tests. Start the API with `scripts/dev.sh` against the intended development database, and the web with `pnpm dev --host localhost --port 5173 --strictPort`.
9. With the real client ID, manually check new Google signup, return login, existing account linking, popup cancellation, reload, sign-out and simultaneous tabs on localhost. Automated tests replace Google interaction; they cannot establish Google Cloud branding/origin correctness.
10. After environment configuration and real-account validation, promote API first using `develop → main → production`. Its existing workflow applies migration 00013. Then promote the web with the same branch flow. The static image reads provider configuration from the API. Its nginx header `Cross-Origin-Opener-Policy: same-origin-allow-popups` supports popup fallback when FedCM is unavailable.

The source is not a substitute for completing Google setup. Do not announce Google sign-in as live until a real account succeeds on the deployed origin. Rolling back migration 00013 drops external identity mappings; rehearse Down only in an isolated database and back up before any production rollback. An older API cannot authenticate Google-only accounts.

## Validation

- API integration tests use a disposable PostgreSQL 17 instance and a fake external verifier: new/returning accounts, one-time password linking, replay, concurrent exchange, expiration, missing browser proof, disabled provider, transactional rollback, explicit password setup, native/web lifetime separation and account deletion cascade.
- Adapter tests sign local RSA JWTs and test the actual official verifier with stubbed Google public-key transport: valid signature, wrong signature/audience/issuer, expired/future tokens, absent subject, incorrect nonce and unverified email.
- Web Playwright tests replace the Google script and API: login, registration, linking, reload, two-tab refresh serialization, disabled provider and no token keys in localStorage.
- Required checks: API vet, golangci-lint, all tests including integration, build; web formatting, ESLint, Svelte/TypeScript checks, domain tests, Playwright and production build; nginx configuration check; migration Up/Down rehearsal.

Validation completed locally: the full API suite passed against disposable PostgreSQL, identity tests also passed with `-race`, and vet/lint/build succeeded. The web passed formatting, ESLint, Svelte/TypeScript checks, 46 unit tests, three Playwright flows and the production build. The production nginx image accepted the configuration. Migration 00013 was successfully rehearsed Down → Up against the disposable database with existing test users. OpenAPI parsed and all internal references resolved. The owner supplied the OAuth web client ID, which was stored only in the ignored local API `.env`, with the 90-day web setting. Migration 00013 was then applied to the shared Orbita database through the existing SSH tunnel, and the local API was restarted. A live browser check confirmed that the local API serves the configured client ID, the official Google button renders and the browser challenge cookie is HttpOnly and SameSite=Strict. The initial anonymous refresh correctly returns 401. Full sign-in with a real user account and production environment setup remain pending.

## Deviations

The public client ID is provided at runtime by the API rather than duplicated in frontend build settings. This keeps one image across environments. The shared database migration has been applied after receiving owner configuration. Production application configuration and deployments remain pending; the client secret and downloaded OAuth JSON are not used.
