# Subscription Expired Notice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show "subscription expired" on the storefront (notice, site keeps working) and in the admin (banner on every page, friendly message on a refused save).

**Architecture:** digitalservice gets one read route, `GET /api/v1/subscription-status`, that reports `active` or `expired` from the existing entitlement provider and fails open. The storefront layout and the admin dashboard layout each fetch it and render a banner. The admin API client maps `402 SUBSCRIPTION_REQUIRED` to a friendly message.

**Tech Stack:** Go (gin) in `digitalservice`; Next.js server components in `eandstravelmongolia` and `admin`; pure helpers in plain `.mjs` tested with `node --test`.

**Spec:** `digitalservice/docs/superpowers/specs/2026-10-07-subscription-expired-notice-design.md`

## Global Constraints

- Unknown, stale or failed lookups report `active`. Only a known status that is not active reports `expired` (`past_due`, `canceled`, or an `active`/`trialing` subscription past its period end, i.e. `!Entitlement.Active()` with a status other than `entitlement.StatusUnknown`).
- The route returns only `{"state": "active" | "expired"}`. No plan, dates, amounts or tenant data. Header `Cache-Control: private, max-age=60`.
- The route sits behind the tenant API key and OUTSIDE the subscription gate; it is `GET` only and rate limited like its neighbours.
- No new dependencies in any repo; `package.json` and lock files stay unchanged; secrets stay server-side.
- Storefront banner text: en "This site's subscription has expired. Some features may stop working."; also mn and ko (Claude-written, flagged for native review). Admin banner text: "Your subscription has expired. You can still view your data, but changes can't be saved until it is renewed. Contact your platform administrator."
- Only error code `SUBSCRIPTION_REQUIRED` gets the friendly admin message; other 402 codes (`MODULE_NOT_ENTITLED`, `LIMIT_EXCEEDED`) keep the backend message.
- Never log tokens, API keys or the full backend body. AGENTS.md rules apply in every repo (no `--no-verify`, nothing pushed, `.env*` values never printed).
- Go gate (digitalservice): `go build ./... && go vet ./internal/... ./pkg/... && go test ./internal/... ./pkg/... -count=1`; never `go test ./...`. Front ends: `node --test <files>`, `npx tsc --noEmit`, `next build`, `npm run lint` (report existing lint errors separately, do not fix unrelated files).
- Each repo gets its own branch from `master`, created in its own git worktree: digitalservice `D:\bkup\projects\digitalbrochure\digitalservice-subexp` (branch `feat/subscription-status`, already exists); storefront `eandstravelmongolia-sub` (`feat/subscription-notice`); admin `admin-sub` (`feat/subscription-notice`). Never work in the user's own checkouts. For the front-end worktrees, link `node_modules` with a directory junction to the main checkout's `node_modules` and use `next build --webpack` (Turbopack rejects the junction).

## Review Focus

- A provider error, a stale answer and an unknown status never produce `expired` (the "never tell a paying customer they lapsed" rule).
- A lapsed tenant (past_due, canceled, or past its period end) gets `expired` AND the route still answers 200 (it is outside the subscription gate).
- No API key: the route is refused like the other tenant routes; the response carries only the state.
- Storefront: the status call failing, timing out or returning garbage shows NO banner and does not break the page render.
- Admin: a 402 with another code keeps its message; a non-402 error is unchanged; the banner fetch failing shows nothing.
- Banner HTML never injects backend-supplied text (the message is a constant; only the state enum is read).

---

### Task 1: digitalservice `GET /api/v1/subscription-status`

**Files (digitalservice worktree):**
- Create: `internal/service/subscription_status_service.go`, `internal/api/tenant/public/subscription_status.go`
- Modify: `internal/api/tenant/public/public.go` (Deps field and route), `internal/api/tenant/tenant.go` (pass `Entitlement` into the public Deps), `internal/api/guard_test.go` only if the guard requires a classification
- Test: `internal/service/subscription_status_service_test.go`, `internal/api/tenant/public/subscription_status_test.go`

**Interfaces:**
- Produces: `type SubscriptionState string` with `SubscriptionActive = "active"` and `SubscriptionExpired = "expired"`; `func NewSubscriptionStatusService(p entitlement.Provider, log *zap.Logger) *SubscriptionStatusService`; `func (s *SubscriptionStatusService) State(ctx context.Context, tenantID primitive.ObjectID) SubscriptionState` (never returns an error, nil receiver or nil provider returns `SubscriptionActive`); `public.Deps.SubscriptionStatus *service.SubscriptionStatusService`; route `GET /subscription-status` returning `{"state": ...}` in the standard envelope with header `Cache-Control: private, max-age=60`.
- Consumes: `entitlement.Provider.For(ctx, tenantID) (Entitlement, error)`, `Entitlement.Active()`, `entitlement.StatusUnknown`.

