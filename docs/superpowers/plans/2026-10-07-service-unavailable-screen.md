# Service Unavailable Screen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When tenantcore (Render free tier) or digitalservice is asleep or unreachable, show a friendly "service is waking up" screen that retries by itself, on the storefront and in the admin, instead of default errors.

**Architecture:** The existing `GET /subscription-status` probe becomes three-way (`active` | `expired` | `unavailable`) using a pure classifier. The layouts read the state and render the screen directly (an error boundary cannot read server error text in production). A small client component refreshes the route every 10 seconds. Storefront form routes and the admin API client map the same failures to a friendly message.

**Tech Stack:** Next.js server components, plain `.mjs` pure helpers tested with `node --test`, in `eandstravelmongolia` and `admin`. No digitalservice change.

**Spec:** `digitalservice/docs/superpowers/specs/2026-10-07-service-unavailable-screen-design.md`

## Global Constraints

- `unavailable` iff the probe fails with: (1) HTTP 503 whose error has `domain === 'TENANT'` and `code === 'FEATURE_UNAVAILABLE'`; (2) a network failure reaching digitalservice (connection refused, DNS, reset: a `TypeError` from `fetch`, or the dev-mode `ApiError` with status 0); (3) a timeout (`AbortError`/`TimeoutError`) after **8000 ms**; (4) HTTP 502, 503 or 504 with NO `code` and NO `domain` (a hosting proxy page, not the digitalservice JSON envelope). A 503 that has any other `domain` or `code` is NOT `unavailable`. A missing `TENANT_API_KEY` (plain `Error`) is NOT `unavailable`. Every other failure stays `'active'` (fail open). `unavailable` takes precedence over `expired`.
- The `unavailable` result is never cached; a successful answer keeps `revalidate: 60`. Next only caches 200 responses; the live check below must confirm recovery on the next refresh.
- Retry: `router.refresh()` every 10 seconds, paused while `document.visibilityState === 'hidden'`, plus a "Try again now" button. No countdown number.
- Constant text only (locale strings on the storefront, constants in the admin); no backend text is ever rendered; no `dangerouslySetInnerHTML`.
- Storefront strings in en, mn and ko (Mongolian and Korean written by Claude, native review flagged). English: title "We're waking our service up", body "This page will retry on its own in a few seconds.", button "Try again now", form message "Our service is waking up. Please try again in a minute." Admin: panel "The service is waking up. This page will retry on its own in a few seconds."; form message "The service is temporarily unavailable. Please try again in a minute."
- No new dependencies; `package.json` and lock files unchanged; secrets and tokens stay server-side; never log keys, tokens, bodies or error text. AGENTS.md in each repo applies (no `--no-verify`, nothing pushed, `.env*` values never printed).
- Verification per repo: `node --test` for the helpers (plus the existing `src/lib` tests and, in the storefront, `src/lib/translations/`), `npx tsc --noEmit`, `npx next build --webpack`, `npm run lint`. Known unrelated: storefront build stops at prerender when `TENANT_API_KEY` is unset; 2 pre-existing storefront tsc `RouteContext` errors; one admin lint error in `tours/page.tsx`. Report them separately.
- Branches stack: in each repo create `feat/service-unavailable` from the existing `feat/subscription-notice` branch, in its own git worktree (storefront `eandstravelmongolia-unavail`, admin `admin-unavail`, as siblings of the main checkouts). Link `node_modules` with a directory junction to the main checkout's `node_modules`; use `next build --webpack`. Never work in the user's own checkouts.

## Review Focus

- A TENANT 503 is `unavailable`; a 503 with a different domain/code is not; a 502/503/504 proxy page (no code, no domain) is; a plain config `Error` (missing key) is not; a timeout and a refused connection are.
- A recovered backend clears the screen on the next refresh (the `unavailable` outcome is never cached; the success path stays cached 60s).
- The screen replaces the whole storefront page (no half-rendered header/footer with an error); the admin keeps sidebar and topbar.
- Auto-retry pauses in a hidden tab and does not stack timers across re-renders or leak after unmount.
- Form submits: a 503 shows the localized waking-up message; a validation 400 and other failures show today's messages.
- Existing behaviors unchanged: the expired banner, 401/403 handling, the 402 friendly message in the admin.

