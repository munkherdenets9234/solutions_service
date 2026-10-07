# Request Email Notifier Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Email opted-in tenant users when the storefront creates a booking, rental, airport transfer or guide application, with an outbox (retry, status), one-click unsubscribe, a mail log in admin, customers with avatars on the review form, and guide file preview and download.

**Architecture:** tenantcore gets one fixed-field template. digitalservice enqueues one `mail_outbox` row per recipient after each successful create; a worker loop sends due rows through the existing `notify.Client` and retries with backoff. Unsubscribe is an HMAC-signed token handled by a public route. The `admin` app gets a mail log page, a staff toggle, the review-form customer fields, and guide file preview.

**Tech Stack:** Go (gin, mongo-driver, Cloudinary SDK) in `digitalservice` and `tenantcore`; Next.js (server actions) in `admin`.

**Spec:** `digitalservice/docs/superpowers/specs/2026-10-06-request-email-notifier-design.md`

## Global Constraints

- Mail goes only through `internal/notify.Client.Send(ctx, to, template string, data map[string]string) error`. digitalservice never holds mail credentials.
- Template data is fixed fields only; every value capped at 256 runes, `unsubscribe_url` at 512. No visitor free text is a subject, body or From.
- A mail failure never fails or delays the visitor's request: `Notify` only inserts rows and does no network call.
- Retry: 5 attempts in total; after failure N (1..4) the next attempt is at +1m, +5m, +30m, +2h; after failure 5, `status=failed`. `ErrMailNotConfigured` retries like any failure. Rows deleted after 30 days (TTL index on `created_at`).
- `last_error` holds a code only: `mail_not_configured`, `rate_limited`, `upstream`, `unreachable`. Never provider text.
- UI wording is "Accepted", never "delivered".
- Unsubscribe token: HMAC-SHA256 over `tenant_id`, `user_id`, expiry (90 days); key from `MAIL_UNSUBSCRIBE_KEY`, required when the notifier is enabled, startup fails without it, no default. Constant-time compare. Every invalid token gets the identical response.
- Every query on tenant data is scoped by tenant. Storage public ids never leave the service.
- Preview URL TTL 5 minutes. Avatar types jpeg and png only, sniffed from bytes, never the client Content-Type.
- Never log request bodies, tokens, full addresses, or secrets. Log ids and outcomes only (`AGENTS.md`).
- Gate for digitalservice: `go build ./... && go vet ./internal/... ./pkg/... && go test ./internal/... ./pkg/... -count=1`. Never `go test ./...` there (`test/api` needs MongoDB). In tenantcore run `go test ./...`.
- Do not push, deploy, set production secrets, or change production settings. digitalservice is on `master` with unrelated uncommitted work: work on a new branch and commit only the files each task names, never `git add -A`.

## Review Focus

- Any create succeeds when `notify.Client` is nil or no recipient exists: response unchanged, no rows, no panic.
- One recipient's send fails: other recipients' rows still send; the create already succeeded.
- A queued row whose user unsubscribed or was suspended before sending is skipped, not sent.
- Two workers (two instances) claim the same due row: only one sends it.
- Unsubscribe `GET` (a mail scanner prefetch) changes nothing; only `POST` does. Expired, tampered and unknown-user tokens give byte-identical responses.
- Visitor-supplied name or notes contain CR/LF or are 10,000 characters: `summary` is single-line, capped at 256 runes.
- Guide file route with `disposition=bogus` is 400; a `fileId` from another tenant's application is 404.
- Avatar that is a PDF or a renamed `.exe` is rejected with 4xx and no `Customer` is created; a `customer_id` from another tenant is rejected on review create.

## Open items the implementer must resolve first

- **Site host:** `admin_url` and `unsubscribe_url` are built from the tenant's site host. Read `internal/tenantresolve` for what the resolved tenant exposes. If it has no host, or the storefront does not proxy `/api/v1/public/*` to digitalservice, **stop and report** (spec gap); do not add config.

---

### Task 1: tenantcore template `request_notification`

**Files:**
- Modify: `tenantcore/pkg/mailer/template.go`
- Test: `tenantcore/pkg/mailer/template_test.go`

