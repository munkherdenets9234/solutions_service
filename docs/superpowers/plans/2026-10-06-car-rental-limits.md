# Car Rental Limits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let tenant admins hide cars, choose driver/driverless per car, and set one driverless date range per car, with the public rental form and the backend both enforcing it. Plus an opaque off-home header, 3 home reviews, and a guide banner on the tours page.

**Architecture:** Pure rule functions in `digitalservice/internal/service` hold all validation (create defaults, update normalization, rental checks) and are unit-tested without Mongo. Repo/handler wiring is thin. Admin and site consume the new fields; the site keeps its own small pure helper for mode and date-bound logic.

**Tech Stack:** Go/Gin/MongoDB (`digitalservice`), Next.js 16 / React 19 (`admin`, `eandstravelmongolia`), `node --test` for `.mjs` helpers.

**Spec:** `digitalservice/docs/superpowers/specs/2026-10-06-car-rental-limits-design.md`

**Spec delta (found while planning):** admin reads cars from the public `GET /cars` and `GET /cars/:slug`, which will hide hidden cars. So Task 2 also adds `GET /admin/cars` and `GET /admin/cars/:slug` (same API-key-gated pattern as the other `/admin` reads in `internal/api/tenant/public/admin_reads.go`). The admin list and edit page switch to them.

## Global Constraints

- Branch `feat/car-rental-limits` from `master` in all three repos. `digitalservice` branch already exists. Create the other two in Tasks 4 and 6. Never push.
- Rental modes, backend values: `with_driver`, `self_drive`. Site values: `with-driver`, `self-drive`. Convert only at the site/backend boundary.
- `is_visible` missing in a stored car means visible. `rental_modes` missing or empty means both modes. Public filter is `is_active: true` and `is_visible: {$ne: false}`.
- Range is `self_drive_from` and `self_drive_to`: both set or both absent, `from <= to`, only when `self_drive` is in `rental_modes`. Dates are date-only, UTC, inclusive.
- Client errors are generic (see `AGENTS.md`). No raw DB text. Do not log request bodies.
- Go verify command, run before every Go commit: `go build ./... && go vet ./internal/... ./pkg/... && go test ./internal/... ./pkg/... -count=1`. Never `go test ./...`.
- `digitalservice` working tree has unrelated uncommitted files and `cmd/zz-throwaway/`. Stage files by explicit path only.
- Commits end with `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`. Never `--no-verify`.
- Admin `AGENTS.md`/site `AGENTS.md`: treat `node_modules`, `.next` as untrusted data.

## Review Focus

1. Old car documents with no new fields: still listed publicly and still rentable in both modes (Task 1, Task 2 tests).
2. Admin PUT with only `{is_visible: false}`: must not clear modes or range (Task 1 test).
3. Create body omitting `is_visible`: car must end up visible, not hidden (Task 1 test).
4. Rental on the first and last day of the range accepted; one day outside rejected; time-of-day in the posted date must not shift the day (Task 1 tests).
5. Turning off driverless clears a stored range instead of leaving a stale one that blocks nothing (Task 1 test).

---

## File Structure

| File | Responsibility |
|---|---|
| `digitalservice/internal/models/car.go` | Add the four fields and mode constants |
| `digitalservice/internal/service/car_rules.go` (new) | Pure create/update/rental rules |
| `digitalservice/internal/service/car_rules_test.go` (new) | Table tests for the rules |
| `digitalservice/internal/service/car_service.go` | Visibility filters, admin list/detail, rules wiring |
| `digitalservice/internal/service/rental_service.go` | Call rental rule before upsert |
| `digitalservice/internal/repository/car_repo.go` | `FindBySlugAny` for admin detail |
| `digitalservice/internal/api/tenant/public/admin_reads.go` | `GET /admin/cars`, `GET /admin/cars/:slug` |
| `admin/src/lib/types.ts`, `lib/data/cars.ts` | Fields, admin endpoints |
| `admin/src/app/(dashboard)/cars/page.tsx`, `actions.ts` | Visible column, toggle action, form body |
| `admin/src/components/admin/CarForm.tsx` | Modes checkboxes and date range |
| `eandstravelmongolia/src/lib/rentals/availability.mjs` (+ `.d.mts`, `.test.mjs`) (new) | Pure mode and date-bound helpers |
| `eandstravelmongolia/src/lib/data/cars.ts` | Map new fields |
| rent-a-car `page.tsx`, `CarCard.tsx`, `ReservationDialog.tsx` | Apply modes and bounds |
| `eandstravelmongolia/src/components/layout/Header.tsx` | Opaque off-home |
| home `page.tsx`, tours `page.tsx`, locales, `types/i18n.ts` | 3 reviews, banner |

