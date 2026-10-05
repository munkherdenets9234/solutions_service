# Guide recruitment: design

Date: 2026-10-05. Branch: `feat/guide-recruitment` in `digitalservice`, `admin` and `eandstravelmongolia`.

## Purpose

The travel agency wants to collect applications from potential tour guides for next summer. Applicants fill a public form on the E&S site. Staff review the applications in the travel admin dashboard. The form follows the agency's own eight-section list (personal, languages, guide experience, Mongolia knowledge, driving, availability, references, documents), written in Mongolian with an English version.

Success means: a visitor submits the form with documents; staff open the dashboard, find the application, read every field, open every document, set a status and leave notes; and anyone can see who changed what, and when.

## Decisions already made

| Question | Decision |
|---|---|
| Who applies | Any visitor, from a public page on the E&S site. A `/careers` hiring page lists openings; the first opening is "Summer guide 2027". |
| Documents | JPEG, PNG and PDF. Stored privately. Staff open them through short-lived signed links. |
| Storage | Cloudinary, delivery type `authenticated`. The operator supplies the credential. |
| Workflow | Status, internal notes and a history timeline. |
| Not in scope | Emails to applicants, edit after submit, delete, export, multiple openings management, applicant accounts. |

## Architecture

```
E&S site (eandstravelmongolia)    digitalservice (Go/Gin/Mongo)               admin (Next.js)
/careers
/careers/guide  --multipart-->  site route  --multipart-->  POST /guide-applications
                                (holds tenant key)           (public, rate limited, outside
                                                              the subscription gate)
                                                            GET   /admin/guide-applications
                                                            GET   /admin/guide-applications/:id
                                                            PATCH /admin/guide-applications/:id/status
                                                            POST  /admin/guide-applications/:id/notes
                                                            GET   /admin/guide-applications/:id/files/:fileId
```

The browser never holds the tenant API key. The site route forwards to the backend with the server-only `TENANT_API_KEY`, the same pattern as `src/app/api/contact/route.ts`.

The public route sits beside the other lead routes in `internal/api/tenant/public/leads.go`, outside the subscription gate. A tenant with a lapsed subscription should not silently drop applicants. Admin routes sit behind the normal gate and admin auth, like `contact-messages`.

## Backend (`digitalservice`)

### Model `GuideApplication` (`internal/models/guide_application.go`)

Collection `guide_applications`, tenant scoped (`tenant_id`), as `ContactMessage` is.

- `season` string, fixed `"summer-2027"` for now. The field exists so next year's openings do not need a migration.
- `locale` `"mn" | "en"`, the form language used.
- `personal`: `full_name` (required), `nickname`, `birth_date` (date), `gender` (`male|female|other|undisclosed`), `phone` (required), `email` (required), `address`, `emergency_contact` {`name`, `phone`}.
- `languages[]`: `{language, level}`. `language` is one of `mn, en, ko, zh, ja, ru, fr, es, other`; `other_name` set only when `other`. Levels: Mongolian `native|good|intermediate`; all others `native|fluent|intermediate|basic`.
- `experience`: `years` (int), `previous_companies` string, `tour_types[]` from `private, group, vip, adventure_4x4, cultural, hiking_trekking, festival, business_corporate`, `main_directions` string, `largest_group` int.
- `regions[]` from `gobi, central, khuvsgul, western, eastern, ulaanbaatar_terelj, other`; `regions_other` string.
- `driving`: `has_license` bool, `license_class`, `years_driving`, `can_drive_4x4` bool, `long_distance` bool, `has_own_vehicle` bool, `vehicles` string. Fields after `has_license` are only accepted when `has_license` is true.
- `availability`: `months[]` (1 to 12), `days` string, `trip_lengths[]` from `d1_3, d4_7, d8_14, d15_plus`, `full_season` bool, `booked_trips` string.
- `references[]`, at most 5: `{name, position, contact}`.
- `files[]`: `{id, kind, public_id, mime, size, original_name}`. `kind` from `photo, id_card, driver_license, guide_certificate, cv, first_aid`.
- `status` from `new, reviewing, shortlisted, rejected, hired`; default `new`.
- `events[]`, append only: `{type: status|note, at, user_id, user_name, from, to, text}`.
- `consent_at` time. Required; the form refuses to submit without the consent box.
- `created_at`, `updated_at`.

