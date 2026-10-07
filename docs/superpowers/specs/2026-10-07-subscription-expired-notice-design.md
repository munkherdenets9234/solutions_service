# Subscription expired notice on the storefront and admin — design

Date: 2026-10-07. Status: design approved in chat; spec awaiting review.
Repos: digitalservice (new route), eandstravelmongolia (storefront banner), admin (banner and 402 message).

## Goal

When a tenant's subscription is expired, tell people on both front ends:

- Storefront visitors see a notice on every page. The site keeps working.
- Admin staff see a banner on every dashboard page, and a clear message when a save is refused.

## Background (what exists today)

- digitalservice answers `402 SUBSCRIPTION_REQUIRED` for writes (POST, PUT, PATCH, DELETE) of a tenant whose subscription is not active. Reads always pass.
- The storefront lead forms (booking, rental, airport transfer, guide application, contact) are deliberately OUTSIDE the subscription gate: a lapsed tenant must still receive bookings. They never return 402.
- So the storefront has no expired signal today, and the admin only learns when a save fails.
- The `503 FEATURE_UNAVAILABLE` seen when tenantcore is unreachable is a different condition and is out of scope.
- Neither front end has a dedicated "unavailable" screen. The admin shows a generic "Something went wrong" page.

## Decision (from the product owner)

Notice only. The storefront keeps working and keeps taking bookings; it adds a visible notice. No hard stop, no change to the gate.

## Constraints

- Never tell a paying customer they lapsed because the lookup failed. Unknown or failed lookups report `active`, the same rule `SubscriptionMiddleware` follows.
- The new route exposes only the state. No plan, dates, amounts or tenant data.
- No new dependencies in any repo. Secrets stay server-side.
- Storefront wording is shown in en, mn and ko.

## 1. digitalservice: `GET /api/v1/subscription-status`

- Mounted in the tenant public routes, behind the tenant API key like the storefront reads, and OUTSIDE the subscription gate.
- Response, using the repo's standard envelope: `{"state": "active" | "expired"}`.
- `expired` iff the entitlement provider returns a known status that is not active, i.e. `!ent.Active()` and status is not `StatusUnknown` (past due, cancelled, lapsed). Trialing counts as active.
- `active` for an active or trialing tenant, an unknown status, a stale answer, and any provider error. A provider error is logged with the tenant id only and never turned into `expired`.
- Uses the existing entitlement provider via a narrow interface. No new database access.
- Rate limited with the existing resolve limiter group like its neighbours. `GET` only.
- Response header `Cache-Control: private, max-age=60`. The answer is per tenant, so shared caches must not store it.
- Tests with a fake provider: `active` and `trialing` return `active`; `past_due` and `canceled` return `expired`; unknown status and a provider error return `active`. Route test that it needs the API key, and that it answers for a lapsed tenant (it sits outside the subscription gate). `guard_test.go` classification if the guard requires it.

## 2. Storefront (eandstravelmongolia)

- Server helper `getSubscriptionState(): Promise<'active' | 'expired'>` in `src/lib/`: calls the new route through the existing API client with a 60 second revalidate. Any failure, non-200 or unexpected body returns `'active'` (fail open, no banner).
- The `[locale]` layout renders a slim banner above the header when the state is `expired`. Not dismissible. Uses existing site colours and an accessible `role="status"`.
- New locale key `subscription.expiredNotice` in `src/locales/en.json`, `mn.json` and `ko.json`. English: "This site's subscription has expired. Some features may stop working." The Mongolian and Korean text is written by Claude and needs a native-speaker check. The key follows the existing translation pipeline (`getTranslation`), so it also becomes editable in the admin Translations page.
- The booking and contact forms and every other page are unchanged.
- Pure helper for state parsing in a plain `.mjs` file, tested with `node --test` (a `node --test` convention already exists in the repo: `node --test src/lib/translations/`).

## 3. Admin (admin app)

- The dashboard layout fetches `GET /subscription-status` with `cache: 'no-store'` and shows a banner above the content on every page when `expired`: "Your subscription has expired. You can still view your data, but changes can't be saved until it is renewed. Contact your platform administrator." Failure to fetch shows nothing.
- `request()` in `src/lib/api/client.ts`: a response with status 402 and error code `SUBSCRIPTION_REQUIRED` throws an `ApiError` whose message is the friendly text above (the existing forms already display `ApiError.message`). Other 402 codes (`MODULE_NOT_ENTITLED`, `LIMIT_EXCEEDED`) keep the backend message.
- `error.tsx` (dashboard and root): when the error carries the subscription message, show it instead of "Something went wrong".
- Pure helpers (state parsing, 402 mapping) in plain `.mjs` with `node --test`.

## Out of scope

Hard stop or blocking forms for lapsed tenants; renewal links or payment; showing plan or dates; changing the 503 tenantcore-unreachable behavior; a contact address in the banner (none is known).

## Verification and what stays unproven

- Go gate: `go build ./... && go vet ./internal/... ./pkg/... && go test ./internal/... ./pkg/... -count=1` (never `go test ./...`).
- Front ends: `node --test` for the helpers, `tsc`, `next build`, `lint` (the admin Turbopack build needs `--webpack` in a worktree with a `node_modules` junction).
- Not verifiable here: a real expired tenant (needs a subscription change in tenantcore's database), and the Mongolian and Korean wording.

## Merge notes

The admin change touches `src/app/(dashboard)/layout.tsx` and `src/lib/api/client.ts`, which the open notifier admin PR also changes. Expect a small conflict there.
