# digitalservice — handover (updated 2026-10-06)

## Latest state (2026-10-06) — read this first; it supersedes everything below where they differ

**The local tenant resolver is gone.** `X-API-Key` is ALWAYS resolved through tenantcore (`internal/tenantresolve`, cached 60 s fresh / 24 h stale, 30 s negative). This service never validates a key against its own `tenants` collection. Removed: `TENANT_RESOLVER` (setting + `local` mode), `middleware.NewLocalResolver`, `TenantService.Resolve`, `TenantRepo.FindByAPIKeyHash`. Principle and decisions: tenantcore `docs/superpowers/specs/2026-10-05-central-tenant-resolution-design.md`.

- **Required settings now:** `TENANTCORE_URL` and `TENANTCORE_SERVICE_KEY`. Startup stops without them (Validate, plus a backstop in `buildTenantResolver`). There is no fallback and no rollback switch.
- `TENANT_RESOLVER` is retired. Unset is right. A leftover `tenantcore` is tolerated (`Config.LegacyTenantResolver`); any other value, including `local`, fails Validate by name.
- Still configurable: `TENANT_RESOLVE_RATE_PER_MINUTE` (600), `TENANT_RESOLVE_BURST` (120), `TRUSTED_PROXIES`. The resolve limiter is on whenever `RATE_LIMIT_ENABLED` is. `/readyz` always lists `tenant_resolver_tenantcore`, carries `tenant_resolver` while tenantcore is unreachable, and notes an unset `TRUSTED_PROXIES` (startup logs a WARN too; set it to the host's proxy ranges before production).
- Key states: unknown key 401, suspended 403, tenantcore unreachable with nothing cached 503 (never 401). Any status other than `active` counts as suspended.
- **DEPLOYING THIS BREAKS PRODUCTION unless `TENANTCORE_URL` and `TENANTCORE_SERVICE_KEY` are set on the host** (Render) and every tenant's key is known to tenantcore. The migration `tenantcore/cmd/migrate-from-digitalservice` (`-only-missing`) has never run live, and keys must be re-issued per tenant. Needs the owner's approval per step; see `tenantcore/docs/superpowers/runbooks/2026-10-05-central-tenant-resolution-rollout.md` (its `TENANT_RESOLVER` and rollback steps are obsolete).
- The `tenants` collection and `TenantService.Create/RotateAPIKey` still exist (platform tenant CRUD, `api_key_hash`/`api_key_last4`), but nothing validates against them. Cleanup left: retire that CRUD and the stored hashes once tenantcore is the only admin path.

**Dev-only exact errors (2026-10-06, built, tested, uncommitted).** Error bodies gain `error.detail` with the specific cause (missing header, key not recognised, suspended, domain mismatch; for an internal error, its cause). It is emitted ONLY when `APP_ENV` is not production AND the TCP peer is loopback (`RemoteAddr`, not a forwarded header): `pkg/response/response.go` (`isLoopbackPeer`), `apierr.APIError.WithDetail`. The public `message` stays the same for every cause. Tests: `pkg/response/response_test.go`. The E&S site's `ApiError` shows it in dev (`GET /destinations -> 401 AUTH/UNAUTHORIZED: ...`).

**Local setup that works (verified 2026-10-06):** tenantcore :8092, digitalservice :8080 (start with `PORT=8080`; `.env` has `APP_PORT=8081`), E&S site :3000 with `API_BASE_URL=http://localhost:8080/api/v1` (it had `https://`, which gave `ERR_SSL_WRONG_VERSION_NUMBER`), travel admin :3001, inno admin :3011 (autoPort may pick another). The E&S key in the site `.env.local`, this `.env` and tenantcore all end `4d1b`; the site gets 200 and a bogus key gets 401 `tenantcore does not recognise this key`.

**State of the tree:** branch `feat/guide-recruitment`, uncommitted, NOT pushed. Check: `go build ./... && go vet ./internal/... ./pkg/... && go test ./internal/... ./pkg/... -count=1` is green (20 packages, 2026-10-06). Besides the work above, the tree also holds changes made earlier that this session did not touch: `internal/api/tenant/private/{account,content,operations,translations,translations_test,uploads,users}.go` and an untracked `cmd/zz-throwaway/` (throwaway helper from the password-reset verification; delete when done). Superadmin login with the `.env` credentials returns 401 (see Open items).
## Request email deploy steps (2026-10-07)

Request email is opt-in: with `MAIL_UNSUBSCRIBE_KEY`, `PUBLIC_BASE_URL` and `ADMIN_BASE_URL` all unset the service starts with the feature off (one WARN, `request_email` disabled on `/readyz`, unsubscribe routes 404, `/admin/mail-outbox` still reads existing rows). Set one and startup fails unless all three are valid. Order matters:

1. Deploy tenantcore first, at the commit that contains the `request_notification` template (branch `feat/request-notification-template`, `9e30e6c`). Without it every send fails.
2. On the digitalservice host (Render), generate a random `MAIL_UNSUBSCRIBE_KEY` of at least 32 bytes (never commit it). Set `PUBLIC_BASE_URL` to this service's public https origin and `ADMIN_BASE_URL` to the admin console's https origin: scheme https, host only, no path, query or fragment. `TENANTCORE_URL` and `TENANTCORE_SERVICE_KEY` must already be set.
3. Deploy digitalservice.
4. Check `/readyz`: `request_email` must show enabled.
5. Real delivery still needs the Gmail App Password configured in tenantcore.

Links in the mail: unsubscribe is `PUBLIC_BASE_URL/api/v1/public/unsubscribe?token=...`; the record link is `ADMIN_BASE_URL/<bookings|rentals|airport-transfers|guide-applications>/<id>`. Both origins are global (the admin app is single-tenant per deployment); per-tenant admin hosts are future work. Untested live.

## Update 2026-10-02 (latest; supersedes the status above where they differ)

- Branch `refactor/backend-core`, working tree clean, 13 commits unpushed. Check: `go build ./... && go vet ./internal/... ./pkg/... && go test ./internal/... ./pkg/... -count=1` (never `go test ./...`). Start with `PORT=8080` (this repo's `.env` has `APP_PORT=8081`; the launch config handles it).
- **Tenant password reset: DONE, verified live** (Plan B Task 6 reduced to a real reset on the E&S tenant user; the subscription-gate exemption and timing checks were not run). Bug found live and fixed in `8e29af1`: a user with an empty `name` made tenantcore answer 500 to the code mail, so the code was stored but never sent (`greetingName` now falls back to "there").
- **Editable site translations** (new, `930b9c8` and earlier): collection `site_pages` per tenant; admin `GET/GET/PUT /admin/translations[/:page]` (token, role admin, PUT behind the subscription gate); public `GET /translations?lang=en|mn|ko` (API key only). Validation by shape only. Each stored value may carry a per-language `base` (shipped wording at seed time); the public read omits a value equal to its `base`. Error messages never echo submitted text. Files: `internal/models/site_page.go`, `internal/service/site_page_service.go`, `internal/repository/site_page_repo.go`, `internal/api/tenant/{private,public}/translations.go`.
- Tenants in this service's database: `travel-tour-mongolia` (E&S), `nomad-trails`, `inno-nomads`. **Nelson Travel and Bayan Bogd are NOT here**, so cancelling them in tenantcore has no effect on this API. Cancelled-tenant behaviour here: writes 402, reads 200; login, lead forms and password reset are exempt.
- Needs tenantcore up: with it down every write answers 500 (fail-closed by design).
- Still true from below: digitalservice's `/login` answers differently for an unknown email; three older PII exposures remain.

- **Ports / how to start (2026-10-02):** tenantcore :8092, digitalservice :8080, travel admin :3001, inno dashboard :3011, carwash :8091, carwash-web :3002. The launch-config entry `eandstravelmongolia` serves 404 on every page (its `npm --prefix` form starts Next from the repo root): start the E&S site with `npm run dev -- -p 3000` from `eandstravelmongolia/`. Port 3000 may be another project; check the page title.
- **Pushing is blocked from this machine:** GitHub answers `Permission denied (publickey)` for `~/.ssh/id_ed25519`. Nothing from the 2026-10-01/02 sessions was pushed except what the user pushed themselves (tenantcore `backend-update` was merged as PR #1). Unpushed at last check: digitalservice 13, E&S site 7, travel admin 6, inno admin 6, inno site 1, carwash 1, carwash-web 1.
- **Production tenantcore** is `https://core-backend-5cjs.onrender.com`. Checked 2026-10-02: `/healthz` and `/readyz` 200, public API 200, admin routes 401 without a token, `POST /api/v1/admin/password-reset/request` is registered but answers **503** because `GMAIL_EMAIL`/`GMAIL_PASSWORD` are not set on Render (`/readyz` is `degraded`; `email` and `expiry_notice` are off). Set a Google **App Password** (16 lowercase letters) there, and `EXPIRY_NOTICE_EMAIL`.
- **Inno dashboard production 404 on password reset:** `POST <host>/admin/password-reset/request` (no `/api/v1`) is 404 on production, which is exactly the dashboard's error. The dashboard's server-side `API_URL` on Vercel must be `https://core-backend-5cjs.onrender.com/api/v1` (and `NEXT_PUBLIC_API_URL` the same); redeploy after changing. Not confirmed: the Vercel settings could not be seen.
- **Cancelled on 2026-10-02 (by the user, in tenantcore):** Nelson Travel and Bayan Bogd (Bayan Bogd is the tenant behind carwash/carwash-web). E&S Discovery Mongolia is still active and its subscription **ends 2026-10-20**: after that its writes (including saving translations) return 402 until renewed. Plan question still open: E&S is on `starter`, the assistant had set `travel-pro`; ask the user, change nothing unasked.
- **Credentials:** the user typed a password in chat for sign-in during the session. It is stored nowhere. Suggest changing it. Never print `.env`.

Work was **paused mid-plan** because the session context was filling. The tree is clean and the suite is green; the remaining step is a live verification, not code.

- Branch: `refactor/backend-core` (**no upstream**, nothing pushed). Last commit: `cd75030 feat: add tenant password reset routes`.
- Working tree: clean.
- Check: `go build ./... && go vet ./internal/... ./pkg/... && go test ./internal/... ./pkg/... -count=1` is green. **Do not run `go test ./...`**: `test/api` needs MongoDB/testcontainers and is a separate target.
- A pre-commit hook runs gitleaks and static analysis on every commit.

## What digitalservice is
The multi-tenant travel API (destinations, bookings, tenant users, storefront reads) on Go + Gin + MongoDB. Runs on **:8080** (launch config `digitalservice`). Tenants are resolved from `X-API-Key`; tenant admin users log in with email/password and get an HS256 token.

## What changed this session (all committed)

**1. Subscriptions moved to tenantcore** (`38d6ac9`, with the earlier uncommitted backend restructure it sat on). digitalservice stores **no** subscriptions any more. `SubscriptionMiddleware` (the write gate) reads through `entitlement.Provider`, which is an HTTP client to tenantcore (`internal/entitlement/client.go`). Failure rules: a lookup failure is a 500, **never** a 402; `StatusUnknown` (no subscription) passes; a tenantcore 404 passes as unprovisioned. `entitlement.Unenforced` is what runs when `TENANTCORE_URL`/`TENANTCORE_SERVICE_KEY` are blank.

**2. Tenant-user password reset** (`bff414c`..`cd75030`), the forgot-password flow for travel-admin users:

| Piece | Where |
|---|---|
| Code model (hashed, attempts, expiry, tenant-scoped) | `internal/models/tenant_password_reset.go` |
| Repository + filters (+ TTL index) | `internal/repository/tenant_password_reset_repo.go`, `indexes.go` |
| Mail client to tenantcore (`POST /api/v1/svc/notifications/email`) | `internal/notify/client.go` |
| Service (the rules) | `internal/service/tenant_password_reset_service.go` |
| Routes `POST /api/v1/password-reset/request` and `/confirm` | `internal/api/tenant/public/password_reset.go` |
| Readiness entry `password_reset`, docs in OpenAPI | `internal/config/config.go`, `internal/api/docs/docs/openapi.json` |

Rules (spec: `tenantcore/docs/superpowers/specs/2026-10-01-tenant-user-password-reset-design.md`): 6-digit code, SHA-256 hashed, 10 minutes, single use, **5 wrong guesses burn it**; every confirm failure returns the same message; the request returns **before any account-dependent work** (the lookup, insert and mail run in a goroutine) so response time reveals nothing; the code is looked up **per tenant** and the user it was issued for must match the user found in that tenant; routes are **outside the subscription gate**, beside `/login`.

Tested with fakes, and the key guarantees were **mutation-checked** (removing the identity check, suspension re-check, background hand-off or attempt counter each fails a named test). `-race` is unavailable.

## Plan status
Plan: `tenantcore/docs/superpowers/plans/2026-10-01-tenant-user-password-reset.md`. Ledger (this machine only, gitignored): `tenantcore/.superpowers/sdd/2026-10-01-tenant-user-password-reset/progress.md`.

- Tasks 1–4: done here.
- Task 5 (travel `admin` page): written but **uncommitted and not clicked through** (see `admin/handover.md`).
- **Task 6 pending — live verification**, which needs this service, tenantcore and the travel admin running:
  1. Create a throwaway tenant named `ZZ-THROWAWAY reset test` and one active user with the email `munkherdene@bdsec.mn`, by a guarded one-off helper that refuses any tenant not named `ZZ-THROWAWAY…`. **Never use E&S's real admin** (`enkhjin.erdenebuyan@gmail.com`).
  2. Request a code through the travel admin; the user confirms the email arrives, completes the reset to a throwaway password; confirm the old password no longer signs in.
  3. **Subscription-gate exemption** (Review Focus 4): give the throwaway a cancelled subscription in tenantcore (mirrored at the same `_id`), wait out the 60 s entitlement cache, confirm a gated write returns **402** and `POST /password-reset/request` still returns **200**. This cannot be unit-tested without a database.
  4. Timing: paced requests (limit 10/min) for a real vs unknown address should differ only by noise.
  5. Clean up everything, including mirrored tenantcore records.

## Open items and known issues
- **`SUPERADMIN_PASSWORD` in `.env` no longer matches the stored hash** for `munkherdene.ts9234@gmail.com` (changed after seeding), so API-driven setup via `/platform/login` fails. Direct DB inserts were used instead. Don't reset it without asking.
- Only **E&S** (`6a47a932c11d67fbde0d0cd4`) is enforced; Inno Nomads and Nomad Trails don't exist in tenantcore, so they run unenforced. The migration (`tenantcore/cmd/migrate-from-digitalservice`) has never run.
- `digitalservice/.env` has `TENANTCORE_URL` and `TENANTCORE_SERVICE_KEY` (service client `digitalservice-local`). The key is shown once and cannot be recovered; revoke via tenantcore if lost.
- digitalservice's own **`/login` still answers differently for an unknown email** (an account-existence oracle). It is rate limited and was an accepted exposure since the refactor; the new reset does not have this flaw. Three older exposures from the refactor are also still open: tenant admin reads need only the API key, `GET /platform/tenants` and `/platform/admins` are public.
- A reset does **not** end sessions already signed in (stateless tokens, 24 h).

## Gotchas
- Never print `.env`. Check shape, not value.
- `internal/api/docs/docs/openapi.json` is **CRLF**; edit as bytes and write back CRLF.
- Windows shell: put long Python in a file rather than a heredoc; a command moved to the background keeps running its remaining steps (including `git commit`).
- A `httptest` server with a blocked handler deadlocks if `close(block)` is registered **before** the server's `Close`; cleanups run last-in-first-out.