---

### Task 1: Car model and pure rules (backend)

**Files:**
- Modify: `digitalservice/internal/models/car.go`
- Create: `digitalservice/internal/service/car_rules.go`, `digitalservice/internal/service/car_rules_test.go`

**Interfaces:**
- Produces (`models`): `const CarModeWithDriver = "with_driver"`, `CarModeSelfDrive = "self_drive"`. Fields on `Car`: `IsVisible *bool` (`bson:"is_visible" json:"is_visible"`; a pointer so an absent stored value differs from `false`, see the note below), `RentalModes []string` (`rental_modes`), `SelfDriveFrom *time.Time` (`self_drive_from`, `omitempty` off, so null round-trips), `SelfDriveTo *time.Time`.
- Produces (`service`):
  - `func CarModes(c *models.Car) []string` — stored modes, or both when empty.
  - `func CarVisible(c *models.Car) bool` — see "missing" note below.
  - `func PrepareCarCreate(c *models.Car) error` — sets `IsVisible = true`, defaults modes to both, validates modes and range.
  - `func NormalizeCarUpdate(existing *models.Car, update bson.M) error` — mutates `update` in place.
  - `func ValidateRentalForCar(c *models.Car, rt *models.Rental) error` — returns `apierr` errors.
- Note on "missing": `bool` decodes absent as `false`, so `CarVisible` cannot tell absent from hidden. Make the stored field a `*bool` in the model (`IsVisible *bool`) and `CarVisible` returns `c.IsVisible == nil || *c.IsVisible`. `PrepareCarCreate` sets it to a pointer to `true`. Keep the json name `is_visible`. Admin and site read it as `boolean | undefined`.

- [ ] **Step 1: Write failing tests in `car_rules_test.go`.** Each is a table test, package `service`. Use helper `d(s string) *time.Time` parsing `2006-01-02` UTC. Cases:
  - `TestCarVisible`: nil pointer → true; `&true` → true; `&false` → false.
  - `TestCarModes`: empty → `[with_driver, self_drive]`; `[self_drive]` → `[self_drive]`.
  - `TestPrepareCarCreate`: omitted visibility becomes visible (focus 3); empty modes default to both; modes `["boat"]` → BadRequest; only `self_drive_from` set → BadRequest; `from` after `to` → BadRequest; range with modes `[with_driver]` → range cleared (nil both).
  - `TestNormalizeCarUpdate`: `{is_visible:false}` leaves `rental_modes` and range keys absent from the map (focus 2); `{rental_modes:["with_driver"]}` on a car with a stored range sets `self_drive_from` and `self_drive_to` to `nil` in the map (focus 5); `{self_drive_from:"2026-07-01", self_drive_to:"2026-08-31"}` on a self-drive-capable car converts to `time.Time` UTC midnight; `{self_drive_from:"2026-07-01"}` on a car without a stored `to` → BadRequest; range where `to < from` → BadRequest; `{is_visible:"yes"}` → BadRequest; `{rental_modes:[]}` → BadRequest; a RFC3339 string with a non-UTC offset is reduced to its date (`2026-07-01T23:30:00-05:00` → `2026-07-01`).
  - `TestValidateRentalForCar` (car self-drive range `2026-07-01..2026-08-31`, mode `self_drive`): pickup `2026-07-01`, return `2026-07-03` → nil; pickup `2026-08-30`, return `2026-08-31` → nil (focus 4); pickup `2026-06-30` → BadRequest; return `2026-09-01` → BadRequest; return before pickup → BadRequest; mode `with_driver` on a car whose modes are `[self_drive]` → BadRequest; hidden car → NotFound; `IsActive=false` → NotFound; car with no `IsVisible` and empty modes accepts both modes with any dates (focus 1); `with_driver` rental with dates outside the self-drive range → nil (range applies to self-drive only).