---

### Task 1: Storefront probe, screen and retry (eandstravelmongolia)

**Files (storefront worktree `eandstravelmongolia-unavail`):**
- Create: `src/lib/service-state.mjs`, `src/lib/service-state.d.mts`, `src/lib/service-state.test.mjs`, `src/components/layout/ServiceUnavailable.tsx`, `src/components/layout/AutoRetry.tsx`
- Modify: `src/lib/api/subscription.ts`, `src/app/[locale]/layout.tsx`, `src/locales/en.json`, `src/locales/mn.json`, `src/locales/ko.json`, `src/types/i18n.ts`

**Interfaces:**
- Produces: `classifyStatusFailure(f: { status?: number; domain?: string; code?: string; networkError?: boolean; timedOut?: boolean }): 'unavailable' | 'active'` (rules from Global Constraints); `type ServiceState = 'active' | 'expired' | 'unavailable'`; `getSubscriptionState(): Promise<ServiceState>` (same name, three-way, never throws, 8000 ms timeout); `<ServiceUnavailable t={...} />` (server component, renders the localized title, body and the `<AutoRetry />` button); `<AutoRetry intervalMs?: number />` client component; locale keys `serviceUnavailable.title`, `.body`, `.retry`, `.formMessage` (the form key is used by Task 2).
- Consumes: the existing `ApiError` (`status`, `code`, `domain`) and `apiGet(path, searchParams, { signal, revalidate })` from `src/lib/api/client.ts`; the existing `parseSubscriptionState` in `src/lib/subscription-state.mjs`.

- [ ] **Step 0: Create the worktree** from the storefront repo: `git worktree add -b feat/service-unavailable ../eandstravelmongolia-unavail feat/subscription-notice`; junction `node_modules`.
- [ ] **Step 1: Write failing tests** `service-state.test.mjs` (node:test): `503 TENANT FEATURE_UNAVAILABLE is unavailable`; `503 with another domain is active`; `503 TENANT with another code is active`; `502/503/504 with no code and no domain is unavailable`; `500 with no code is active`; `404 is active`; `401 and 403 are active`; `networkError is unavailable`; `timedOut is unavailable`; `status 0 (dev-mode connect failure) is unavailable`; `no arguments / empty object is active`; plus a locale test that en, mn and ko each have non-empty strings at `serviceUnavailable.title`, `.body`, `.retry`, `.formMessage`.
- [ ] **Step 2: Run** `node --test src/lib/service-state.test.mjs`. Expected: FAIL, module not found.
- [ ] **Step 3: Implement** the classifier in `service-state.mjs` (+ `.d.mts`) and add the locale strings and the `serviceUnavailable` entry in `src/types/i18n.ts`.
- [ ] **Step 4: Run** the same test plus `node --test src/lib/translations/`. Expected: PASS.
- [ ] **Step 5: Rework `getSubscriptionState`** in `src/lib/api/subscription.ts`: timeout `8000`; on success `parseSubscriptionState(data)` (`'expired'`/`'active'`); in the catch, build the classifier input: if `err instanceof ApiError` use its `status`, `code`, `domain`; else `networkError = err instanceof TypeError`, `timedOut = err?.name === 'AbortError' || err?.name === 'TimeoutError'`; a plain `Error` (missing key) maps to `'active'`. Return `'unavailable'` only when the classifier says so. Never throw, never log.
- [ ] **Step 6: Implement the screen and retry.** `AutoRetry` (`'use client'`): `useRouter().refresh()` on a 10 s interval started in `useEffect`, cleared on unmount, skipped while the tab is hidden, plus a button labelled with `t.serviceUnavailable.retry`. `ServiceUnavailable`: centred full-page block, `role="status"`, existing site colour tokens, title and body from `t`. In `[locale]/layout.tsx`: when the state is `'unavailable'` render the `<html>`/`<body>` shell with `TranslationProvider` and ONLY `<ServiceUnavailable t={t} />` (no Header, `main`, Footer); `'expired'` keeps the existing notice above the normal site.
- [ ] **Step 7: Verify** `npx tsc --noEmit`, `npx next build --webpack`, `npm run lint`. Expected: no new errors.
- [ ] **Step 8: Commit** only touched files in the storefront worktree, message `feat: show a waking-up screen when the service is unavailable`.

