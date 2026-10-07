# "Service temporarily unavailable" screen on the storefront and admin — design

Date: 2026-10-07. Status: design approved in chat; spec awaiting review.
Repos: eandstravelmongolia (storefront), admin. No digitalservice change.
Stacks on: the unmerged `feat/subscription-notice` branches of both repos (they own the layout fetch and the `ApiError` code field this work extends). Spec: `2026-10-07-subscription-expired-notice-design.md`.

## Goal

tenantcore runs on Render's free tier and is sometimes asleep, slow, or answering oddly. digitalservice already turns that into `503 TENANT/FEATURE_UNAVAILABLE`, but the front ends show a default error or "Something went wrong". Show a clear, friendly "service is waking up" screen that retries by itself.

## Background

- digitalservice answers `503`, domain `TENANT`, code `FEATURE_UNAVAILABLE`, message "tenant lookup is temporarily unavailable" whenever tenantcore cannot be asked or answers unusably (cold start, 404/5xx from the host, rejected service key) and nothing usable is cached. It serves a cached identity for up to 24 hours first, so the error appears mainly for keys not seen since digitalservice last restarted.
- A tenantcore `401` that names the tenant key becomes `401` for the front end ("unknown key"). That, `403` (wrong domain, suspended tenant) and every other error are NOT part of this work and keep today's behavior.
- Next.js hides server-render error text in production, so an error boundary cannot tell a 503 from any other crash. The layout must read the state itself and render the screen directly.

## Decisions (from the product owner)

- Only the 503 (tenant lookup unavailable) gets the screen, plus the case where digitalservice itself is unreachable. A real 401/403 is not hidden behind "retrying".
- The screen retries automatically every 10 seconds.

## Shared rule: the `unavailable` state

The status probe from the subscription work (`GET /subscription-status`) becomes three-way: `'active' | 'expired' | 'unavailable'`. It is `unavailable` when, and only when, the probe fails with one of:

1. HTTP 503 whose JSON error has `domain === 'TENANT'` and `code === 'FEATURE_UNAVAILABLE'`.
2. A network failure reaching digitalservice (connection refused, DNS, reset).
3. A timeout (8 seconds; see below).
4. HTTP 502, 503 or 504 whose body is NOT the digitalservice JSON envelope (a hosting proxy page while digitalservice is asleep).

A 503 JSON error with any other domain or code (for example another service's `FEATURE_UNAVAILABLE`) is NOT `unavailable`. Every other failure keeps returning `'active'` (no banner, fail open), as in the subscription spec. `unavailable` takes precedence over `expired`.

Timeout: the probe waits up to 8 seconds, not 2, so a slow-but-working backend is not mistaken for an outage. The wait only occurs when the backend is slow or down; a healthy cached call is unaffected.

The `unavailable` result is never cached. A successful `active`/`expired` answer keeps the 60-second cache from the subscription spec.

Pure classifier (plain `.mjs`, tested with `node --test`): `classifyStatusFailure({ status, body, networkError, timedOut }): 'unavailable' | 'active'`.

## Storefront (eandstravelmongolia)

- `getSubscriptionState()` returns the three-way state (rename to `getServiceState()` if clearer; keep one probe call).
- The `[locale]` layout: when `'unavailable'`, render a full-page `ServiceUnavailable` screen INSTEAD of header, children and footer. Text: "We're waking our service up. This page will retry on its own in a few seconds." with a "Try again now" button. Locale keys in `en.json`, `mn.json` and `ko.json` (Mongolian and Korean written by Claude, needing a native-speaker check).
- Retry: a small client component calls `router.refresh()` every 10 seconds and on the button. It pauses while the tab is hidden. It shows no countdown number (avoids hydration noise).
- Accessibility: `role="status"`, readable on mobile, no `dangerouslySetInnerHTML`, constant text only.
- Form submits (booking, rental, airport transfer, guide application, contact, newsletter): the route handlers' `upstreamFailure` maps a TENANT/FEATURE_UNAVAILABLE 503, a network failure and a gateway 502/503/504 to HTTP `503` with a distinct error the form shows as the localized message "Our service is waking up. Please try again in a minute." All other failures keep today's mapping.
- Limitation: pages already statically cached keep serving during an outage (a feature). The screen shows only on renders that need a fresh probe.

## Admin (admin app)

- `getSubscriptionState()` becomes three-way the same way (shared logic in a plain `.mjs`).
- Dashboard layout: when `'unavailable'`, keep the sidebar and topbar and show the notice in the content area instead of the page, with the same 10-second `router.refresh()` retry and button. Constant text: "The service is waking up. This page will retry on its own in a few seconds."
- `request()` in `src/lib/api/client.ts`: a 503 with domain `TENANT` and code `FEATURE_UNAVAILABLE`, or a network failure, throws an `ApiError` with a friendly message ("The service is temporarily unavailable. Please try again in a minute.") so every form shows it. `ApiError` also keeps `domain`. Other errors are unchanged.
- The existing error pages keep working; no new reliance on error text in production.

## Out of scope

A keep-warm ping for tenantcore; tenantcore or digitalservice client timeouts; the `401`/`403` cases; anything about subscriptions beyond sharing the probe.

## Verification

- `node --test` for the classifier and the message mapping, `tsc`, `next build --webpack`, `npm run lint` in each front end.
- Live check is possible locally: the running stack already returns this exact 503 for the storefront's tenant key. Load the storefront and admin, confirm the screen and the 10-second retry, then (after the service key is fixed) confirm recovery.
- Not verifiable here: a real Render cold start, and the Mongolian and Korean wording.

## Merge notes

Both front-end branches stack on `feat/subscription-notice`; merge those PRs first. The admin work also shares `layout.tsx` and `client.ts` with the open notifier admin PR.