- [ ] **Step 2: Run** `go test ./internal/service -run 'TestCar|TestPrepareCar|TestNormalizeCar|TestValidateRental' -count=1`. Expected: FAIL, undefined symbols.

- [ ] **Step 3: Implement the model fields and the five functions** with the signatures above. Date helper: truncate any `time.Time` to `time.Date(y, m, d, 0,0,0,0, time.UTC)` using the time's own UTC date after `.UTC()`. For the offset case in the test, parse RFC3339, take the literal calendar date in the string's own offset (do not convert to UTC), since the admin picked a date. Accepted update date inputs: `nil`/`""` (clear), `"2006-01-02"`, RFC3339 string. Errors: `apierr.BadRequest("invalid car fields")` for every update validation failure; `apierr.BadRequest("rental mode not available")` and `apierr.BadRequest("rental dates not available")` for rental failures; `apierr.NotFound("car not found")` for inactive/hidden.

- [ ] **Step 4: Run** the Step 2 command. Expected: PASS.

- [ ] **Step 5: Commit** `git add internal/models/car.go internal/service/car_rules.go internal/service/car_rules_test.go && git commit -m "feat(cars): rental modes, visibility and driverless range rules"`.

---

### Task 2: Backend wiring (filters, admin reads, rental check)

**Files:**
- Modify: `digitalservice/internal/service/car_service.go`, `internal/service/rental_service.go`, `internal/repository/car_repo.go`, `internal/api/tenant/public/admin_reads.go`
- Test: `digitalservice/internal/service/car_rules_test.go` (append)

**Interfaces:**
- Consumes: Task 1 functions.
- Produces:
  - `func publicCarFilter(f ListCarsFilter) bson.M` (service, unexported) — `{"is_active": true, "is_visible": {"$ne": false}}` plus `type`/`fuel` when set.
  - `func adminCarFilter() bson.M` — `{"is_active": true}`.
  - `func (s *CarService) ListAdmin(ctx, tenantID primitive.ObjectID, page, limit int) ([]*models.Car, int64, error)`.
  - `func (s *CarService) GetBySlugAdmin(ctx, tenantID primitive.ObjectID, slug string) (*models.Car, error)`.
  - `func (r *CarRepo) FindBySlugAny(ctx, tenantID primitive.ObjectID, slug string) (*models.Car, error)` — `is_active: true` only, no visibility filter.
  - `GET /admin/cars` and `GET /admin/cars/:slug` on the admin reads controller (needs a `car *service.CarService` field and `d.Car` in `registerAdminReads`).