**Interfaces:**
- Produces: `mailer.TemplateRequestNotification Template = "request_notification"`; required keys `app`, `tenant`, `request_type`, `summary`, `admin_url`, `unsubscribe_url`; subject `New {{request_type}} for {{tenant}}`. Body lists type, tenant, summary, the admin link, and a closing line with the unsubscribe link.

- [ ] **Step 1: Write failing tests:** `TestRequestNotificationRenders` (all six keys; subject has type and tenant; body has `admin_url`, `summary`, `unsubscribe_url`); `TestRequestNotificationRequiresAllKeys` (drop each key, error names it); `TestRequestNotificationSubjectCannotCarryLineBreaks` (`request_type` = `x\r\nBcc: a@b.c` gives a single-line subject); `Known("request_notification")` is true.
- [ ] **Step 2: Run** `cd tenantcore && go test ./pkg/mailer -run RequestNotification -v`. Expected: FAIL, undefined `TemplateRequestNotification`.
- [ ] **Step 3: Implement** the const and `templates` entry, modelled on `TemplateLeadNotification`.
- [ ] **Step 4: Run** `cd tenantcore && go test ./... -count=1`. Expected: PASS, including `openapi_test.go`.
- [ ] **Step 5: Commit** `pkg/mailer/template.go pkg/mailer/template_test.go` in tenantcore, message `feat(mailer): request_notification template`.

---

### Task 2: `TenantUser.ReceiveEmails` and recipients lookup

**Files:**
- Modify: `digitalservice/internal/models/tenant_user.go`, `internal/repository/tenant_user_repo.go`, `internal/service/tenant_user_service.go`, `internal/api/tenant/private/users.go` (create/update DTOs), user DTO in `internal/dto/` if one exists
- Test: `internal/service/tenant_user_service_test.go` (create if absent)

**Interfaces:**
- Produces: `TenantUser.ReceiveEmails bool` (`bson:"receive_emails" json:"receive_emails"`); `(*TenantUserRepo).FindEmailRecipients(ctx context.Context, tenantID primitive.ObjectID) ([]*models.TenantUser, error)` returning only active users with `receive_emails=true` for that tenant; `(*TenantUserRepo).SetReceiveEmails(ctx context.Context, tenantID, id primitive.ObjectID, v bool) error`.

- [ ] **Step 0: Create branch** `git checkout -b feat/request-email-notifier` in digitalservice.
- [ ] **Step 1: Write failing test** `TestCreateAndUpdatePersistReceiveEmails` against the service's existing test seam (read how other tenant_user tests fake the repo; if none exists, assert the input structs carry the field to the model).
- [ ] **Step 2: Run** `go test ./internal/service -run ReceiveEmails -v`. Expected: FAIL.
- [ ] **Step 3: Implement** the field, both repo methods (filter `{tenant_id, status: active, receive_emails: true}`), and `receive_emails` on the create and update handlers in `users.go`. Default false.
- [ ] **Step 4: Run** the gate. Expected: PASS.
- [ ] **Step 5: Commit** only touched files, message `feat(users): receive_emails flag and recipients lookup`.

---

### Task 3: Unsubscribe token and config

**Files:**
- Create: `digitalservice/internal/unsubscribe/token.go`
- Modify: `internal/config/config.go`, `internal/config/config_test.go`
- Test: `internal/unsubscribe/token_test.go`

**Interfaces:**
- Produces: `func Sign(key []byte, tenantID, userID primitive.ObjectID, expires time.Time) string`; `func Verify(key []byte, token string, now time.Time) (tenantID, userID primitive.ObjectID, err error)` returning one sentinel `ErrInvalidToken` for every failure; `Config.MailUnsubscribeKey string` read from `MAIL_UNSUBSCRIBE_KEY`.

