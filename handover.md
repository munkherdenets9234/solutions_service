# digitalservice — handover (2026-10-01)

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
