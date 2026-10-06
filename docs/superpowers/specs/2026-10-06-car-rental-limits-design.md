# Car rental limits — design

Date: 2026-10-06. Repos: `digitalservice` (backend), `admin`, `eandstravelmongolia` (site). Branch `feat/car-rental-limits` from `master` in each. Nothing pushed.

## Intent

The travel agency wants control over what the public car rental form offers. An admin can hide a car, say whether it is rented with a driver, driverless, or both, and set the dates a car is available driverless. The public form obeys, and the server enforces the same rules.

Same release, two small site items: an opaque header off the home page, and tidy home/tours content.

## Backend (`digitalservice`)

`models.Car` adds:

| Field | bson/json | Meaning |
|---|---|---|
| `IsVisible` | `is_visible` | Shown on the public site. Missing in old documents = visible. |
| `RentalModes` | `rental_modes` | Subset of `with_driver`, `self_drive`. Missing or empty in old documents = both. |
| `SelfDriveFrom`, `SelfDriveTo` | `self_drive_from`, `self_drive_to` | One date range per car, `*time.Time`. Both set or both absent. |

Rules:
- Public list and `GetBySlug` filter `is_active: true` and `is_visible: {$ne: false}`.
- Admin list filters `is_active: true` only, so hidden cars stay listed. New `ListAdmin` service/repo path; public `List` is unchanged except for the visibility filter.
- `Create` sets `IsVisible = true` and defaults `RentalModes` to both. The body binds straight into `Car`, so an omitted `false` must not hide a new car.
- `Update` accepts `is_visible`, `rental_modes`, `self_drive_from`, `self_drive_to`. Validation: modes drawn from the two values and not empty; `from <= to`; range only with `self_drive` in modes (otherwise cleared); both dates or neither. Check `stripProtectedFields` does not drop `is_visible`.
- `RentalService.Create` rejects, with generic client errors: car not found or not active, car not visible, mode not in the car's `rental_modes`, self-drive pickup or return outside the car's range (when a range is set), return before pickup. Prices and tenant stay server-side as today.
- `Delete` stays a soft delete (`is_active=false`). Unchanged.

Tests: table tests for modes/range validation on update and for each rental rejection; public filter treats missing `is_visible` as visible.

## Admin (`admin`)

- `lib/types.ts` `Car` gets the four fields. Cars list uses an admin data call that includes hidden cars.
- Cars list: new Visible column with a toggle. A server action sends `is_visible` through `PUT /admin/cars/:id`. Hidden rows are dimmed.
- `CarForm`: rental option checkboxes (with driver, driverless) and, when driverless is on, from/to date inputs. Added to both `new` and `[slug]/edit`.
- Delete button unchanged.

## Site (`eandstravelmongolia`)

- Rent-a-car page: the mode toggle shows only modes some visible car supports. Cars not supporting the selected mode are hidden. Car card shows only supported modes.
- `ReservationDialog`: for driverless, pickup and return inputs get `min` and `max` from the car range. The client check is a convenience; the backend rejects out-of-range dates.
- `/api/rentals` validates and forwards as today; backend errors map to the existing generic message.
- Header: solid background (no translucency) on non-home pages. Home keeps transparent-to-solid on scroll.
- Home: reviews limited to 3.
- Tours list: guide recruitment banner above the journey list, linking to `/{locale}/careers`. That page exists only on `feat/guide-recruitment` and is not merged. Until it merges, the link 404s. Banner text goes through the locale files (en, mn, ko).

Tests: `node --test` for any new pure helpers (mode filtering, date bounds). Browser check of rent-a-car, header and home against a local backend.

## Out of scope

Summary strip, popup form, multiple ranges per car, blocking dates covered by existing rentals, removing Delete.

## Risks

- `digitalservice` has uncommitted translations work on `master`. The new branch carries it. Commit only this feature's files.
- `admin` is on `feat/guide-recruitment` today. Branch from `master`.