- [ ] **Step 1: Write failing tests:** `TestSignVerifyRoundTrip`; `TestVerifyRejectsExpired`; `TestVerifyRejectsTamperedPayloadAndSignature`; `TestVerifyUsesOneErrorForAllFailures` (`errors.Is(err, ErrInvalidToken)` for malformed, expired, bad signature); `TestVerifyRejectsWrongKey`; config test `TestNotifierEnabledRequiresUnsubscribeKey` (mail link configured and key empty gives a startup error naming `MAIL_UNSUBSCRIBE_KEY`). Build test keys at run time, not as literals that look like secrets.
- [ ] **Step 2: Run** `go test ./internal/unsubscribe ./internal/config -v`. Expected: FAIL.
- [ ] **Step 3: Implement** with `crypto/hmac` + `subtle.ConstantTimeCompare`, base64url token of `payload.signature`. Config validation fails when `TENANTCORE_URL` and `TENANTCORE_SERVICE_KEY` are set but the key is empty.
- [ ] **Step 4: Run** the gate. Expected: PASS.
- [ ] **Step 5: Commit** the touched files, message `feat(unsubscribe): signed token and MAIL_UNSUBSCRIBE_KEY config`.

---

### Task 4: Outbox model and repository

**Files:**
- Create: `digitalservice/internal/models/mail_outbox.go`, `internal/repository/mail_outbox_repo.go`
- Modify: `internal/repository/indexes.go` (indexes)
- Test: `internal/repository/mail_outbox_repo_test.go` following the pattern of `tenant_password_reset_repo_test.go` (read it first; use the same Mongo-free or skip-if-no-Mongo approach)

**Interfaces:**
- Produces:
  - `type MailStatus string` with `MailPending="pending"`, `MailSent="sent"`, `MailFailed="failed"`
  - `models.MailOutbox` with fields per the spec table: `ID, TenantID, UserID, To, Kind, RecordID, Data map[string]string, Status, Attempts int, NextAttemptAt, LastError, CreatedAt, UpdatedAt, SentAt *time.Time`
  - `(*MailOutboxRepo).Enqueue(ctx, rows []*models.MailOutbox) error`
  - `(*MailOutboxRepo).ClaimDue(ctx, now time.Time, lease time.Duration) (*models.MailOutbox, error)` returns `(nil, nil)` when none; atomically pushes `next_attempt_at` to `now+lease` and increments nothing else
  - `(*MailOutboxRepo).MarkSent(ctx, id primitive.ObjectID, now time.Time) error`
  - `(*MailOutboxRepo).MarkAttemptFailed(ctx, id primitive.ObjectID, errCode string, attempts int, nextAt time.Time, final bool) error`
  - `(*MailOutboxRepo).CancelPendingForUser(ctx, tenantID, userID primitive.ObjectID) (int64, error)`
  - `(*MailOutboxRepo).List(ctx, tenantID primitive.ObjectID, status MailStatus, page, limit int) ([]*models.MailOutbox, int64, error)`
  - `(*MailOutboxRepo).ResetFailed(ctx, tenantID, id primitive.ObjectID) error` sets `pending`, `attempts=0`, `next_attempt_at=now`; not-found if the row is not `failed` or is another tenant's
  - indexes: `{status, next_attempt_at}`, `{tenant_id, status, created_at}`, TTL on `created_at` 30 days

- [ ] **Step 1: Write failing tests:** `TestClaimDueIsExclusive` (two claims on one due row: second gets nil); `TestClaimDueSkipsFutureAndNonPending`; `TestResetFailedIsTenantScoped`; `TestCancelPendingForUserOnlyTouchesPending`; `TestListFiltersByTenantAndStatus`.
- [ ] **Step 2: Run** `go test ./internal/repository -run MailOutbox -v`. Expected: FAIL.
- [ ] **Step 3: Implement.** `ClaimDue` uses `FindOneAndUpdate` with filter `{status: pending, next_attempt_at: {$lte: now}}`, sorted by `next_attempt_at`.
- [ ] **Step 4: Run** the gate. Expected: PASS.
- [ ] **Step 5: Commit** touched files, message `feat(outbox): mail_outbox model, repo and indexes`.

---

### Task 5: `RequestNotifier` (enqueue) and `MailWorker`

**Files:**
- Create: `digitalservice/internal/service/request_notifier.go`, `internal/service/mail_worker.go`
- Test: `internal/service/request_notifier_test.go`, `internal/service/mail_worker_test.go`