---

### Task 2: Storefront form submits (eandstravelmongolia)

**Files (same worktree):**
- Modify: `src/lib/api/guard.ts`, `src/lib/api/guard-core.mjs`, `src/lib/api/guard-core.test.mjs`, the five route handlers under `src/app/api/*/route.ts` only if they bypass `upstreamFailure`, the forms `src/components/forms/BookingForm.tsx`, `ContactForm.tsx`, `AirportTransferForm.tsx`, `GuideApplicationForm.tsx`, `src/components/rentals/ReservationDialog.tsx`, and the newsletter component (find it with `grep -rn "api/newsletter" src`)

**Interfaces:**
- Consumes: `classifyStatusFailure` from Task 1; locale key `serviceUnavailable.formMessage`.
- Produces: `GENERIC_ERRORS.unavailable` (English fallback text) in `guard-core.mjs`; `upstreamFailure(route, err)` returns HTTP **503** with `{ error: GENERIC_ERRORS.unavailable }` for an unavailable-class failure (a TENANT `FEATURE_UNAVAILABLE` 503, a network `TypeError`, a timeout, or a code-less 502/503/504) and keeps today's mapping for everything else; `isUnavailableResponse(status: number): boolean` (true only for 503) used by the forms.

- [ ] **Step 1: Write failing tests** in `guard-core.test.mjs`: `GENERIC_ERRORS.unavailable is a non-empty string`; `isUnavailableResponse(503) is true`, `isUnavailableResponse(502|400|429|500|201) is false`.
- [ ] **Step 2: Run** `node --test src/lib/api/guard-core.test.mjs`. Expected: FAIL.
- [ ] **Step 3: Implement** the helper and `GENERIC_ERRORS.unavailable`; update `upstreamFailure` in `guard.ts` to classify the thrown error with `classifyStatusFailure` (same input mapping as Task 1 step 5) before the existing 4xx/502 branches, returning 503 for unavailable-class failures; log with the existing `redactForLog` path only (no new logging of error text).
- [ ] **Step 4: Update every form** that posts to these routes: when the response status passes `isUnavailableResponse`, show `t.serviceUnavailable.formMessage` in the existing error slot instead of `t.common.error_generic`; all other statuses keep today's text. Keep the forms' state machines otherwise unchanged.
- [ ] **Step 5: Run** `node --test src/lib/api/guard-core.test.mjs src/lib/service-state.test.mjs`, then `npx tsc --noEmit`, `npx next build --webpack`, `npm run lint`. Expected: no new errors.
- [ ] **Step 6: Commit** only touched files, message `feat: friendly message on form submits when the service is unavailable`.

---

### Task 3: Admin probe, notice and message (admin)

**Files (admin worktree `admin-unavail`):**
- Create: `src/lib/service-state.mjs`, `src/lib/service-state.d.mts`, `src/lib/service-state.test.mjs`, `src/components/admin/ServiceUnavailableNotice.tsx`, `src/components/admin/AutoRetry.tsx`
- Modify: `src/lib/subscription.mjs` (message constant only if cleaner there), `src/lib/api/client.ts`, `src/lib/data/subscription.ts`, `src/app/(dashboard)/layout.tsx`

