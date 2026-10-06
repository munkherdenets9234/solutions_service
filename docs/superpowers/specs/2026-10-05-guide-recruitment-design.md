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

The public handler is `internal/api/tenant/public/guide_applications.go`, registered in the lead group in `public.go` (rate limited, outside the subscription gate). A tenant with a lapsed subscription should not silently drop applicants.

The admin routes (`internal/api/tenant/private/guide_applications.go`) require the admin bearer token and the role `admin`. Staff get 403. They are not in `registerAdminReads`, because applicant data is personal and must not be readable by every staff role.

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
- Upload: `type: authenticated`, folder `tenants/<tenant_id>/guide-applications`, random UUID name. The destination never comes from the request. The Cloudinary Go SDK always uploads through the auto endpoint and ignores `UploadParams.ResourceType`, so PDFs and images are all stored as resource type `image` with type `authenticated`. An upload that does not come back as `image` + `authenticated` is destroyed and rejected (502).
- Cloudinary reports API failures inside the SDK result (`res.Error`) with a nil Go error. The service checks for that and turns it into an error. A corrupt PDF is rejected by Cloudinary as "Invalid PDF file"; the service maps it to 422 "document could not be read; export it again as a valid PDF or image". Provider text is never passed to the client.
- Download: `DownloadURL(publicID, mime, ttl)` returns a signed Cloudinary download URL, `https://api.cloudinary.com/v1_1/<cloud>/image/download`, with parameters `public_id`, `type=authenticated`, `timestamp`, `expires_at`, `attachment`, `api_key` and `signature`. The signature is the SHA-1 hex of the parameters sorted by key and joined as `k=v&k=v`, with the API secret appended. The secret is never in the URL. The link is valid for 5 minutes. Cloudinary enforces the expiry: an expired link returns 401, and a link to a deleted object returns 404. The CDN delivery URL is not used, because the account refuses CDN delivery of authenticated files. The admin file route checks tenant ownership of the application and the file id before signing.
- Cleanup: `Delete(ctx, publicID, mime)` is used when a submit fails partway. An empty public id is a no-op and nothing is sent upstream. Cleanup runs on a context that is not cancelled with the request. If a cleanup delete fails, the failure is attached to the internal cause of the returned error so it is logged with the public ids; the client sees the original error only.

### Submit flow

`POST /guide-applications` accepts `multipart/form-data`: one part `data` holding the JSON document, plus file parts named `file_<kind>` (and `file_guide_certificate_1..3`).

Multipart rules: the only plain form fields are `data` and `website`, each at most once. Any other form field, or a file part without a filename, returns 400. A bare `file_guide_certificate` part (no number) is refused as an unexpected file part (400). At most 8 files; the `data` part is at most 256 KiB; the whole body is at most 8 times the per-file limit plus 1 MiB. The handler extends the server's 15 s read and write deadlines to 3 minutes for this request only, because a submit uploads files one after another.

1. Rate limit per client IP, using the existing `middleware.RateLimiter.Limit`.
2. Honeypot field `website`. If non-empty the server answers `201` with a fake id and stores nothing.
3. Validate the JSON. Validate every file before any upload starts: the controller refuses a file larger than the per-file limit, and the service reads the first bytes of each file and sniffs its type. A bad file fails the whole submit and nothing is uploaded.
4. Upload files one by one. On any failure, delete the files already uploaded and return the error. Nothing is saved.
5. Insert the document with `status: new` and an empty `events` list (no staff actor yet).
6. If the insert fails, delete the uploaded files.
7. Return `201` with `{id, confirmation_id}` where `confirmation_id` is `GA-` plus the last six characters of the id, uppercase.

`consent_at` must be present in the request, but the stored value is the server's clock at submit time, not the client's. The uploaded file's original name is trimmed to 255 characters and stripped of control characters before it is stored (empty becomes `document`); it is for display only and never chooses a storage path.

The duplicate check (same tenant, season and email within 24 hours) is serialised per process only. Two API instances could still both accept the same email at the same moment; a unique index cannot express a 24 hour window.

### Admin routes

- `GET /admin/guide-applications?page&limit&status&q&language&region`. `q` matches name, phone and email, case-insensitive. Counts per status for the filter tabs come from `GET /admin/guide-applications/counts`. Default sort newest first.
- `GET /admin/guide-applications/:id` returns the full document.
- `PATCH /admin/guide-applications/:id/status` with `{status}`. Value checked against the list. Appends a status event with `from`/`to`. The acting user comes from the admin token, never from the body. Setting the same status again returns success and writes no event.
- `POST /admin/guide-applications/:id/notes` with `{text}`, 1 to 2000 characters. Appends a note event. Notes are never edited or deleted.
- `GET /admin/guide-applications/:id/files/:fileId` returns `{url, expires_at}`.

Concurrent changes: last write wins on `status`. The service reads the current status and then writes without a condition, so the recorded `from` can be stale and a duplicate event can be written. Both events stay in `events[]`, so nothing is lost. Appends use Mongo `$push`, not read-modify-write.

The list `meta.page` and `meta.limit` are the values the service used (page at least 1, limit 1 to 100, otherwise 20). The search text `q` is cut to 100 characters.

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
| Missing or invalid field | 422 `VALIDATION_FAILED`, message `<field.path>: <reason>`; form shows it inline |
| Malformed multipart, invalid JSON, unexpected form field or file part, missing `data` | 400 `BAD_REQUEST` |
| Body too large, more than 8 files, `data` part over 256 KiB | 422 `VALIDATION_FAILED` |
| Wrong file type, empty file, unreadable PDF or too big | 422 `VALIDATION_FAILED`, message `files.<kind>: <reason>`; checked before any upload; nothing saved |
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
- The unit and controller tests never call Cloudinary. A separate live check (`private_file_service_live_test.go`, build tag `live`, skipped unless `CLOUDINARY_URL` is set) runs against the real account: it uploads a small PDF as an authenticated image, fetches the signed download link and requires 200 with a PDF body, fetches an expired link and logs its status (observed: 401), deletes the object, and fetches the earlier link again, which must not return 200 (observed: 404). It removes the object even when an assertion fails and never prints the URL or the credential. Fallback if Cloudinary ever stops working for this: store files in MongoDB GridFS behind the same `PrivateFileService` interface; no other code changes.

## Delivery

Three repositories, one branch each, named `feat/guide-recruitment`. Build order: backend first (the other two depend on its routes), then admin, then site. `digitalservice` branches from `refactor/backend-core`, so its pull request targets that branch until it merges.

## Open follow-ups, not part of this work

- Deleting an applicant and their files on request.
- Email confirmation to the applicant.
- Export to spreadsheet.
- More than one opening.
