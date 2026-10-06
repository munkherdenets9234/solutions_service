# Guide Recruitment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Public guide application form on the E&S site, stored in `digitalservice` with private documents, reviewed in the travel admin with status, notes and history.

**Architecture:** `digitalservice` gets a `guide_application` module (model, validation, private Cloudinary file service, repo, service, controllers). The E&S site proxies a multipart form to it with the server-only tenant key. The admin reads and manages applications behind the staff bearer token.

**Tech Stack:** Go/Gin/MongoDB (`digitalservice`), Cloudinary Go SDK already in `go.mod`, Next.js 16 / React 19 / Tailwind (`admin`, `eandstravelmongolia`), `node --test` for pure JS modules.

**Spec:** `docs/superpowers/specs/2026-10-05-guide-recruitment-design.md` (this repo). Read it first. Field lists, enum values, limits and error table live there and are not repeated here.

**Repos and branches:** `D:\bkup\projects\digitalbrochure\{digitalservice,admin,eandstravelmongolia}`, branch `feat/guide-recruitment` in each, created from master. Backend tasks (1-8) come first; admin tasks (9-11) and site tasks (12-15) need the backend routes. Task 16 is the cross-repo check.

## Global Constraints

- Go container-free tests only (`go test`, no Docker). Run with `make test-unit`-style commands; add new packages to the `test-unit` target in `Makefile`. The testcontainers suite (`test/api`) is not run here; write the one integration test in Task 8 and say it was not run.
- Service tests use a fake store behind a narrow interface, the pattern in `internal/service/site_page_service_test.go`.
- Every query is scoped by `tenant_id` taken from `apictx.TenantID(c)`, never from the request.
- Acting staff id comes from `apictx.ActorID(c)`, never from the body.
- Files: types `image/jpeg`, `image/png`, `application/pdf` decided by byte sniffing; limit `cfg.UploadMaxBytes` (10 MiB default); max 8 files; at most 1 per kind except `guide_certificate` (3). Download links last 5 minutes. Cloudinary `type=authenticated`; PDFs `raw`; download through `https://api.cloudinary.com/v1_1/<cloud>/<resource_type>/download`, not the CDN.
- Application bodies, names and file names are never logged.
- Admin routes use `Auth("admin")` plus the subscription gate. They are NOT put in `registerAdminReads` (that file's key-only reads are a known exposure); the admin app must send its bearer token on these GETs.
- Commit messages end with `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`.
- The spec's "counts in the list response" is implemented as a separate route `GET /admin/guide-applications/counts` returning `{new, reviewing, shortlisted, rejected, hired}`, because `response.List` carries only page meta.

## Review Focus

Inputs the spec implies and a person will hit; each is pinned by a named test in the owning task.

1. Mongolian Cyrillic names and `+976 9400 6739`-style phone spacing are accepted (Task 2).
2. Applicant turns exactly 18 on the submit date: accepted; one day short: rejected (Task 2).
3. A `.exe` renamed to `.pdf`, and a PDF sent with `Content-Type: image/png`: sniffed type decides (Task 3).
4. Double-click submit sends two requests: second returns 409, one row (Task 5).
5. Staff of tenant A requests tenant B's application or file: 404 (Task 6).
6. Upload fails on the third file: first two deleted, no row (Task 5).
7. Honeypot filled: 201 with a fake id, no row, no upload (Task 7).

---

## Part A: digitalservice

### Task 1: Model, enums, config flag

**Files:**
- Create: `internal/models/guide_application.go`
- Modify: `internal/config/config.go` (field `CloudinaryPrivateURL`, env `CLOUDINARY_PRIVATE_URL`; method `PrivateFilesURL() string` returns it, else `CloudinaryURL`; method `PrivateFilesEnabled() bool`; add a line to the features report next to the uploads one)
- Modify: `.env.example` (`CLOUDINARY_PRIVATE_URL=` blank, one comment line)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces (package `models`): `GuideApplication` struct with the fields and `bson`/`json` snake_case tags from the spec; nested types `GuidePersonal`, `GuideLanguage{Language, OtherName, Level}`, `GuideExperience`, `GuideDriving`, `GuideAvailability`, `GuideReference`, `GuideFile{ID, Kind, PublicID, Mime, Size, OriginalName}`, `GuideEvent{Type, At, UserID *primitive.ObjectID, UserName, From, To, Text}`. Types `GuideStatus` (consts `GuideNew, GuideReviewing, GuideShortlisted, GuideRejected, GuideHired`) and `GuideFileKind`. Slices `GuideStatuses`, `GuideRegions`, `GuideTourTypes`, `GuideTripLengths`, `GuideLanguageCodes`, `GuideFileKinds` hold the allowed values from the spec.

- [ ] **Step 1: Write failing config test** `TestPrivateFilesURLFallsBackToCloudinaryURL`: with only `CloudinaryURL` set, `PrivateFilesURL()` equals it and `PrivateFilesEnabled()` is true; with both set, the private one wins; with neither, enabled is false.
- [ ] **Step 2:** Run `go test ./internal/config/ -run PrivateFiles -v`. Expected: FAIL (undefined).
- [ ] **Step 3:** Implement the config additions and the model file.
- [ ] **Step 4:** Run `go test ./internal/config/ -count=1` and `go build ./...`. Expected: PASS.
- [ ] **Step 5:** Commit `feat: add guide application model and private file config`.

### Task 2: Validation

**Files:**
- Create: `internal/service/guide_application_validate.go`
- Test: `internal/service/guide_application_validate_test.go`

**Interfaces:**
- Consumes: Task 1 model.
- Produces: `func ValidateGuideApplication(a *models.GuideApplication, now time.Time) error` returning `apierr.ValidationFailed("<field>: <reason>")` for the first failure. `func ValidateGuideFiles(files []GuideUploadMeta) error` where `type GuideUploadMeta struct{ Kind models.GuideFileKind; Mime string; Size int64 }` enforces count, per-kind and kind-enum rules. `func NormalizeEmail(s string) string` (trim, lowercase).

- [ ] **Step 1: Write failing tests**, table style, one case per line: `TestRequiredFields` (each required field from the spec missing in turn names that field in the error); `TestEnumsRejectUnknown` (language, level for mn vs others, region, tour type, trip length, gender); `TestMongolianNameAndPhoneAccepted` (`full_name: "Бат-Эрдэнэ Мөнхбаяр"`, `phone: "+976 9400 6739"`); `TestAgeBoundary` (born exactly 18 years before `now`: ok; one day later: error); `TestDrivingFieldsOnlyWhenLicense` (license_class set with `has_license=false` rejected); `TestReferenceCap` (6 references rejected); `TestTextCap` (2001 characters rejected, 2000 ok); `TestConsentRequired`; `TestFilesRules` (no cv rejected; 9 files rejected; two `id_card` rejected; three `guide_certificate` accepted; unknown kind rejected). Add `TestNormalizeEmail`.
- [ ] **Step 2:** Run `go test ./internal/service/ -run "Required|Enums|Mongolian|Age|Driving|Reference|TextCap|Consent|FilesRules|NormalizeEmail" -v`. Expected: FAIL (undefined).
- [ ] **Step 3:** Implement. Phone: strip spaces, `-`, `(`, `)`, leading `+`, then 7-15 digits. Email: `net/mail.ParseAddress` and an `@` check. Lengths count runes (`utf8.RuneCountInString`).
- [ ] **Step 4:** Re-run the same command. Expected: PASS.
- [ ] **Step 5:** Commit `feat: validate guide applications`.

### Task 3: Private file service

**Files:**
- Create: `internal/service/private_file_service.go`
- Test: `internal/service/private_file_service_test.go`

**Interfaces:**
- Produces:
  - `type PrivateFiles interface { Upload(ctx, r io.Reader, tenantID primitive.ObjectID) (*StoredFile, error); Delete(ctx, publicID, mime string) error; DownloadURL(publicID, mime string, ttl time.Duration) (url string, expires time.Time, err error); Available() bool }`
  - `type StoredFile struct{ PublicID, Mime string; Size int64 }`
  - `func SniffPrivateType(head []byte) (mime string, ok bool)` using `http.DetectContentType`; accepts only the three allowed types.
  - `func NewPrivateFileService(cloudinaryURL string, maxBytes int64) (*PrivateFileService, error)`; returns `nil, nil` when the URL is blank, as `NewUploadService` does. `*PrivateFileService` implements `PrivateFiles`; `Available()` is nil-safe.
  - `func signDownloadParams(p map[string]string, secret string) string` (SHA-1 hex of sorted `k=v` joined by `&`, plus secret), kept unexported for the test.

- [ ] **Step 1: Write failing tests**: `TestSniffAcceptsJpegPngPdf` (real magic bytes); `TestSniffRejectsExeNamedPdf` (`MZ...` bytes rejected); `TestSniffIgnoresClientDeclaredType` (PDF bytes pass regardless; the function takes bytes only); `TestSignDownloadParamsMatchesKnownVector` (use a fixed params map and secret, expected hex computed independently with `printf ... | sha1sum`); `TestDownloadURLShape` (URL host `api.cloudinary.com`, path `/v1_1/<cloud>/raw/download` for pdf and `/image/download` for jpeg, query has `type=authenticated`, `expires_at`, `api_key`, `signature`, and never the secret); `TestAvailableNilReceiver`; `TestUploadRejectsOverLimitStream` using an unexported `uploadFunc` field replaced in the test with a fake that records calls.
- [ ] **Step 2:** Run `go test ./internal/service/ -run "Sniff|SignDownload|DownloadURL|Available|UploadRejects" -v`. Expected: FAIL.
- [ ] **Step 3:** Implement. Upload: read 512 bytes, sniff, cap stream with the one-extra-byte technique from `upload_service.go`, `uploader.UploadParams{Type: "authenticated", ResourceType: "raw" for pdf else "image", Folder: "tenants/<tenant>/guide-applications", PublicID: uuid}`; destroy the object if it went over the limit. `DownloadURL`: params `public_id`, `type=authenticated`, `timestamp=now`, `expires_at=now+ttl`, `attachment=true`; the SDK client is held in a field so the test can inject `uploadFunc`/`destroyFunc`.
- [ ] **Step 4:** Re-run. Expected: PASS.
- [ ] **Step 5:** Commit `feat: add private file service for applicant documents`.

### Task 4: Repository and indexes

**Files:**
- Create: `internal/repository/guide_application_repo.go`
- Modify: `internal/repository/indexes.go` (three indexes from the spec)
- Test: none container-free; covered by the service fake. Add `TestGuideApplicationIndexesListed` only if `EnsureIndexes` exposes its spec list (it does not today, so skip and note it).

**Interfaces:**
- Produces `GuideApplicationRepo` (`NewGuideApplicationRepo(db *mongo.Database)`, collection `guide_applications`) with:
  - `Create(ctx, tenantID, a *models.GuideApplication) error` (sets id, tenant, status `new`, timestamps, empty non-nil `events`)
  - `FindByID(ctx, tenantID, id primitive.ObjectID) (*models.GuideApplication, error)`
  - `List(ctx, tenantID, f GuideListFilter, page, limit int) ([]*models.GuideApplication, int64, error)` with `type GuideListFilter struct{ Status models.GuideStatus; Q, Language, Region string }`; `Q` is a case-insensitive `regexp.QuoteMeta` match on name, phone, email
  - `CountByStatus(ctx, tenantID) (map[models.GuideStatus]int64, error)`
  - `HasRecentByEmail(ctx, tenantID, season, email string, since time.Time) (bool, error)`
  - `SetStatus(ctx, tenantID, id, status, ev models.GuideEvent) error` and `AddNote(ctx, tenantID, id, ev models.GuideEvent) error`, both single `UpdateOne` with `$set` + `$push` on `events`, filtered by `_id` and `tenant_id`; return `mongo.ErrNoDocuments` when nothing matched
  - `Delete(ctx, tenantID, id) error` (used only to roll back a failed submit)

- [ ] **Step 1:** Write the repo and index entries mirroring `contact_message_repo.go`.
- [ ] **Step 2:** Run `go build ./... && go vet ./internal/repository/`. Expected: clean.
- [ ] **Step 3:** Commit `feat: add guide application repository and indexes`.

### Task 5: Service, submit flow

**Files:**
- Create: `internal/service/guide_application_service.go`
- Test: `internal/service/guide_application_service_test.go`

**Interfaces:**
- Consumes: Tasks 2, 3, 4.
- Produces:
  - `type guideStore interface` with exactly the repo methods above (so a fake satisfies it).
  - `type guideUsers interface{ FindByIDs(ctx, tenantID, ids []primitive.ObjectID) ([]*models.TenantUser, error) }`
  - `NewGuideApplicationService(store guideStore, files PrivateFiles, users guideUsers, now func() time.Time) *GuideApplicationService`
  - `type GuideUpload struct{ Kind models.GuideFileKind; OriginalName string; Open func() (io.ReadCloser, error) }`
  - `type SubmitResult struct{ ID string; ConfirmationID string }`
  - `Submit(ctx, tenantID, a *models.GuideApplication, uploads []GuideUpload) (*SubmitResult, error)`

- [ ] **Step 1: Write failing tests** with a fake store and fake `PrivateFiles` that records uploads and deletes: `TestSubmitSavesRowAndFiles` (row has `status=new`, `events` empty, files carry returned public ids, result `ConfirmationID` is `GA-` + last six id characters uppercased); `TestSubmitDuplicateWithin24hConflicts` (second call same email in other case/whitespace -> `apierr` 409, store has one row, fake files saw no second upload); `TestSubmitAllowsSameEmailAfter24h` (advance `now` by 25h); `TestSubmitUploadFailureDeletesEarlierFiles` (fake fails on third upload: two deletes, zero rows); `TestSubmitInsertFailureDeletesFiles`; `TestSubmitFeatureUnavailableWhenFilesNil` (files `Available()==false` -> `FeatureUnavailable`, nothing stored); `TestSubmitValidatesBeforeUploading` (invalid email: zero uploads).
- [ ] **Step 2:** Run `go test ./internal/service/ -run Submit -v`. Expected: FAIL.
- [ ] **Step 3:** Implement. Order: availability check, `ValidateGuideApplication`, `ValidateGuideFiles` from upload metadata (size and mime come from sniffing at upload; so validate kind rules first, then upload, then re-check mime/size from `StoredFile`), duplicate check, upload loop with deferred rollback closure, insert, return. Normalize email before storing and checking. Season fixed `summer-2027`.
- [ ] **Step 4:** Re-run. Expected: PASS.
- [ ] **Step 5:** Commit `feat: add guide application submit flow`.

### Task 6: Service, admin operations

**Files:**
- Modify: `internal/service/guide_application_service.go`
- Test: `internal/service/guide_application_service_test.go`

**Interfaces:**
- Produces on `*GuideApplicationService`:
  - `List(ctx, tenantID, f repository.GuideListFilter, page, limit int) ([]*models.GuideApplication, int64, error)` (limit clamp 1-100, default 20, as `ContactMessageService.List`)
  - `Counts(ctx, tenantID) (map[models.GuideStatus]int64, error)` with all five keys present, zero filled
  - `Get(ctx, tenantID, idHex string) (*models.GuideApplication, error)` (404 via `apierr.NotFound("guide application")`)
  - `SetStatus(ctx, tenantID, idHex string, status models.GuideStatus, actor *primitive.ObjectID) error`
  - `AddNote(ctx, tenantID, idHex, text string, actor *primitive.ObjectID) error`
  - `FileDownload(ctx, tenantID, idHex, fileID string) (url string, expires time.Time, err error)`

- [ ] **Step 1: Write failing tests**: `TestSetStatusAppendsEventWithNameAndFromTo`; `TestSetStatusSameValueWritesNoEvent`; `TestSetStatusRejectsUnknownValue`; `TestSetStatusNeedsActor` (nil actor -> 401-class error; staff identity is mandatory); `TestAddNoteBounds` (empty and 2001 characters rejected, 2000 ok; whitespace-only rejected); `TestNotesAreAppendOnly` (two notes -> two events in order); `TestCrossTenantGetIs404` and `TestCrossTenantFileDownloadIs404` (application created for tenant A, requested as tenant B); `TestFileDownloadUnknownFileIs404`; `TestCountsFillsZeroes`; `TestEventKeepsUserNameAfterRename` (event stores `user_name` at write time).
- [ ] **Step 2:** Run `go test ./internal/service/ -run "SetStatus|AddNote|Notes|CrossTenant|FileDownload|Counts|EventKeeps" -v`. Expected: FAIL.
- [ ] **Step 3:** Implement. Name lookup through `guideUsers.FindByIDs` for the single actor id; empty name falls back to the email-less literal `"staff"`.
- [ ] **Step 4:** Re-run. Expected: PASS.
- [ ] **Step 5:** Commit `feat: add guide application status, notes and file links`.

### Task 7: Controllers, routes, wiring

**Files:**
- Create: `internal/api/tenant/public/guide_applications.go`, `internal/api/tenant/private/guide_applications.go`
- Modify: `internal/api/tenant/public/public.go` (route in the `lead` group), `internal/api/tenant/private/private.go` (routes in the `admin` group), `internal/api/tenant/tenant.go`, `internal/api/router.go`, `internal/bootstrap/bootstrap.go`, `internal/bootstrap/wiring.go` (repo, service, `buildPrivateFiles(cfg, log)` modelled on `buildUpload`, nil when disabled; every place that lists `ContactMessage`)
- Test: `internal/api/guard_test.go` (extend), `internal/api/tenant/public/guide_applications_test.go`

**Interfaces:**
- Consumes: Tasks 5-6.
- Produces routes: `POST /guide-applications` (multipart: part `data` = JSON of `models.GuideApplication`, parts `file_<kind>` and `file_guide_certificate_1..3`); `GET /admin/guide-applications`, `GET /admin/guide-applications/counts`, `GET /admin/guide-applications/:id`, `PATCH /admin/guide-applications/:id/status` body `{status}`, `POST /admin/guide-applications/:id/notes` body `{text}`, `GET /admin/guide-applications/:id/files/:fileId`. Success bodies: submit `{id, confirmation_id}` (201); file `{url, expires_at}`.
- Honeypot: form field `website`; non-empty returns 201 `{id: <fresh ObjectID hex>, confirmation_id}` and stores nothing.
- Body ceiling: `http.MaxBytesReader` at 8 x `UploadMaxBytes` plus 1 MiB; over it returns 413-class `apierr.ValidationFailed`.

- [ ] **Step 1: Write failing tests**: guard test extended so the generic walk (`TestEveryTenantRouteRequiresAPIKey`) still passes, plus `TestGuideAdminRoutesRequireBearer` (every `/admin/guide-applications*` route with a key but no token returns 401). Handler tests in the public package with a fake service interface: `TestHoneypotReturnsFakeSuccessAndStoresNothing`, `TestMissingDataPartIs400`, `TestMultipartMapsFilePartsToKinds`.
- [ ] **Step 2:** Run `go test ./internal/api/... -run "Guide|Honeypot|Multipart|EveryTenantRoute" -v`. Expected: FAIL.
- [ ] **Step 3:** Implement controllers (depend on a small interface so the handler test can fake the service) and wire. Public route goes in the `lead` group (rate limited, outside the subscription gate). Private routes go in the `admin` group. Add `PATCH` is already supported by `httpx.G`.
- [ ] **Step 4:** Run `go build ./... && go vet ./... && go test ./internal/... ./pkg/... -count=1`. Expected: all PASS.
- [ ] **Step 5:** Commit `feat: expose guide application routes`.

### Task 8: Live Cloudinary check and integration test

**Files:**
- Create: `internal/service/private_file_service_live_test.go` (build tag `live`)
- Create: `test/api/guide_applications_test.go` (testcontainers; written, not run here)

- [ ] **Step 1:** Add `TestPrivateFileServiceLive` in `internal/service/private_file_service_live_test.go` behind `//go:build live`, skipped unless `CLOUDINARY_URL` is set: upload a 300-byte PDF, assert `DownloadURL` with a 5-minute ttl fetches 200 and starts with `%PDF`, assert a URL with `expires_at` in the past returns non-200, `Delete`, assert the unsigned object URL returns 401.
- [ ] **Step 2:** Run `go test -tags live ./internal/service/ -run PrivateFileServiceLive -v` with the real `CLOUDINARY_URL` loaded. Expected: PASS. If the expired link still returns 200, record that in the spec's follow-ups; the 5-minute claim then rests on the signature timestamp only.
- [ ] **Step 3:** Write the integration test: submit with a CV and a photo, list as staff, set status, add note, fetch file link; assert cross-tenant 404. State in the commit message that it was not run (no Docker).
- [ ] **Step 4:** Commit `test: add live Cloudinary check and guide application integration test`.

---

## Part B: admin

### Task 9: Types, data layer, actions, nav

**Files:**
- Modify: `src/lib/types.ts`, `src/lib/nav.ts` (entry `{ href: '/guide-applications', label: 'Guide Applications' }` under Manage)
- Create: `src/lib/data/guide-applications.ts`, `src/app/(dashboard)/guide-applications/actions.ts`, `src/lib/guide-labels.mjs`, `src/lib/guide-labels.test.mjs`

**Interfaces:**
- Produces: TypeScript `GuideApplication` (snake_case fields as in the backend JSON), `GuideStatus` union; `listGuideApplications(token, { page, limit, status?, q?, language?, region? })`, `getGuideApplication(token, id)`, `guideCounts(token)`, `guideFileLink(token, id, fileId) -> {url, expires_at}`; server actions `setGuideStatusAction(id, status)`, `addGuideNoteAction(id, text)`, `openGuideFileAction(id, fileId)` that call `requireToken()` like `contact-messages/actions.ts`. `guideLabel(group, value) -> string` in `guide-labels.mjs` maps enum values to display text (e.g. `adventure_4x4` -> `Adventure / 4x4`) and returns the raw value for unknown input.

- [ ] **Step 1: Write failing test** `guide-labels.test.mjs`: known values map, unknown value returns itself, every enum value in the spec has a label (list them in the test).
- [ ] **Step 2:** Run `node --test src/lib/guide-labels.test.mjs`. Expected: FAIL.
- [ ] **Step 3:** Implement labels, types, data functions (all pass the token, unlike older reads), actions, nav entry.
- [ ] **Step 4:** Run `node --test src/lib/guide-labels.test.mjs && npx tsc --noEmit`. Expected: PASS, no type errors.
- [ ] **Step 5:** Commit `feat: add guide application types, data layer and actions`.

### Task 10: List page

**Files:**
- Create: `src/app/(dashboard)/guide-applications/page.tsx`

**Interfaces:** Consumes Task 9. Uses `DataTable`, `StatusBadge`, `ErrorNotice`, `safeLoad` as `contact-messages/page.tsx` does.

- [ ] **Step 1:** Implement: tabs per status with counts (links with `?status=`), search box (`?q=`), language and region selects (`?language=`, `?region=`), columns name / phone / languages summary / regions / created / status, row links to `/guide-applications/[id]`, empty message "No applications yet."
- [ ] **Step 2:** Run `npx tsc --noEmit && npm run lint`. Expected: clean.
- [ ] **Step 3:** Start the dev server with the preview tool, sign in, open `/guide-applications`; expected: empty state or rows, no console errors.
- [ ] **Step 4:** Commit `feat: add guide applications list page`.

### Task 11: Detail page

**Files:**
- Create: `src/app/(dashboard)/guide-applications/[id]/page.tsx`, `src/components/admin/GuideStatusSelect.tsx`, `src/components/admin/GuideNoteForm.tsx`, `src/components/admin/GuideFileLink.tsx` (client components; each calls its server action)

- [ ] **Step 1:** Implement: all eight sections read-only, labels through `guideLabel`; files list where clicking runs `openGuideFileAction` and opens the returned URL in a new tab; status select; note form; timeline newest first showing type, who, when, from -> to or text. A missing id renders `notFound()`.
- [ ] **Step 2:** Run `npx tsc --noEmit && npm run lint`. Expected: clean.
- [ ] **Step 3:** Preview: open a detail page (after Task 16 data exists); expected: sections render, status change adds a timeline entry, note appears, file opens.
- [ ] **Step 4:** Commit `feat: add guide application detail page`.

---

## Part C: eandstravelmongolia

### Task 12: Proxy route and client

**Files:**
- Modify: `src/lib/api/client.ts` (add `apiPostForm<T>(path: string, body: FormData)` that sends no `Content-Type` header so `fetch` sets the boundary)
- Create: `src/app/api/guide-applications/route.ts`, `src/lib/careers/proxy.mjs`, `src/lib/careers/proxy.test.mjs`

**Interfaces:**
- Produces: `POST /api/guide-applications` accepting the same multipart the backend does; returns `{ confirmationId }` (201) or `{ error, field? }` with the backend status passed through for 400/409/413/422/429 and 502 otherwise. `checkBodySize(contentLength: number|null, maxBytes: number) -> boolean` in `proxy.mjs`.

- [ ] **Step 1: Write failing test** `proxy.test.mjs`: `checkBodySize` false when over, true at limit, false when header missing (reject unknown length).
- [ ] **Step 2:** Run `node --test src/lib/careers/proxy.test.mjs`. Expected: FAIL.
- [ ] **Step 3:** Implement helper, `apiPostForm`, route (rejects over-size before reading; forwards `request.formData()`; never echoes the tenant key or the submitted body).
- [ ] **Step 4:** Run `node --test src/lib/careers/ && npx tsc --noEmit`. Expected: PASS.
- [ ] **Step 5:** Commit `feat: proxy guide applications to the backend`.

### Task 13: Client validation module

**Files:**
- Create: `src/lib/careers/validate.mjs`, `src/lib/careers/validate.test.mjs`, `src/lib/careers/validate.d.mts`

**Interfaces:**
- Produces: `validateApplication(values, files, now) -> { ok: boolean, errors: Record<string,string> }` (keys are field paths, values are message keys such as `required`, `invalid_email`, `under_18`, `file_type`, `file_size`), mirroring the backend rules in Task 2. `visibleDrivingFields(hasLicense: boolean) -> string[]`.

- [ ] **Step 1: Write failing tests** mirroring Task 2's cases: required fields, Mongolian name and `+976 9400 6739` phone accepted, age boundary, unknown enum, driving fields hidden when no license, file rules (type by extension and size on the client; the server stays the authority), CV required.
- [ ] **Step 2:** Run `node --test src/lib/careers/validate.test.mjs`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Re-run. Expected: PASS.
- [ ] **Step 5:** Commit `feat: add careers form validation`.

### Task 14: Locale strings

**Files:**
- Modify: `src/locales/en.json`, `src/locales/mn.json`, `src/locales/ko.json`, and the `Translation` type file `src/types/i18n.ts` if it is not derived from `en.json`

- [ ] **Step 1:** Add a `careers` object: page meta, opening card text, the eight section titles and every field label, option labels for each enum value, consent text, success and error messages, in English and Mongolian. Mongolian wording follows the agency's list (`Хувийн мэдээлэл`, `Хэлний мэдлэг`, `Guide-ийн туршлага`, `Монгол орны мэдлэг`, `Машин жолоодох чадвар`, `Ажлын боломж`, `Өмнө ажиллаж байсан байгууллага / хүн`, `Upload`). `ko.json` gets the English strings.
- [ ] **Step 2:** Run `node -e "for (const l of ['en','mn','ko']) { const j=require('./src/locales/'+l+'.json'); if(!j.careers) throw l }" && npx tsc --noEmit`. Expected: no output, no type errors.
- [ ] **Step 3:** Commit `feat: add careers strings`.

### Task 15: Pages and form

**Files:**
- Create: `src/app/[locale]/careers/page.tsx`, `src/app/[locale]/careers/guide/page.tsx`, `src/components/forms/GuideApplicationForm.tsx`
- Modify: `src/components/layout/Footer.tsx` (add a `careers` link beside `contact`), sitemap if it lists static pages (`src/app/sitemap.ts`)

**Interfaces:** Consumes Tasks 12-14. The form builds `FormData` with part `data` (JSON) and `file_<kind>` parts, posts to `/api/guide-applications`, and renders server field errors beside the field.

- [ ] **Step 1:** Implement `/careers` (one opening card "Summer guide 2027" with an Apply button) and `/careers/guide` using `getTranslation(locale)` like the contact page. Form: eight sections with a section list, conditional fields via `visibleDrivingFields`, off-screen honeypot `website`, consent checkbox, file rows with size and remove button, submit button disabled while sending (stops double submit), data and files kept on error, success screen with the confirmation id.
- [ ] **Step 2:** Run `npx tsc --noEmit && npm run lint`. Expected: clean.
- [ ] **Step 3:** Preview `/en/careers`, `/mn/careers/guide`: both render, validation messages show, no console errors.
- [ ] **Step 4:** Commit `feat: add careers page and guide application form`.

---

## Task 16: End-to-end check (all three running)

- [ ] **Step 1:** Start `digitalservice` (`PORT=8080`, Atlas DB, `CLOUDINARY_URL` set), the site and the admin via the preview tool.
- [ ] **Step 2:** Submit the form from `/mn/careers/guide` with a CV PDF and a photo. Expected: confirmation id shown.
- [ ] **Step 3:** In the admin, open the list: application visible under tab `New (1)`. Open detail: all sections, open the CV (PDF opens), set status `reviewing`, add a note; timeline shows two entries with the staff name.
- [ ] **Step 4:** Submit the same email again. Expected: duplicate message, no new row.
- [ ] **Step 5:** Submit with a `.txt` renamed to `.pdf`. Expected: file-type error, no row, nothing left in Cloudinary under `tenants/<id>/guide-applications`.
- [ ] **Step 6:** Report plainly what ran and what did not (Docker integration test, Korean wording, mobile layout if unchecked).

---

## Post-implementation corrections

What changed compared with the plan text above. The plan body is left as written.

- Resource type: PDFs are not stored as `raw`. The Cloudinary Go SDK uploads through the auto endpoint and ignores `UploadParams.ResourceType`, so PDFs and images are all stored as `image` with type `authenticated`, and downloads use `/v1_1/<cloud>/image/download`. An upload that does not land as image + authenticated is destroyed and rejected.
- `TestDownloadURLShape` expects the path `/v1_1/<cloud>/image/download` for every supported type, not `/raw/download` for PDF.
- Signatures are `DownloadURL(publicID, mime, ttl)` and `Delete(ctx, publicID, mime)`; `Delete` with an empty id does nothing.
- Routes: the admin routes are bearer + role `admin` only (staff get 403) and are not in `registerAdminReads`. The public handler is `public/guide_applications.go`, registered in the lead group.
- Docker test: the testcontainers integration test (`test/api`) was written but not run, because Docker was not available. Container-free tests and the Go dev toolchain were used instead.
- Live test: the live Cloudinary check uploads an authenticated image, expects a signed download to return 200 with a PDF, logs the status of an expired link (observed 401), and requires the link to stop serving after delete (observed 404). The plan's earlier assertions (raw upload, unsigned fetch refused) no longer apply.
- Errors: validation failures are HTTP 422 `VALIDATION_FAILED` (the plan said 400); malformed multipart or JSON and unexpected parts are 400. File errors are named `files.<kind>` and files are checked (type, size) before any upload starts.
- Also added after review: `consent_at` is stamped by the server; the duplicate guard is per process only; the submit handler extends the request deadlines to 3 minutes; list `meta` reports the effective page and limit.