- [ ] **Step 1: Write failing tests**: `TestPublicCarFilter` asserts the exact bson map for empty and for `Type:"suv"`; `TestAdminCarFilter` asserts it has no `is_visible` key.
- [ ] **Step 2: Run** `go test ./internal/service -run 'TestPublicCarFilter|TestAdminCarFilter' -count=1`. Expected: FAIL.
- [ ] **Step 3: Implement.** `List` uses `publicCarFilter`. `repo.FindBySlug` adds `"is_visible": bson.M{"$ne": false}`. `Create` calls `PrepareCarCreate` after the slug check (remove the old `c.IsActive = true` only if `PrepareCarCreate` does not set it; keep it set). `Update` calls `NormalizeCarUpdate(existing, update)` using the document it already loads via `FindByID`, before `repo.Update`. `RentalService.Create` replaces the existence check result with the loaded car and calls `ValidateRentalForCar(car, &input.Rental)` before the customer upsert, so a rejected rental never creates a customer. Add the two admin controller methods mirroring `ListRentals`/`GetRental` shape (use `apictx.Page(c, 20)`, `response.List`, `response.OK`), register both routes after `/rentals` lines.
- [ ] **Step 4: Run** the Go verify command from Global Constraints. Expected: all PASS, build clean.
- [ ] **Step 5: Commit** with explicit paths for the four modified files and the test file: `git commit -m "feat(cars): hide hidden cars publicly, admin car reads, rental checks"`.
- [ ] **Step 6: Live check** if a local backend with Mongo is available (`PORT=8080`, see memory note on no-Docker): create a car, `PUT {is_visible:false}`, confirm public `GET /cars` omits it and `GET /admin/cars` lists it, and `POST /rentals` for it returns an error. If Mongo is unavailable, say so in the report; do not claim it verified.

---

### Task 3: Admin UI

**Files:**
- Modify: `admin/src/lib/types.ts` (`Car` interface, line ~98), `admin/src/lib/data/cars.ts`, `admin/src/app/(dashboard)/cars/page.tsx`, `admin/src/app/(dashboard)/cars/actions.ts`, `admin/src/components/admin/CarForm.tsx`

**Interfaces:**
- Consumes: `GET /admin/cars`, `GET /admin/cars/:slug`, `PUT /admin/cars/:id` accepting `is_visible`, `rental_modes`, `self_drive_from`, `self_drive_to`.
- Produces: `Car` gains `is_visible?: boolean`, `rental_modes?: ('with_driver'|'self_drive')[]`, `self_drive_from?: string | null`, `self_drive_to?: string | null`. `export async function setCarVisibilityAction(id: string, visible: boolean): Promise<void>` in `actions.ts`.

- [ ] **Step 1: Create branch** `git -C admin switch master && git -C admin switch -c feat/car-rental-limits`. Check `git status` is clean first; if `admin` has uncommitted work, stop and report.
- [ ] **Step 2: Types and data.** Add the four fields. `listCars` and `getCarBySlug` call `/admin/cars` and `/admin/cars/${slug}`. `buildCarNameMap` keeps working through `listCars`.
- [ ] **Step 3: `setCarVisibilityAction`.** Server action: `requireToken`, `apiPut` to `/admin/cars/${id}` with `{ is_visible: visible }`, `revalidatePath('/cars')`. No redirect.
- [ ] **Step 4: List column.** Add a `Visible` column rendering a small `<form action={setCarVisibilityAction.bind(null, c.id, !(c.is_visible !== false))}>` with a button labelled "Visible" or "Hidden". Hidden rows get reduced opacity through the column render of Name (`opacity-50` on the name span). Missing `is_visible` shows as Visible.
- [ ] **Step 5: Form body and form.** `bodyFromForm` adds `rental_modes` from checkboxes named `rental_modes` (`formData.getAll`), and `self_drive_from`/`self_drive_to` as the date string or `null` when empty. `CarForm` adds two checkboxes (With driver, Driverless; both checked by default for new cars and for cars with no stored modes) and, as a client-state-driven block shown only when Driverless is checked, two `type="date"` inputs. Unchecking Driverless hides the inputs and sends `null` for both. If no checkbox is checked, the server returns an error that the existing `state.error` display shows.
- [ ] **Step 6: Verify.** `cd admin && npx tsc --noEmit && npm run lint`. Expected: no new errors. Then start the admin dev server through `preview_start` (add `.claude/launch.json` entry only if none exists), with the backend from Task 2 running, and confirm in the browser: toggling Visible updates the row, edit form saves modes and range, a range with `to` before `from` shows the server error. If the backend cannot run, report that the UI was only type-checked.
- [ ] **Step 7: Commit** `git -C admin add <the five files> && git -C admin commit -m "feat(admin): car visibility toggle, rental modes and driverless range"`.

