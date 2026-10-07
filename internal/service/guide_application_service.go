package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	guideSeason          = "summer-2027"
	guideDuplicateWindow = 24 * time.Hour
)

// guideStore is the persistence seam. GuideApplicationRepo satisfies it; tests
// use a fake.
type guideStore interface {
	Create(ctx context.Context, tenantID primitive.ObjectID, a *models.GuideApplication) error
	FindByID(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (*models.GuideApplication, error)
	List(ctx context.Context, tenantID primitive.ObjectID, f repository.GuideListFilter, page, limit int) ([]*models.GuideApplication, int64, error)
	CountByStatus(ctx context.Context, tenantID primitive.ObjectID) (map[models.GuideStatus]int64, error)
	HasRecentByEmail(ctx context.Context, tenantID primitive.ObjectID, season, email string, since time.Time) (bool, error)
	SetStatus(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, status models.GuideStatus, ev models.GuideEvent) error
	AddNote(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, ev models.GuideEvent) error
}

// guideUsers resolves staff display names for the timeline.
type guideUsers interface {
	FindByIDs(ctx context.Context, tenantID primitive.ObjectID, ids []primitive.ObjectID) ([]*models.TenantUser, error)
}

var (
	_ PrivateFiles = (*PrivateFileService)(nil)
	_ guideStore   = (*repository.GuideApplicationRepo)(nil)
)

type GuideApplicationService struct {
	store guideStore
	files PrivateFiles
	users guideUsers
	now   func() time.Time
	locks keyedLock
	// notifier is nil when staff mail is off.
	notifier requestNotifier
}

// WithNotifier sets the staff-mail notifier. Pass an untyped nil to turn it off.
func (s *GuideApplicationService) WithNotifier(n requestNotifier) { s.notifier = n }

// keyedLock is a mutex per key; idle entries are removed.
type keyedLock struct {
	mu sync.Mutex
	m  map[string]*lockEntry
}

type lockEntry struct {
	mu   sync.Mutex
	refs int
}

func (k *keyedLock) Lock(key string) func() {
	k.mu.Lock()
	if k.m == nil {
		k.m = map[string]*lockEntry{}
	}
	e := k.m[key]
	if e == nil {
		e = &lockEntry{}
		k.m[key] = e
	}
	e.refs++
	k.mu.Unlock()
	e.mu.Lock()
	return func() {
		e.mu.Unlock()
		k.mu.Lock()
		if e.refs--; e.refs == 0 {
			delete(k.m, key)
		}
		k.mu.Unlock()
	}
}

func NewGuideApplicationService(store guideStore, files PrivateFiles, users guideUsers, now func() time.Time) *GuideApplicationService {
	if now == nil {
		now = time.Now
	}
	return &GuideApplicationService{store: store, files: files, users: users, now: now}
}

// GuideUpload is one file offered with an application. Open may be called
// more than once (a type check before any upload, then the upload itself), so
// it must return a fresh reader each time.
type GuideUpload struct {
	Kind         models.GuideFileKind
	OriginalName string
	Open         func() (io.ReadCloser, error)
}

type SubmitResult struct {
	ID             string
	ConfirmationID string
}

// Submit validates and stores a public application with its private files.
// Nothing is stored unless every step succeeds; uploaded files are removed on
// any later failure. Application content and file names are never logged.
func (s *GuideApplicationService) Submit(ctx context.Context, tenantID primitive.ObjectID, a *models.GuideApplication, uploads []GuideUpload) (*SubmitResult, error) {
	if s.files == nil || !s.files.Available() {
		return nil, apierr.FeatureUnavailable("document uploads")
	}
	now := s.now()
	if err := ValidateGuideApplication(a, now); err != nil {
		return nil, err
	}
	// The consent record is stamped by the server; the client value only had
	// to be present.
	a.ConsentAt = now
	kinds := make([]GuideUploadMeta, len(uploads))
	for i, u := range uploads {
		kinds[i] = GuideUploadMeta{Kind: u.Kind}
	}
	if err := ValidateGuideFiles(kinds); err != nil {
		return nil, err
	}

	// Check every file's type before anything is uploaded, so a bad third file
	// cannot leave the first two stored remotely.
	for _, u := range uploads {
		if err := s.sniffUpload(u); err != nil {
			return nil, err
		}
	}

	email := NormalizeEmail(a.Personal.Email)

	// Serialise duplicate check -> uploads -> Create per (tenant, email) so a
	// double click or retry cannot insert twice. The guarantee is per process
	// only: a second API instance could still race, and a unique index cannot
	// express a 24h window.
	unlock := s.locks.Lock(tenantID.Hex() + "/" + email)
	defer unlock()

	dup, err := s.store.HasRecentByEmail(ctx, tenantID, guideSeason, email, now.Add(-guideDuplicateWindow))
	if err != nil {
		return nil, apierr.Internal(err)
	}
	if dup {
		return nil, apierr.Conflict("an application from this email was already received")
	}

	var stored []models.GuideFile
	// rollback removes uploaded files and returns the failures, which are
	// attached to the returned error's internal cause (logged, never sent to
	// the client).
	rollback := func() []error {
		cctx := context.WithoutCancel(ctx)
		var errs []error
		for _, f := range stored {
			if err := s.files.Delete(cctx, f.PublicID, f.Mime); err != nil {
				errs = append(errs, fmt.Errorf("delete %s: %w", f.PublicID, err))
			}
		}
		return errs
	}

	for _, u := range uploads {
		sf, err := s.uploadOne(ctx, tenantID, u)
		if err != nil {
			// An object created remotely before a timeout error has no public id
			// to delete; accepted.
			return nil, withCleanupErrors(err, rollback())
		}
		stored = append(stored, models.GuideFile{
			ID:           primitive.NewObjectID().Hex(),
			Kind:         u.Kind,
			PublicID:     sf.PublicID,
			Mime:         sf.Mime,
			Size:         sf.Size,
			OriginalName: u.OriginalName,
		})
	}

	real := make([]GuideUploadMeta, len(stored))
	for i, f := range stored {
		real[i] = GuideUploadMeta{Kind: f.Kind, Mime: f.Mime, Size: f.Size}
	}
	if err := ValidateGuideFiles(real); err != nil {
		return nil, withCleanupErrors(err, rollback())
	}

	a.Personal.Email = email
	a.Season = guideSeason
	a.Files = stored
	if err := s.store.Create(ctx, tenantID, a); err != nil {
		return nil, withCleanupErrors(apierr.Internal(err), rollback())
	}

	hexID := a.ID.Hex()
	if s.notifier != nil {
		// Name and season only: no phone, email or notes.
		s.notifier.Notify(ctx, tenantID, NotifyGuide, hexID, notifySummary(a.Personal.FullName, "season", time.Time{})+" ("+guideSeason+")")
	}
	return &SubmitResult{ID: hexID, ConfirmationID: "GA-" + upperTail(hexID, 6)}, nil
}

// sniffUpload reads the first bytes of a file and checks its real type. The
// result names the file (files.<kind>) so the site can mark the right row.
func (s *GuideApplicationService) sniffUpload(u GuideUpload) error {
	rc, err := u.Open()
	if err != nil {
		return namedFileError(u.Kind, apierr.BadRequest(msgCouldNotReadFile))
	}
	defer rc.Close()
	head := make([]byte, 512)
	n, err := io.ReadFull(rc, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return namedFileError(u.Kind, apierr.BadRequest(msgCouldNotReadFile))
	}
	if n == 0 {
		return namedFileError(u.Kind, apierr.BadRequest(msgFileEmpty))
	}
	if _, ok := SniffPrivateType(head[:n]); !ok {
		return namedFileError(u.Kind, apierr.ValidationFailed(msgUnsupportedType))
	}
	return nil
}

func (s *GuideApplicationService) uploadOne(ctx context.Context, tenantID primitive.ObjectID, u GuideUpload) (*StoredFile, error) {
	rc, err := u.Open()
	if err != nil {
		return nil, namedFileError(u.Kind, apierr.BadRequest(msgCouldNotReadFile))
	}
	defer rc.Close()
	sf, err := s.files.Upload(ctx, rc, tenantID)
	if err != nil {
		var ae *apierr.APIError
		if errors.As(err, &ae) {
			return nil, namedFileError(u.Kind, ae)
		}
		return nil, apierr.Upstream(apierr.DomainUpload, err)
	}
	return sf, nil
}

// namedFileError re-wraps a client-facing (400/422) file error as a 422 whose
// message starts with files.<kind>: and keeps the same safe text. Other errors
// (upstream failures) pass through unchanged.
func namedFileError(kind models.GuideFileKind, err error) error {
	var ae *apierr.APIError
	if !errors.As(err, &ae) {
		return err
	}
	if ae.HTTPStatus != http.StatusBadRequest && ae.HTTPStatus != http.StatusUnprocessableEntity {
		return err
	}
	if strings.HasPrefix(ae.Message, "files.") {
		return err
	}
	return apierr.ValidationFailed("files." + string(kind) + ": " + ae.Message)
}

// withCleanupErrors attaches failed cleanup deletes to err's internal cause so
// the error handler logs them. The client-facing status and message do not
// change.
func withCleanupErrors(err error, cleanup []error) error {
	if len(cleanup) == 0 {
		return err
	}
	var ae *apierr.APIError
	if errors.As(err, &ae) {
		cp := *ae
		cp.Err = errors.Join(append([]error{ae.Err}, cleanup...)...)
		return &cp
	}
	return apierr.Internal(errors.Join(append([]error{err}, cleanup...)...))
}

func upperTail(s string, n int) string {
	if len(s) > n {
		s = s[len(s)-n:]
	}
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

const (
	guideDownloadTTL = 5 * time.Minute
	guidePreviewTTL  = 5 * time.Minute
	guideNoteMaxLen  = 2000
	guideStaffName   = "staff"
)

// ClampPage returns the page and limit List actually uses: page at least 1,
// limit 1..100 (anything else becomes 20). Controllers report these values.
func ClampPage(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return page, limit
}

const guideSearchMaxRunes = 100

// List returns one page of applications for the tenant, newest first. The
// search text is cut to 100 characters.
func (s *GuideApplicationService) List(ctx context.Context, tenantID primitive.ObjectID, f repository.GuideListFilter, page, limit int) ([]*models.GuideApplication, int64, error) {
	page, limit = ClampPage(page, limit)
	if r := []rune(f.Q); len(r) > guideSearchMaxRunes {
		f.Q = string(r[:guideSearchMaxRunes])
	}
	rows, total, err := s.store.List(ctx, tenantID, f, page, limit)
	if err != nil {
		return nil, 0, apierr.Internal(err)
	}
	return rows, total, nil
}

// Counts returns the number of applications per status, all five keys present.
func (s *GuideApplicationService) Counts(ctx context.Context, tenantID primitive.ObjectID) (map[models.GuideStatus]int64, error) {
	got, err := s.store.CountByStatus(ctx, tenantID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make(map[models.GuideStatus]int64, len(models.GuideStatuses))
	for _, st := range models.GuideStatuses {
		out[st] = got[st]
	}
	return out, nil
}

// Get loads one application. A malformed id and an id from another tenant are
// both NotFound so ids cannot be probed.
func (s *GuideApplicationService) Get(ctx context.Context, tenantID primitive.ObjectID, idHex string) (*models.GuideApplication, error) {
	id, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return nil, apierr.NotFound("guide application")
	}
	a, err := s.store.FindByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apierr.NotFound("guide application")
		}
		return nil, apierr.Internal(err)
	}
	return a, nil
}