**Interfaces:**
- Consumes: `FindEmailRecipients` (Task 2), `unsubscribe.Sign` (Task 3), `MailOutboxRepo` methods (Task 4), `notify.Client.Send`.
- Produces:
  - `type NotifyKind string` with `NotifyBooking="booking"`, `NotifyRental="car rental"`, `NotifyTransfer="airport transfer"`, `NotifyGuide="guide application"`
  - `type linkBuilder interface { Links(ctx context.Context, tenantID primitive.ObjectID, kind NotifyKind, recordID string) (adminURL, tenantName string, err error) }`
  - `func NewRequestNotifier(users recipientLister, store outboxEnqueuer, links linkBuilder, key []byte, appName string, now func() time.Time, log Logger) *RequestNotifier`
  - `func (n *RequestNotifier) Notify(ctx context.Context, tenantID primitive.ObjectID, kind NotifyKind, recordID, summary string)` nil-receiver safe, never returns an error, inserts rows only
  - `type mailSender interface { Send(ctx context.Context, to, template string, data map[string]string) error }`
  - `type outboxWorkStore interface` with `ClaimDue`, `MarkSent`, `MarkAttemptFailed`
  - `type userChecker interface { FindByID(ctx context.Context, tenantID, id primitive.ObjectID) (*models.TenantUser, error) }`
  - `func NewMailWorker(store outboxWorkStore, mail mailSender, users userChecker, now func() time.Time, log Logger) *MailWorker`
  - `func (w *MailWorker) Run(ctx context.Context)` polls every 15s until `ctx` is done; `func (w *MailWorker) Tick(ctx context.Context)` processes due rows once (test seam)
  - `func nextAttemptDelay(failures int) time.Duration` returning 1m, 5m, 30m, 2h for failures 1..4
  - `func errorCode(err error) string` mapping to the four codes in Global Constraints

- [ ] **Step 1: Write failing tests (notifier):** `TestNotifyEnqueuesOneRowPerRecipient`; `TestNotifyNoRecipientsOrNilIsNoOp`; `TestSummaryIsSingleLineAndCapped`; `TestDataKeysAreExactlyTemplateKeys` (`app, tenant, request_type, summary, admin_url, unsubscribe_url`); `TestUnsubscribeURLCarriesVerifiableToken` (Verify returns the same tenant and user).
- [ ] **Step 2: Write failing tests (worker):** `TestTickSendsDueRowAndMarksSent`; `TestTickRetriesWithBackoff` (failures 1..4 reschedule at the right delays, attempts increment); `TestTickFailsAfterFifthAttempt` (`final=true`); `TestTickSkipsUnsubscribedOrSuspendedUser` (row marked failed or cancelled with no `Send`, per the cancellation rule below); `TestMailNotConfiguredRetries` (`last_error=mail_not_configured`); `TestLastErrorNeverContainsProviderText`; `TestNilMailSenderLeavesRowsPending`.
- [ ] **Step 3: Run** `go test ./internal/service -run 'Notif|MailWorker|NextAttempt' -v`. Expected: FAIL, undefined.
- [ ] **Step 4: Implement.** `Notify` collapses whitespace and truncates by runes, then builds rows with `status=pending`, `next_attempt_at=now`. Before sending, the worker re-reads the user with `userChecker`; if not active or `receive_emails=false`, mark the row `failed` with `last_error=recipient_opted_out` and do not send (add that fifth code to `errorCode` constants and the list in Global Constraints is a superset, not a contradiction). Run `Tick` loop with a per-send `context.WithTimeout(ctx, 30*time.Second)`.
- [ ] **Step 5: Run** the gate. Expected: PASS.
- [ ] **Step 6: Commit** the four files, message `feat(notify): outbox-backed RequestNotifier and MailWorker`.

---

### Task 6: Wire the notifier into the four create paths

**Files:**
- Modify: `internal/service/booking_service.go`, `rental_service.go`, `airport_transfer_service.go`, `guide_application_service.go`, `internal/bootstrap/wiring.go`, `internal/bootstrap/bootstrap.go` (start the worker, stop it in `App.Close`)
- Test: `internal/service/guide_application_service_test.go` (existing), `internal/bootstrap/wiring_test.go`

**Interfaces:**
- Consumes: `RequestNotifier.Notify`, `MailWorker.Run` from Task 5.
- Produces: each of the four services accepts an optional `requestNotifier` interface (`Notify(ctx, tenantID, kind, recordID, summary)`); nil means off.