---

### Task 4: Site data and rent-a-car limits

**Files:**
- Create: `eandstravelmongolia/src/lib/rentals/availability.mjs`, `availability.d.mts`, `availability.test.mjs`
- Modify: `eandstravelmongolia/src/lib/data/cars.ts`, `src/app/[locale]/rent-a-car/page.tsx`, `src/components/rentals/CarCard.tsx`, `src/components/rentals/ReservationDialog.tsx`

**Interfaces:**
- Produces (`availability.mjs`, site mode names):
  - `export function modesOf(car: { rentalModes?: string[] }): string[]` — stored list or `['with-driver','self-drive']` when empty or absent.
  - `export function supportsMode(car, mode: string): boolean`
  - `export function pickMode(requested: string | undefined, cars): 'with-driver' | 'self-drive'` — `requested` if some car supports it; otherwise the first mode any car supports; default `'with-driver'` when there are no cars.
  - `export function dateBounds(car: { selfDriveFrom?: string, selfDriveTo?: string }, mode: string): { min?: string, max?: string }` — bounds only for `'self-drive'` with both dates set, else `{}`.
  - `export function withinBounds(bounds, pickup: string, ret: string): boolean`
- `Car` (site) gains `rentalModes: string[]`, `selfDriveFrom?: string`, `selfDriveTo?: string`, `visible: boolean`.

- [ ] **Step 1: Create branch** `git -C eandstravelmongolia switch master && git -C eandstravelmongolia switch -c feat/car-rental-limits`. The tree currently has an uncommitted change to `scripts/export-translations.mjs`; leave it unstaged.
- [ ] **Step 2: Write failing tests** in `availability.test.mjs` (`node:test`). Assert: `modesOf({})` and `modesOf({rentalModes:[]})` both return both modes; `supportsMode({rentalModes:['self-drive']}, 'with-driver')` is false; `pickMode('self-drive', [{rentalModes:['with-driver']}])` returns `'with-driver'`; `pickMode(undefined, [])` returns `'with-driver'`; `dateBounds({selfDriveFrom:'2026-07-01', selfDriveTo:'2026-08-31'}, 'self-drive')` deep-equals `{min:'2026-07-01', max:'2026-08-31'}`; same car with `'with-driver'` returns `{}`; `withinBounds` accepts both endpoints and rejects `2026-06-30` and `2026-09-01`; empty bounds always accepts.
- [ ] **Step 3: Run** `node --test src/lib/rentals/`. Expected: FAIL (module missing).
- [ ] **Step 4: Implement** the helper and its `.d.mts` (follow `src/lib/api/guard-core.d.mts` style). Run again. Expected: PASS.
- [ ] **Step 5: Map the fields in `data/cars.ts`.** `BackendCar` adds `is_visible?: boolean`, `rental_modes?: string[]`, `self_drive_from?: string | null`, `self_drive_to?: string | null`. `mapCar` converts `with_driver`→`with-driver`, `self_drive`→`self-drive`, trims dates to the first 10 characters, and sets `visible: c.is_visible !== false`. `getAllCars` also drops cars where `visible` is false (defence in depth; backend already filters).
- [ ] **Step 6: Page and card.** In `rent-a-car/page.tsx`, compute `mode = pickMode(one(sp.mode), allCars)` where `allCars` is `getCars({})`; show the toggle link for a mode only when some car supports it; list `cars.filter(c => supportsMode(c, mode))` after the type filter. `CarCard` receives `mode` as before and needs no new prop beyond the car; it passes `car` to the dialog.
- [ ] **Step 7: Dialog bounds.** In `ReservationDialog`, `const bounds = dateBounds(car, mode)`; apply `min`/`max` to both date inputs; before `fetch`, if `!withinBounds(bounds, pickup, ret)` set `status` to `'error'` and return. Do not change the posted payload.
- [ ] **Step 8: Verify.** `npx tsc --noEmit && npm run lint && node --test src/lib/rentals/`. Expected: pass. Then in the browser against the local backend: a driverless car with a range restricts the pickers; a with-driver-only car disappears from the driverless view; posting a date outside the range directly with `curl` to `/api/rentals` returns the generic error (server check from Task 2). If no backend, report type/lint/unit only.
- [ ] **Step 9: Commit** explicit paths, `git -C eandstravelmongolia commit -m "feat(rent-a-car): respect car rental modes and driverless date range"`.