// SetStatus moves an application to a new status and records who did it. A
// status equal to the current one succeeds without writing anything.
//
// The current status is read first and the write is not conditional on it, so
// two concurrent changes can record a stale "from" and both events are kept.
// The last write wins for the status, as the spec says.
func (s *GuideApplicationService) SetStatus(ctx context.Context, tenantID primitive.ObjectID, idHex string, status models.GuideStatus, actor *primitive.ObjectID) error {
	valid := false
	for _, st := range models.GuideStatuses {
		if st == status {
			valid = true
			break
		}
	}
	if !valid {
		return apierr.ValidationFailed("status is not a valid status")
	}
	if actor == nil {
		return apierr.Unauthorized("staff identity required")
	}
	a, err := s.Get(ctx, tenantID, idHex)
	if err != nil {
		return err
	}
	if a.Status == status {
		return nil
	}
	ev := models.GuideEvent{
		Type:     "status",
		At:       s.now(),
		UserID:   actor,
		UserName: s.actorName(ctx, tenantID, *actor),
		From:     string(a.Status),
		To:       string(status),
	}
	if err := s.store.SetStatus(ctx, tenantID, a.ID, status, ev); err != nil {
		return s.mapWriteErr(err)
	}
	return nil
}

// AddNote appends a staff note to the timeline. Events are never edited or removed.
func (s *GuideApplicationService) AddNote(ctx context.Context, tenantID primitive.ObjectID, idHex, text string, actor *primitive.ObjectID) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return apierr.ValidationFailed("text is required")
	}
	if utf8.RuneCountInString(text) > guideNoteMaxLen {
		return apierr.ValidationFailed("text must be at most 2000 characters")
	}
	if actor == nil {
		return apierr.Unauthorized("staff identity required")
	}
	a, err := s.Get(ctx, tenantID, idHex)
	if err != nil {
		return err
	}
	ev := models.GuideEvent{
		Type:     "note",
		At:       s.now(),
		UserID:   actor,
		UserName: s.actorName(ctx, tenantID, *actor),
		Text:     text,
	}
	if err := s.store.AddNote(ctx, tenantID, a.ID, ev); err != nil {
		return s.mapWriteErr(err)
	}
	return nil
}