- [ ] **Step 1: Write failing tests:** `TestSubmitNotifiesAfterSuccessfulWrite` and `TestSubmitDoesNotNotifyOnValidationError` with a fake notifier in the guide tests; extend `wiring_test.go` with `TestNotifierIsNilSafeWhenTenantcoreUnset` and `TestWorkerStartedAndStoppedWithApp`. Add the same pair for booking, rental and transfer where a fakeable seam exists; otherwise say so in the commit message.
- [ ] **Step 2: Run** `go test ./internal/service ./internal/bootstrap -run 'Notif|Wiring' -v`. Expected: FAIL.
- [ ] **Step 3: Implement.** Call `Notify` only after the successful repo write, with a summary of customer or applicant name plus date or route, no contact details. Reuse the single `notify.Client` already built for password reset. Cancel the worker context in `App.Close` and wait for `Run` to return.
- [ ] **Step 4: Run** the gate. Expected: PASS.
- [ ] **Step 5: Commit** touched files, message `feat(notify): enqueue staff mail on booking, rental, transfer and guide application`.

---

### Task 7: Public unsubscribe route

**Files:**
- Create: `digitalservice/internal/api/tenant/public/unsubscribe.go`
- Modify: `internal/api/tenant/public/public.go` (route), `internal/api/guard_test.go` (`publicRoutes` entry with a written justification)
- Test: `internal/api/tenant/public/unsubscribe_test.go`

**Interfaces:**
- Consumes: `unsubscribe.Verify` (Task 3), `TenantUserRepo.SetReceiveEmails` (Task 2), `MailOutboxRepo.CancelPendingForUser` (Task 4).
- Produces: `GET /api/v1/public/unsubscribe?token=` returns a small HTML confirmation page with a form that POSTs; `POST /api/v1/public/unsubscribe` accepts the token in the form body or JSON and performs the change; both rate limited with the repo's public-write limiter keyed on the real visitor.

- [ ] **Step 1: Write failing tests:** `TestGetDoesNotChangeAnything` (user stays opted in, rows stay pending); `TestPostUnsubscribesAndCancelsPending`; `TestInvalidTokensAreIdentical` (malformed, expired, tampered, unknown user: same status, same body bytes); `TestPostAcceptsJSONBody`; `TestUnsubscribeIsRateLimited`; `TestUnsubscribeOnlyAffectsTokenTenantUser`.
- [ ] **Step 2: Run** `go test ./internal/api/... -run Unsubscribe -v`. Expected: FAIL.
- [ ] **Step 3: Implement.** The HTML page contains no tenant data and no token echo beyond a hidden form field. Add the `guard_test.go` justification: unauthenticated by necessity (recipient has no session), changes only the signed user's flag, token-bound, rate limited.
- [ ] **Step 4: Run** the gate. Expected: PASS, including `guard_test.go`.
- [ ] **Step 5: Commit** touched files, message `feat(unsubscribe): public one-click unsubscribe route`.

---

### Task 8: Mail log API

**Files:**
- Create: `digitalservice/internal/service/mail_outbox_service.go`, `internal/api/tenant/private/mail_outbox.go`
- Modify: `internal/api/tenant/private/private.go` (routes)
- Test: `internal/service/mail_outbox_service_test.go`, `internal/api/tenant/private/mail_outbox_test.go`

**Interfaces:**
- Consumes: `MailOutboxRepo.List`, `ResetFailed` (Task 4).
- Produces: `GET /admin/mail-outbox?status=&page=&limit=` (items expose `id, kind, record_id, to, status, attempts, last_error, created_at, sent_at`; `to` is masked as `a***@domain`); `POST /admin/mail-outbox/:id/retry` admin role only; `func (s *MailOutboxService) List(ctx, tenantID, status string, page, limit int)` and `Retry(ctx, tenantID primitive.ObjectID, id string)`.