- [ ] **Step 1: Write failing service tests** in `subscription_status_service_test.go` using `entitlement.ProviderFunc`: `TestActiveAndTrialingAreActive`; `TestPastDueAndCanceledAreExpired`; `TestActivePastPeriodEndIsExpired` (`PeriodEnd` one hour ago); `TestUnknownStatusIsActive`; `TestProviderErrorIsActiveAndLogsIdOnly` (assert `active`; log output contains the tenant id and no error text from the provider); `TestStaleAnswerIsActive` (`Stale: true` with status `canceled` returns `active`, because a stale answer is not proof); `TestNilReceiverAndNilProviderAreActive`.
- [ ] **Step 2: Run** `go test ./internal/service -run SubscriptionStatus -v`. Expected: FAIL, undefined `NewSubscriptionStatusService`.
- [ ] **Step 3: Implement** `State` as: nil guards, `ent, err := p.For(...)`; on error log `tenant_id` plus a fixed outcome code and return active; if `ent.Stale` or `ent.Status == StatusUnknown` return active; if `!ent.Active()` return expired; else active.
- [ ] **Step 4: Write failing route tests** in `public/subscription_status_test.go` (httptest with the real tenant public `Register` and the existing test helpers used by neighbouring public tests): `TestStatusReturnsOnlyTheState` (response JSON data has exactly the key `state`); `TestStatusExpiredForLapsedTenantStill200`; `TestStatusSetsPrivateCacheHeader`; `TestStatusRequiresAPIKey` (no key gives the same refusal other tenant routes give; reuse how `TestEveryTenantRouteRequiresAPIKey` or sibling tests assert it); `TestStatusIsGetOnly` (POST is 404/405 per router behaviour).
- [ ] **Step 5: Run** `go test ./internal/api/... -run SubscriptionStatus -v`. Expected: FAIL.
- [ ] **Step 6: Implement** the controller and register `GET /subscription-status` on the `exempt` (outside-the-gate) group in `public.Register`, behind `d.AuthRateLimit`-style limiter only if siblings in that group use one for reads (follow the nearest read neighbour); add `SubscriptionStatus` to `public.Deps` and build it in wiring (`internal/bootstrap/wiring.go`) from the existing entitlement provider; update `tenant.Register` to pass it. Add the route to `guard_test.go` only where the guard demands.
- [ ] **Step 7: Run** the Go gate. Expected: PASS.
- [ ] **Step 8: Commit** only touched files in the digitalservice worktree, message `feat: GET /subscription-status reports active or expired`.

---

### Task 2: Storefront banner (eandstravelmongolia)

**Files (storefront worktree):**
- Create: `src/lib/subscription-state.mjs`, `src/lib/subscription-state.d.mts`, `src/lib/subscription-state.test.mjs`, `src/lib/api/subscription.ts`, `src/components/layout/SubscriptionNotice.tsx`
- Modify: `src/app/[locale]/layout.tsx`, `src/locales/en.json`, `src/locales/mn.json`, `src/locales/ko.json`; the translation type or merge code only if a new key needs registering (read `src/lib/translations/` first)

**Interfaces:**
- Produces: `parseSubscriptionState(body: unknown): 'active' | 'expired'` (returns `'expired'` only for an object whose `state === 'expired'`; everything else, including `null`, strings, arrays and unknown values, returns `'active'`); `getSubscriptionState(): Promise<'active' | 'expired'>` (server-only, calls `GET /subscription-status` through `request` in `src/lib/api/client.ts` with `next: { revalidate: 60 }` and returns `'active'` on any thrown error); `<SubscriptionNotice t={...} />` server component; locale key `subscription.expiredNotice`.
- Consumes: Task 1's route.

