# digitalservice — handover (2026-10-01)

## Principle (2026-10-05): tenantcore owns tenants; this service serves them

Tenantcore is responsible for ALL tenant information and management (identity, API key, status, domain, plan, subscription). digitalservice provides its service (tours, bookings, content, users) to tenants identified by API key and is not meant to manage tenants. Today it still keeps a duplicate `tenants` collection and resolves `X-API-Key` locally (`internal/middleware/tenant.go` -> `TenantService.Resolve`), so the two copies can differ, which is why a key rotated in tenantcore does not change what this service accepts. Do not add new tenant-management features here. Planned fix: `tenantcore/docs/superpowers/specs/2026-10-05-central-tenant-resolution-design.md` (resolve through tenantcore, cache 60 s fresh / 24 h stale, tenantcore wins and keys are re-issued).

## Tenant resolution switch (2026-10-05)

- Settings: `TENANT_RESOLVER` (`local` | `tenantcore`, default `local`), `TENANT_RESOLVE_RATE_PER_MINUTE` (default 600) and `TENANT_RESOLVE_BURST` (default 120). Documented in `.env.example`. The limiter exists only in `tenantcore` mode, runs before the tenant gate and keys on client IP (120 at once, then 10/s per IP). It uses `ClientIP`, which trusts X-Forwarded-For like the other limiters, so behind a proxy the trusted-proxy setup must be right; a storefront host sharing one IP must stay under the rate.
- Fail closed: `tenantcore` mode without `TENANTCORE_URL`/`TENANTCORE_SERVICE_KEY` stops startup (Validate in `New`, plus a backstop in wiring); there is no automatic fallback to the local collection.
- Default `local` behaves exactly as before. `tenantcore` resolves X-API-Key through `internal/tenantresolve` (needs `TENANTCORE_URL` and `TENANTCORE_SERVICE_KEY`; startup refuses without them). Unknown key 401 (same body as local), suspended 403, tenantcore unreachable with nothing cached 503 (never 401). `/readyz` adds `tenant_resolver` and `degraded:true` while tenantcore is unreachable.
- To flip: set `TENANT_RESOLVER=tenantcore` and restart; to roll back, set it to `local` (or unset) and restart.
- Rollback caveat: a key re-issued in tenantcore leaves this service's local key hash stale. After rolling back to `local`, the re-issued key is refused here until the local hash is updated, and the old key works again.
- Not run live: all verification used httptest fakes.

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