- [ ] **Step 1: Write failing tests:** `TestListRejectsUnknownStatus` (400); `TestListIsTenantScoped`; `TestRetryResetsOnlyFailedRows`; `TestRetryRequiresAdminRole` (staff gets 403); `TestListMasksRecipient`.
- [ ] **Step 2: Run** `go test ./internal/service ./internal/api/tenant/private -run MailOutbox -v`. Expected: FAIL.
- [ ] **Step 3: Implement** following the existing `ClampPage` helper and role middleware used by other admin-only routes.
- [ ] **Step 4: Run** the gate. Expected: PASS.
- [ ] **Step 5: Commit** touched files, message `feat(outbox): admin mail log and manual retry`.

---

### Task 9: Customer avatar, review `customer_id`, `POST /admin/customers`

**Files:**
- Modify: `internal/models/customer.go`, `internal/models/review.go`, `internal/service/customer_service.go`, `internal/service/review_service.go`, `internal/api/tenant/private/operations.go` and `private.go` (route), the public review read in `internal/api/tenant/public/storefront.go`, `internal/dto/review.go`
- Test: `internal/service/customer_service_test.go`, `internal/service/review_service_test.go` (create if absent)

**Interfaces:**
- Produces: `Customer.AvatarURL string` (`bson:"avatar_url" json:"avatar_url"`); `Review.CustomerID *primitive.ObjectID` (`bson:"customer_id,omitempty" json:"customer_id,omitempty"`); `(*CustomerService).CreateManual(ctx, tenantID primitive.ObjectID, c *models.Customer, avatar io.Reader, userID *primitive.ObjectID) (*models.Customer, error)`; public review JSON adds `customer_avatar`; route `POST /admin/customers` (multipart: `name, email, phone, nationality, avatar`).
- Consumes: `UploadService.Upload(ctx, file io.Reader, tenantID)` returning `*UploadResult` (read its URL field name first).

- [ ] **Step 1: Write failing tests:** `TestCreateManualStoresAvatarURL`; `TestCreateManualRejectsNonImageAvatar` (PDF bytes, and `MZ` exe bytes with an image Content-Type: 4xx, no customer created); `TestCreateManualWorksWithoutAvatar`; `TestReviewCreateRejectsCustomerFromOtherTenant`; `TestReviewCreateLinksCustomerAndFillsRelatedCustomer`; `TestPublicReviewIncludesCustomerAvatar`.
- [ ] **Step 2: Run** `go test ./internal/service ./internal/api/... -run 'CreateManual|CustomerAvatar|CustomerFromOther' -v`. Expected: FAIL.
- [ ] **Step 3: Implement.** Sniff the avatar head with `http.DetectContentType`, accept only `image/jpeg` and `image/png`, enforce `UploadService`'s size cap. `Review.Create` verifies `CustomerID` with `customerRepo.FindByID(tenantID, id)` and fills `related_customer` from the customer when empty. The public read resolves avatars in one `FindByIDs` batch. Add the route to any OpenAPI or guard list the tests require.
- [ ] **Step 4: Run** the gate. Expected: PASS.
- [ ] **Step 5: Commit** touched files, message `feat(reviews): create customer with avatar and link it to a review`.

---

### Task 10: Guide file disposition (backend)

**Files:**
- Modify: `internal/service/private_file_service.go` (`PrivateFiles` interface and `DownloadURL`), `internal/service/guide_application_service.go` (`FileDownload`), `internal/api/tenant/private/guide_applications.go` (`FileLink`), and every test fake implementing `PrivateFiles`
- Test: `internal/service/private_file_service_test.go`, `internal/service/guide_application_service_test.go`, `internal/api/tenant/private/guide_applications_test.go`

**Interfaces:**
- Produces: `type Disposition string` with `DispositionInline="inline"`, `DispositionAttachment="attachment"`; `DownloadURL(publicID, mime string, ttl time.Duration, d Disposition) (string, time.Time, error)`; `FileDownload(ctx, tenantID primitive.ObjectID, idHex, fileID string, d Disposition) (string, time.Time, error)`; `GET /admin/guide-applications/:id/files/:fileId?disposition=inline|attachment`, default `attachment`. Inline omits the `attachment` param from the signed params and uses a 5-minute TTL; attachment keeps today's params and `guideDownloadTTL`.