`user_name` is stored in the event at write time. The timeline therefore stays readable if the staff user is later renamed or removed.

### Validation (service layer, authority)

- Required: `full_name`, `phone`, `email`, `birth_date`, Mongolian level, English level, at least one region, `has_license`, at least one month, `consent_at`, and a `cv` file.
- Email format; phone has 7 to 15 digits after stripping separators; applicant is at least 18 on the submit date.
- All enum values checked against the lists above. Unknown value returns `VALIDATION_FAILED` and names the field.
- At most 5 references, at most 8 files, at most 1 file per `kind` except `guide_certificate` (up to 3).
- Free text capped at 2000 characters per field.
- The same email applying again within 24 hours for the same tenant and season is rejected with a conflict error. After 24 hours it is allowed.

### Private files (`internal/service/private_file_service.go`)

A new `PrivateFileService`. The existing `UploadService` is not touched: it accepts images only and returns public URLs.

- Config: reuses `CLOUDINARY_URL`. The 2026-10-05 check showed its key and secret can already upload with `type=authenticated`, so no new variable is required. An optional `CLOUDINARY_PRIVATE_URL` overrides it if the agency later wants a separate Cloudinary account. If neither is set the service is nil and reports unavailable, like `UploadService`. `.env.example` gets the variable name only.
- Type is decided by sniffing the first bytes, never by client content type. Allowed: `image/jpeg`, `image/png`, `application/pdf`. Size cap is `UPLOAD_MAX_BYTES` (10 MiB default), enforced on the stream with the same one-extra-byte technique `UploadService` uses.
- Upload: `type: authenticated`, folder `tenants/<tenant_id>/guide-applications`, random UUID name. The destination never comes from the request. PDFs go as resource type `raw`; images as `image`.
- Download: `DownloadURL(publicID, resourceType, ttl)` returns a signed Cloudinary private-download URL (`https://api.cloudinary.com/v1_1/<cloud>/<resource_type>/download`, parameters `public_id`, `type=authenticated`, `timestamp`, `expires_at`, `attachment`, signed with the API secret) valid 5 minutes. The CDN delivery URL is not used: the 2026-10-05 credential check showed the account refuses CDN delivery of authenticated raw files (`401 deny or ACL failure`), while the private-download endpoint returned the PDF. The admin file route checks tenant ownership of the application and the file id before signing.
- Cleanup: `Delete(publicID)` is used when a submit fails partway.

### Submit flow

`POST /guide-applications` accepts `multipart/form-data`: one part `data` holding the JSON document, plus file parts named `file_<kind>` (and `file_guide_certificate_1..3`).

1. Rate limit per client IP, using the existing `middleware.RateLimiter.Limit`.
2. Honeypot field `website`. If non-empty the server answers `201` with a fake id and stores nothing.
3. Validate the JSON. Validate each file (type, size, kind rules) before any upload starts.
4. Upload files one by one. On any failure, delete the files already uploaded and return the error. Nothing is saved.
5. Insert the document with `status: new` and an empty `events` list (no staff actor yet).
6. If the insert fails, delete the uploaded files.
7. Return `201` with `{id, confirmation_id}` where `confirmation_id` is `GA-` plus the last six characters of the id, uppercase.

### Admin routes

- `GET /admin/guide-applications?page&limit&status&q&language&region`. `q` matches name, phone and email, case-insensitive. Response also carries counts per status for the filter tabs. Default sort newest first.
- `GET /admin/guide-applications/:id` returns the full document.
- `PATCH /admin/guide-applications/:id/status` with `{status}`. Value checked against the list. Appends a status event with `from`/`to`. The acting user comes from the admin token, never from the body. Setting the same status again returns success and writes no event.
- `POST /admin/guide-applications/:id/notes` with `{text}`, 1 to 2000 characters. Appends a note event. Notes are never edited or deleted.
- `GET /admin/guide-applications/:id/files/:fileId` returns `{url, expires_at}`.

Concurrent changes: last write wins on `status`. Both events stay in `events[]`, so nothing is lost. Appends use Mongo `$push`, not read-modify-write.