**Interfaces:**
- Produces: `classifyStatusFailure` (identical contract to the storefront's, same tests); `SERVICE_UNAVAILABLE_MESSAGE` ("The service is temporarily unavailable. Please try again in a minute."); `ApiError` gains `domain?: string`; `request()` throws `ApiError(0, SERVICE_UNAVAILABLE_MESSAGE, 'SERVICE_UNAVAILABLE')` when `fetch` throws a network `TypeError` or an abort/timeout, and for a TENANT `FEATURE_UNAVAILABLE` 503 or a code-less 502/503/504 it throws an `ApiError` carrying `SERVICE_UNAVAILABLE_MESSAGE`, its real `status`, `domain` and `code`; `getSubscriptionState(token?: string): Promise<'active' | 'expired' | 'unavailable'>` (timeout 8000 ms; `'unavailable'` iff the thrown `ApiError` classifies as unavailable; a plain `Error` or anything else is `'active'`); `<ServiceUnavailableNotice />` and `<AutoRetry />`.
- Consumes: Task 3's own classifier; existing `friendlyApiMessage` and the 402 mapping (unchanged).

- [ ] **Step 0: Create the worktree** from the admin repo: `git worktree add -b feat/service-unavailable ../admin-unavail feat/subscription-notice`; junction `node_modules`.
- [ ] **Step 1: Write failing tests** `service-state.test.mjs` with exactly the classifier cases from Task 1 step 1 (no locale test), plus `SERVICE_UNAVAILABLE_MESSAGE` equals the constant above.
- [ ] **Step 2: Run** `node --test src/lib/service-state.test.mjs`. Expected: FAIL.
- [ ] **Step 3: Implement** `service-state.mjs` (+ `.d.mts`) and run the test again. Expected: PASS.
- [ ] **Step 4: Rework `request()` and the probe.** In `client.ts` keep `domain` on `ApiError`, wrap `fetch` so a network or abort failure throws the `SERVICE_UNAVAILABLE` `ApiError` described above (a plain missing-key `Error` from `apiKey()` stays a plain `Error`), and map the TENANT 503 and code-less 502/503/504 to `SERVICE_UNAVAILABLE_MESSAGE`; the 402 mapping stays as is. In `src/lib/data/subscription.ts` classify the caught error (an `ApiError` by `status`/`code`/`domain`, where `code === 'SERVICE_UNAVAILABLE'` or status 0 means unavailable) and return the three-way state; never throw or log.
- [ ] **Step 5: Implement the notice and layout.** `AutoRetry` (`'use client'`, same behaviour as the storefront's: 10 s `router.refresh()`, paused in a hidden tab, cleared on unmount, plus a "Try again now" button). `ServiceUnavailableNotice` shows the constant panel text with the existing admin notice styling and `role="status"`. In `(dashboard)/layout.tsx`: when the state is `'unavailable'` render the sidebar and topbar as today and put the notice (with `AutoRetry`) in the content area INSTEAD of `{children}`; `'expired'` keeps the banner above `{children}`.
- [ ] **Step 6: Verify** `node --test src/lib/*.test.mjs`, `npx tsc --noEmit`, `npx next build --webpack`, `npm run lint`. Expected: no new errors.
- [ ] **Step 7: Commit** only touched files in the admin worktree, message `feat(admin): waking-up notice and friendly message when the service is unavailable`.

---

## Final verification (controller, after all tasks)

Run the storefront and admin from the new worktrees against the local stack (digitalservice on 8081 returns the real TENANT 503 today). Confirm: the storefront shows the screen and retries every 10 s; the admin keeps its sidebar and shows the notice; a form submit shows the localized message; then (after the tenantcore service key is fixed) the screen clears on the next refresh. Report anything not exercised.

## Self-review notes

- Spec coverage: shared rule and classifier -> Tasks 1 and 3; storefront screen and retry -> Task 1; storefront forms -> Task 2; admin notice and message -> Task 3; live check -> Final verification; "never cached" -> Global Constraints and Task 1 step 5.
- The spec's `classifyStatusFailure({status, body, ...})` takes `code`/`domain` instead of `body`: "no code and no domain" is how a non-envelope proxy page is recognised, because both clients already parse the body before throwing.