- [ ] **Step 0: Create the worktree** `git worktree add -b feat/subscription-notice ../eandstravelmongolia-sub master` from the storefront repo; junction `node_modules`; copy nothing else (`.env.local` is only needed for a manual run and is not part of this task).
- [ ] **Step 1: Write failing tests** `subscription-state.test.mjs` with `node:test`: `expired object returns expired`; `active object returns active`; `null`, `undefined`, `'expired'` (a bare string), `[]`, `{state: 'EXPIRED'}`, `{state: 'weird'}`, `{}` all return `active`; and the locale check: `en.json`, `mn.json`, `ko.json` each have a non-empty string `subscription.expiredNotice`.
- [ ] **Step 2: Run** `node --test src/lib/subscription-state.test.mjs`. Expected: FAIL, module not found.
- [ ] **Step 3: Implement** `parseSubscriptionState` in `subscription-state.mjs` with a `.d.mts` typing; add the three locale entries (English text from Global Constraints; write natural Mongolian and Korean equivalents).
- [ ] **Step 4: Run** the same command. Expected: PASS.
- [ ] **Step 5: Implement `getSubscriptionState` and the banner.** In `src/lib/api/subscription.ts` call the existing API client (extend `client.ts` only by exporting a small `apiGetFresh`-style option if `apiGet`'s 300s revalidate does not allow a 60s override; prefer a `revalidate` parameter over duplicating `request`). `SubscriptionNotice` renders a slim full-width bar (`role="status"`, existing site colour tokens, readable on mobile) with `t.subscription.expiredNotice`; the `[locale]/layout.tsx` fetches the state next to `getTranslation` and renders the notice above `<Header />` only when `'expired'`. A fetch failure must not throw out of the layout.
- [ ] **Step 6: Verify:** `npx tsc --noEmit`, `npx next build --webpack`, `npm run lint` (separate existing lint errors from new ones). Expected: no new errors.
- [ ] **Step 7: Commit** only touched files in the storefront worktree, message `feat: show a notice when the tenant subscription has expired`.

---

### Task 3: Admin banner and 402 message (admin)

**Files (admin worktree):**
- Create: `src/lib/subscription.mjs`, `src/lib/subscription.d.mts`, `src/lib/subscription.test.mjs`, `src/lib/data/subscription.ts`, `src/components/admin/SubscriptionBanner.tsx`
- Modify: `src/lib/api/client.ts`, `src/app/(dashboard)/layout.tsx`, `src/app/(dashboard)/error.tsx`, `src/app/error.tsx`

**Interfaces:**
- Produces: `SUBSCRIPTION_EXPIRED_MESSAGE` (the admin text from Global Constraints); `parseSubscriptionState(body: unknown): 'active' | 'expired'` (same rule as Task 2); `friendlyApiMessage(status: number, code: string | undefined, fallback: string): string` (returns `SUBSCRIPTION_EXPIRED_MESSAGE` only for status 402 with code `SUBSCRIPTION_REQUIRED`, otherwise `fallback`); `getSubscriptionState(token?: string): Promise<'active' | 'expired'>` in `src/lib/data/subscription.ts` (server-only, `GET /subscription-status`, `cache: 'no-store'`, `'active'` on any failure); `<SubscriptionBanner />`.
- Consumes: Task 1's route; the existing `ApiError` (`status`, message) and `request()` in `client.ts`.

- [ ] **Step 0: Create the worktree** `git worktree add -b feat/subscription-notice ../admin-sub master` from the admin repo; junction `node_modules`.
- [ ] **Step 1: Write failing tests** `subscription.test.mjs`: `parseSubscriptionState` table as in Task 2; `friendlyApiMessage(402, 'SUBSCRIPTION_REQUIRED', 'x')` returns the expired message; `friendlyApiMessage(402, 'MODULE_NOT_ENTITLED', 'x')` returns `'x'`; `friendlyApiMessage(402, 'LIMIT_EXCEEDED', 'x')` returns `'x'`; `friendlyApiMessage(500, 'SUBSCRIPTION_REQUIRED', 'x')` returns `'x'`; `friendlyApiMessage(402, undefined, 'x')` returns `'x'`.
- [ ] **Step 2: Run** `node --test src/lib/subscription.test.mjs`. Expected: FAIL.
- [ ] **Step 3: Implement** `subscription.mjs` with the `.d.mts` typing.
- [ ] **Step 4: Wire the 402 mapping.** In `client.ts` `request()`, where the failure `ApiError` is built, pass `friendlyApiMessage(res.status, json?.error?.code, <existing message>)` as the message; keep `status` and `code` on the error so callers can still branch. In `(dashboard)/error.tsx` and `error.tsx`, when `error.message` equals `SUBSCRIPTION_EXPIRED_MESSAGE`, render that text with a neutral heading ("Subscription expired") instead of "Something went wrong".
- [ ] **Step 5: Wire the banner.** `getSubscriptionState` in `src/lib/data/subscription.ts` (uses the session token like other data helpers; failure returns `'active'`). `SubscriptionBanner` is a server component with `role="status"`, constant text only. The dashboard layout fetches the state (in parallel with the session read where practical) and renders the banner above the page content only when `'expired'`.
- [ ] **Step 6: Verify:** `npx tsc --noEmit`, `npx next build --webpack`, `npm run lint` (the existing error in `tours/page.tsx` is known and unrelated). Expected: no new errors.
- [ ] **Step 7: Commit** only touched files in the admin worktree, message `feat(admin): subscription expired banner and friendly 402 message`.

---

## Self-review notes

- Spec coverage: §1 route -> Task 1; §2 storefront -> Task 2; §3 admin -> Task 3; "not verifiable" -> each task reports the live check on a real expired tenant as untested.
- Known open choices left to the implementer by design: where exactly the new route sits among the exempt-group limiters (Task 1 step 6) and how the storefront client exposes a 60s revalidate (Task 2 step 5); both must keep behaviour for existing callers unchanged.