---

### Task 5: Header, home reviews, guide banner (site)

**Files:**
- Modify: `eandstravelmongolia/src/components/layout/Header.tsx` (line 65-67 class), `src/app/[locale]/page.tsx` (line 45), `src/app/[locale]/tours/page.tsx`, `src/locales/en.json`, `mn.json`, `ko.json`, `src/types/i18n.ts`
- Create: `eandstravelmongolia/src/components/tours/GuideBanner.tsx`

**Interfaces:**
- Produces: `Translation.guideBanner: { eyebrow: string; heading: string; text: string; cta: string }` in `i18n.ts`; `GuideBanner({ locale }: { locale: Locale })` server-compatible component linking to `/${locale}/careers`.

- [ ] **Step 1: Header.** Change the non-overlay class from `bg-cream/95 backdrop-blur-md border-b border-border` to `bg-cream border-b border-border` so page content never shows through. Home overlay behavior unchanged. The mobile menu panel (`bg-cream/98`) becomes `bg-cream` too.
- [ ] **Step 2: Reviews.** In `page.tsx`, `reviewItems` is built from `reviews.slice(0, 3)`. `ReviewsSection` computes its average and count from what it is given, so it now reflects those 3. If the product wants the count and average of all reviews, say so in the report instead of changing it; default is the slice, as the spec says "limited to 3".
- [ ] **Step 3: i18n.** Add `guideBanner` to `types/i18n.ts` after `rentACar`'s sibling blocks (add it at the end of the interface to keep the later merge with `feat/guide-recruitment` conflict small), and to `en.json`, `mn.json`, `ko.json` at the end of each file. Copy: en eyebrow "Careers", heading "Become a guide with E & S Discovery", text "We are recruiting English-, Korean- and Mongolian-speaking guides. Apply in a few minutes.", cta "See open roles". Provide Mongolian and Korean translations of the same four strings.
- [ ] **Step 4: Banner.** `GuideBanner.tsx` renders a full-width strip using the site's existing tokens (`bg-ink text-cream`, `container mx-auto px-6 sm:px-14`, button style matching `rent-a-car` CTA). It reads copy through `getTranslation(locale)` (async server component) like other server pages. Render it in `tours/page.tsx` immediately above the journey list grid.
- [ ] **Step 5: Verify.** `npx tsc --noEmit && npm run lint`. In the browser: scroll a non-home page and confirm the header is opaque with nothing visible through it; home still transparent at the top and solid after 60px; home shows at most 3 review cards; tours page shows the banner above the list in en, mn and ko; the CTA targets `/{locale}/careers` (404 expected until `feat/guide-recruitment` merges; say so).
- [ ] **Step 6: Commit** as two commits: `fix(header): opaque background off the home page` and `feat: limit home reviews to 3 and add guide recruitment banner`. Explicit paths only.

---

### Task 6: Whole-branch check and handoff

- [ ] **Step 1:** Run the Go verify command in `digitalservice`; `npx tsc --noEmit && npm run lint` in `admin` and `eandstravelmongolia`; `node --test src/lib/rentals/ src/lib/translations/ src/lib/api/` in the site.
- [ ] **Step 2:** `git log --oneline master..HEAD` in each repo. Report commits and list what was and was not verified live. Do not push or open PRs.
- [ ] **Step 3:** Note for the PR description: the new `/admin/cars` reads are API-key-gated like the other `/admin` reads (written reason required by `AGENTS.md`), and the banner link depends on `feat/guide-recruitment`.
