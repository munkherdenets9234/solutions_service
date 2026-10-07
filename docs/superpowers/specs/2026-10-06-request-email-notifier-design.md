# Request email notifier, review customers, guide file preview — design

Date: 2026-10-06 (revised same day: outbox, retry, mail status, unsubscribe added).
Status: design approved in chat; revised spec awaiting review.
Repos: digitalservice (main), tenantcore (one template), admin (UI).

## Goal

1. When the storefront creates a booking, car rental, airport transfer or guide
   application, email every tenant user whose `receive_emails` flag is true.
   Mail is delivered by tenantcore, the platform's only sender.
2. Every send is recorded, retried on failure, and visible to staff with a status.
3. Every mail carries a one-click unsubscribe link.
4. On the admin new-review page, let staff add a customer manually, with a
   profile image. This creates a real `Customer` record.
5. On the admin guide-application page, preview applicant images inline and
   offer a download button on every file, the CV included.

## Constraints

- Products hold no mail credentials. digitalservice names a template and passes
  data to `POST /api/v1/svc/notifications/email` through `internal/notify`.
- Templates take fixed fields. No visitor free text becomes a body, subject or From.
- A mail failure never fails or delays the visitor's request.
- Tenant scoping on every read and write. Storage public ids never leave the service.
- "Sent" means tenantcore's relay accepted the message. SMTP cannot confirm
  delivery, so the UI says "accepted", never "delivered".

## Chosen approach: transactional outbox in digitalservice

The product owns the flow; tenantcore only delivers. After a successful create,
the service inserts one `mail_outbox` row per recipient and returns. A worker
loop sends due rows and retries with backoff. A goroutine-only design was
rejected: it cannot retry across restarts and records no status.

## 1. tenantcore

New template `request_notification`.

- Required keys: `app`, `tenant`, `request_type`, `summary`, `admin_url`,
  `unsubscribe_url`.
- Every value is capped at 256 runes by the caller, except `unsubscribe_url`,
  which is capped at 512.
- Tests: template registered, missing key is an error naming the key,
  `openapi_test.go` stays green.

## 2. Recipients

- `TenantUser.ReceiveEmails bool` (`bson:"receive_emails"`, default false),
  settable through the existing tenant-user create and update endpoints.
- Recipient lookup: active users of the tenant with `receive_emails=true`.

## 3. Outbox

Collection `mail_outbox`, tenant-scoped.

| Field | Meaning |
|---|---|
| `tenant_id`, `user_id` | whose mail |
| `to` | recipient address, copied at enqueue time |
| `kind` | `booking`, `car rental`, `airport transfer`, `guide application` |
| `record_id` | the booking, rental, transfer or application id |
| `data` | the template data map (fixed fields, capped) |
| `status` | `pending`, `sent`, `failed` |
| `attempts` | count of send attempts |
| `next_attempt_at` | when the worker may try again |
| `last_error` | short code only (`mail_not_configured`, `rate_limited`, `upstream`, `unreachable`); never provider text |
| `created_at`, `updated_at`, `sent_at` | timestamps |

- `RequestNotifier.Notify(tenantID, kind, recordID, summary)` looks up recipients,
  inserts rows with `status=pending`, `next_attempt_at=now`, and returns. It
  does no network call. It is called only after a successful write and never
  changes the response. Nil notifier, mail not configured or no recipients: no
  rows, not an error.
- Worker loop (started in bootstrap, stopped in `App.Close`): polls every 15s,
  claims due `pending` rows atomically (`findOneAndUpdate` setting a short lease
  in `next_attempt_at`, so two instances never send the same row), and sends
  through `notify.Client.Send`.
- Retry policy: 5 attempts in total. After a failure the next attempt is at
  +1m, +5m, +30m, +2h. After the fifth failure, `status=failed`.
  `ErrMailNotConfigured` still counts as a failure and retries, so mail sent
  after tenantcore gets credentials still goes out.
- Rows older than 30 days are deleted (TTL index on `created_at`).
- Manual retry: staff can reset a `failed` row to `pending` with `attempts=0`.
- The address is stored on the row, so a later change of a user's email does
  not redirect a queued message.

## 4. Unsubscribe

- Every mail has `unsubscribe_url`: `<site host>/api/v1/public/unsubscribe?token=...`.
- Token is an HMAC-SHA256 signature over `tenant_id`, `user_id` and an expiry,
  90 days. Key: new secret `MAIL_UNSUBSCRIBE_KEY` in digitalservice config. It
  is required whenever the notifier is enabled. Startup fails without it, and
  no default value is invented.
- `GET` shows a confirmation page; `POST` sets `receive_emails=false` for that
  user and also cancels that user's `pending` outbox rows. A mail scanner that
  prefetches the `GET` therefore cannot unsubscribe anyone.
- One-click for mail clients: the same `POST` endpoint also accepts the token
  in the body (RFC 8058 style).
- All-or-nothing: unsubscribe turns off all request emails for that person.
  Staff re-enable it with the `receive_emails` toggle in admin.
- Public route: rate limited, constant-time token comparison, identical
  response for every invalid token (bad signature, expired, unknown user). A
  written justification is added to `guard_test.go`'s `publicRoutes`.
- The recipient filter and the worker re-check `receive_emails` just before
  sending, so an unsubscribe wins over a queued row.

## 5. Mail log (admin)

- `GET /admin/mail-outbox?status=&page=&limit=` lists rows for the caller's
  tenant: kind, record id, recipient, status, attempts, last error code, times.
- `POST /admin/mail-outbox/:id/retry` resets a `failed` row. Admin role only.
- Admin page "Mail log" shows the list with a status filter and a Retry button
  on failed rows. Status labels: Pending, Accepted, Failed.

## 6. Reviews with a new customer

- `Customer.AvatarURL string`. `Review.CustomerID *ObjectID` (optional).
  `related_customer` stays as the display name so the storefront keeps working.
- `POST /customers` (admin) creates a customer with an optional avatar through
  `UploadService`: jpeg or png only, size cap, type sniffed from bytes.
- Admin new-review form: "Add new customer" with name, email, phone,
  nationality and image. It creates the customer, then submits the review linked to it.
- Public review read adds `customer_avatar`.
- Tests: non-image avatar rejected; tenant scoping; review links to the new
  customer; a `customer_id` from another tenant is rejected.

## 7. Guide file preview and download

- `PrivateFileService.DownloadURL` and `GuideApplicationService.FileDownload`
  take a disposition, `inline` or `attachment`. Preview uses `inline` with a
  5-minute TTL. Auth, tenant scope and signing are unchanged.
- Admin route `GET .../files/:fileId?disposition=inline|attachment` returns the signed URL.
- `GuideFileLink`: jpeg/png show a thumbnail; click opens a lightbox. PDFs show a
  file icon. Every file has a Download button; the CV's is the prominent one.
- Tests: disposition validated; cross-tenant file id returns 404.

## Out of scope

Per-type unsubscribe, bounce handling, delivery receipts, mail on status
changes, and any change to tenantcore beyond the one template.

## Not verifiable here

Real Gmail delivery (tenantcore's App Password is still unset) and live
Cloudinary inline delivery. Both are covered by fakes and reported as untested
live. The outbox claim and lease logic is tested against fakes; a live
multi-instance run against MongoDB is not part of this work.