// FileDownload returns a short-lived signed URL for one stored file. The
// storage public id never leaves the service.
func (s *GuideApplicationService) FileDownload(ctx context.Context, tenantID primitive.ObjectID, idHex, fileID string, d Disposition) (string, time.Time, error) {
	ttl := guideDownloadTTL
	switch d {
	case DispositionAttachment:
	case DispositionInline:
		ttl = guidePreviewTTL
	default:
		return "", time.Time{}, apierr.BadRequest("invalid disposition")
	}
	a, err := s.Get(ctx, tenantID, idHex)
	if err != nil {
		return "", time.Time{}, err
	}
	var file *models.GuideFile
	for i := range a.Files {
		if a.Files[i].ID == fileID {
			file = &a.Files[i]
			break
		}
	}
	if file == nil {
		return "", time.Time{}, apierr.NotFound("file")
	}
	// Only images are rendered by the browser; a PDF is never served inline.
	if d == DispositionInline && file.Mime != "image/jpeg" && file.Mime != "image/png" {
		return "", time.Time{}, apierr.BadRequest("only images can be previewed")
	}
	if s.files == nil || !s.files.Available() {
		return "", time.Time{}, apierr.FeatureUnavailable("document downloads")
	}
	url, exp, err := s.files.DownloadURL(file.PublicID, file.Mime, ttl, d)
	if err != nil {
		var ae *apierr.APIError
		if errors.As(err, &ae) {
			return "", time.Time{}, err
		}
		return "", time.Time{}, apierr.Upstream(apierr.DomainUpload, err)
	}
	return url, exp, nil
}

// actorName resolves the staff display name at write time so the timeline
// survives a later rename.
func (s *GuideApplicationService) actorName(ctx context.Context, tenantID, actor primitive.ObjectID) string {
	if s.users == nil {
		return guideStaffName
	}
	users, err := s.users.FindByIDs(ctx, tenantID, []primitive.ObjectID{actor})
	if err != nil {
		return guideStaffName
	}
	for _, u := range users {
		if u.ID == actor && strings.TrimSpace(u.Name) != "" {
			return u.Name
		}
	}
	return guideStaffName
}

func (s *GuideApplicationService) mapWriteErr(err error) error {
	if errors.Is(err, mongo.ErrNoDocuments) {
		return apierr.NotFound("guide application")
	}
	return apierr.Internal(err)
}