Indexes (`internal/repository/indexes.go`): `(tenant_id, created_at desc)`, `(tenant_id, status, created_at desc)`, `(tenant_id, season, personal.email, created_at)` for the duplicate check.

### Privacy and logging

Application bodies and file names are not written to logs. Error responses carry the field name, not the submitted value. Signed links expire in 5 minutes. A consent notice (Mongolian and English) states that ID scans are stored privately and who can see them.

## Site (`eandstravelmongolia`)

- `/careers`: hiring page, one opening card, "Apply" button.
- `/careers/guide`: the form. One page, eight sections, section list as navigation. Mongolian by default, English through the existing locale switch. Strings go in `src/locales/mn.json` and `en.json` under a new `careers` namespace, through the existing translation provider.
- Conditional fields: license class, years, 4x4 and long-distance only when the license answer is yes. "Other" text fields only when "other" is ticked.
- Files are chosen in the form and sent with the submit. Nothing uploads earlier, so an abandoned form leaves no files. Each file row shows name, size and a remove button.
- Client validation mirrors the server rules for fast feedback. The server remains the authority and the form shows server field errors next to the field.
- On a failed submit the form keeps all entered data and selected files.
- Success screen shows the confirmation id.
- `src/app/api/guide-applications/route.ts` forwards the multipart body to the backend with the server-only tenant key. It enforces a body size ceiling before forwarding.
- Honeypot field is rendered off-screen and not focusable.

## Admin (`admin`)

- Nav entry "Guide applications" in `src/lib/nav.ts`, page `(dashboard)/guide-applications/page.tsx`, data layer `src/lib/data/guide-applications.ts`, following `contact-messages`.
- List: status badge, name, phone, languages summary, regions, created date. Tabs per status with counts. Search box. Filters for language and region.
- Detail `(dashboard)/guide-applications/[id]/page.tsx`: all eight sections read-only, file list (click fetches a signed URL and opens it in a new tab), status dropdown, note box, timeline of events newest first.
- Server actions in `actions.ts` for status and note, like `contact-messages/actions.ts`.
- Types go in `src/lib/types.ts`.

## Error handling summary

| Case | Result |
|---|---|
| Missing or invalid field | 400 `VALIDATION_FAILED`, names the field; form shows it inline |
| Wrong file type or too big | 422, names the file; nothing saved |
| Duplicate within 24 h | 409; form says an application with this email was already received |
| Rate limit | 429; form asks to retry later |
| Private file storage not configured | 503 `FEATURE_UNAVAILABLE` on submit; nothing saved, so no silent partial applications |
| Cloudinary upload failure | 502; already uploaded files deleted; nothing saved |
| Admin file link for another tenant's file | 404 |

## Testing

- Go service tests: every validation rule, enum check, age check, duplicate window, tenant scoping on list/get/status/notes/files, event append on status and note, no event on unchanged status. File sniffing: renamed `.exe` rejected, PDF accepted, size cap exactly at and one byte over. Cleanup: fake uploader fails on the third file, the first two are deleted, no row saved. Insert failure deletes uploaded files.
- Go controller tests: multipart parsing, honeypot returns fake success with no row, rate limit, admin auth required, acting user taken from the token.
- Site: unit tests for the validation module and the conditional-field logic; the route test checks the tenant key is added server-side and not returned.
- Admin: list and detail render against fixtures; status and note actions call the right endpoints.
- No test calls Cloudinary. The credential check (upload as authenticated raw, unsigned fetch refused with 401, private-download URL returns a valid PDF, delete) was run by hand on 2026-10-05 and passed; it is repeated once before merge as a manual step. Fallback if Cloudinary ever stops working for this: store files in MongoDB GridFS behind the same `PrivateFileService` interface; no other code changes.

## Delivery

Three repositories, one branch each, named `feat/guide-recruitment`. Build order: backend first (the other two depend on its routes), then admin, then site. `digitalservice` branches from `refactor/backend-core`, so its pull request targets that branch until it merges.

## Open follow-ups, not part of this work

- Deleting an applicant and their files on request.
- Email confirmation to the applicant.
- Export to spreadsheet.
- More than one opening.