- [ ] **Step 1: Write failing tests:** `TestDownloadURLInlineOmitsAttachmentParam` (parse the URL: no `attachment` key and the `signature` differs from the attachment one); `TestDownloadURLAttachmentUnchanged`; `TestInlineUsesFiveMinuteTTL`; `TestFileLinkRejectsUnknownDisposition` (400); `TestFileLinkDefaultsToAttachment`; `TestFileLinkOtherTenantFileIs404`.
- [ ] **Step 2: Run** `go test ./internal/service ./internal/api/tenant/private -run 'Disposition|DownloadURL|FileLink' -v`. Expected: FAIL.
- [ ] **Step 3: Implement.** Validate `disposition` in the handler. The signature covers exactly the params sent, so the inline param map must not contain `attachment`.
- [ ] **Step 4: Run** the gate. Expected: PASS.
- [ ] **Step 5: Commit** touched files, message `feat(guides): inline preview URLs for applicant files`.

---

### Task 11: admin UI

**Files:**
- Modify: `admin/src/components/admin/ReviewForm.tsx`, `admin/src/app/(dashboard)/reviews/actions.ts`, `admin/src/lib/data/guide-applications.ts` (`guideFileLink` takes a disposition), `admin/src/app/(dashboard)/guide-applications/actions.ts`, `admin/src/components/admin/GuideFileLink.tsx`, `admin/src/lib/types.ts`, the staff-user form (find it with `grep -rn "role" "admin/src/app/(dashboard)"`), the sidebar nav
- Create: `admin/src/components/admin/NewCustomerFields.tsx`, `admin/src/components/admin/GuideFilePreview.tsx`, `admin/src/app/(dashboard)/mail-log/page.tsx`, `admin/src/app/(dashboard)/mail-log/actions.ts`, `admin/src/lib/data/mail-outbox.ts`

**Interfaces:**
- Consumes: Task 9 `POST /admin/customers` (multipart), Task 10 `?disposition=`, Task 2 `receive_emails`, Task 8 mail log endpoints.
- Produces: `openGuideFileAction(id, fileId, disposition: 'inline' | 'attachment')`; `createCustomerWithAvatarAction(formData): Promise<{ id: string; name: string }>`; `retryMailAction(id: string)`.

- [ ] **Step 1: Write failing tests** where `admin` has a runner (check `package.json`; if none, use `node --test` for pure helpers such as `isPreviewableMime` and `mailStatusLabel`, where `sent` maps to "Accepted"). If no runner exists, say so in the report and verify with the build and the browser checks in Step 4.
- [ ] **Step 2: Implement.** Review form: an "Add new customer" toggle reveals `NewCustomerFields` (name, email, phone, nationality, image input accepting `image/jpeg,image/png`); submit creates the customer first, then the review with `customer_id`. Guide page: jpeg/png files render a thumbnail fetched with `inline` and a lightbox on click, PDFs show an icon, every file has a Download button using `attachment`, the CV's styled as primary. Staff form: a "Receives request emails" checkbox bound to `receive_emails`. Mail log page: status filter (Pending, Accepted, Failed), table of rows, Retry button on failed rows, hidden from non-admin roles.
- [ ] **Step 3: Run** `cd admin && npm run build && npm run lint`. Expected: no errors.
- [ ] **Step 4: Verify in the browser** with `preview_start` if a dev config exists: the review form creates a customer and a linked review; a guide application with an image shows a thumbnail and the CV has a Download button; the mail log lists rows. If digitalservice, Mongo or Cloudinary is unavailable, say so and report those checks as unverified live.
- [ ] **Step 5: Commit** touched files in the admin repo, message `feat(admin): mail log, review customers, guide file preview, receive_emails toggle`.

---

## Self-review notes

- Spec coverage: §1 → Task 1; §2 → Task 2; §3 → Tasks 4, 5, 6; §4 → Tasks 3, 7; §5 → Tasks 8, 11; §6 → Tasks 9, 11; §7 → Tasks 10, 11; "not verifiable here" → Tasks 6 and 11 report live checks as untested.
- One addition beyond the spec text: the `recipient_opted_out` error code (Task 5). It implements the spec rule "the worker re-checks `receive_emails` just before sending".
- Open item: site host and public proxy (see "Open items" above).
