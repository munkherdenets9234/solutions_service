package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// ── fakes ────────────────────────────────────────────────────────────────

type fakeGuideStore struct {
	rows      map[primitive.ObjectID]*models.GuideApplication
	now       func() time.Time
	createErr error
}

func newFakeGuideStore(now func() time.Time) *fakeGuideStore {
	return &fakeGuideStore{rows: map[primitive.ObjectID]*models.GuideApplication{}, now: now}
}

func (f *fakeGuideStore) Create(_ context.Context, t primitive.ObjectID, a *models.GuideApplication) error {
	if f.createErr != nil {
		return f.createErr
	}
	a.ID = primitive.NewObjectID()
	a.TenantID = t
	a.Status = models.GuideNew
	a.CreatedAt = f.now()
	a.UpdatedAt = a.CreatedAt
	a.Events = []models.GuideEvent{}
	f.rows[a.ID] = a
	return nil
}

func (f *fakeGuideStore) FindByID(_ context.Context, t, id primitive.ObjectID) (*models.GuideApplication, error) {
	if a, ok := f.rows[id]; ok && a.TenantID == t {
		return a, nil
	}
	return nil, mongo.ErrNoDocuments
}

func (f *fakeGuideStore) List(_ context.Context, t primitive.ObjectID, fl repository.GuideListFilter, page, limit int) ([]*models.GuideApplication, int64, error) {
	var out []*models.GuideApplication
	for _, a := range f.rows {
		if a.TenantID != t {
			continue
		}
		if fl.Status != "" && a.Status != fl.Status {
			continue
		}
		if fl.Region != "" && !guideIn(a.Regions, fl.Region) {
			continue
		}
		if fl.Q != "" {
			q := strings.ToLower(fl.Q)
			p := a.Personal
			if !strings.Contains(strings.ToLower(p.FullName), q) &&
				!strings.Contains(strings.ToLower(p.Phone), q) &&
				!strings.Contains(strings.ToLower(p.Email), q) {
				continue
			}
		}
		out = append(out, a)
	}
	total := int64(len(out))
	if limit > 0 && page > 0 {
		lo := (page - 1) * limit
		if lo > len(out) {
			lo = len(out)
		}
		hi := lo + limit
		if hi > len(out) {
			hi = len(out)
		}
		out = out[lo:hi]
	}
	return out, total, nil
}

func (f *fakeGuideStore) CountByStatus(_ context.Context, t primitive.ObjectID) (map[models.GuideStatus]int64, error) {
	out := map[models.GuideStatus]int64{}
	for _, a := range f.rows {
		if a.TenantID == t {
			out[a.Status]++
		}
	}
	return out, nil
}

func (f *fakeGuideStore) HasRecentByEmail(_ context.Context, t primitive.ObjectID, season, email string, since time.Time) (bool, error) {
	for _, a := range f.rows {
		if a.TenantID == t && a.Season == season && a.Personal.Email == email && !a.CreatedAt.Before(since) {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeGuideStore) SetStatus(_ context.Context, t, id primitive.ObjectID, st models.GuideStatus, ev models.GuideEvent) error {
	a, err := f.FindByID(context.Background(), t, id)
	if err != nil {
		return err
	}
	a.Status = st
	a.Events = append(a.Events, ev)
	a.UpdatedAt = f.now()
	return nil
}

func (f *fakeGuideStore) AddNote(_ context.Context, t, id primitive.ObjectID, ev models.GuideEvent) error {
	a, err := f.FindByID(context.Background(), t, id)
	if err != nil {
		return err
	}
	a.Events = append(a.Events, ev)
	a.UpdatedAt = f.now()
	return nil
}

type fakeGuideFiles struct {
	mu          sync.Mutex
	delay       time.Duration
	mimes       map[int]string // upload index -> forced mime
	unavailable bool
	failOnNth   int // 1-based upload index that fails; 0 = never
	mime        string
	size        int64
	uploads     int
	deleted     []string
	deleteErr   error // returned by Delete (the call is still recorded)
	uploadErr   error // returned by every Upload
	dlPublicID  string
	dlMime      string
	dlTTL       time.Duration
}

func (f *fakeGuideFiles) Available() bool { return !f.unavailable }

func (f *fakeGuideFiles) Upload(_ context.Context, r io.Reader, _ primitive.ObjectID) (*StoredFile, error) {
	_, _ = io.Copy(io.Discard, r)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads++
	if f.uploadErr != nil {
		return nil, f.uploadErr
	}
	if f.failOnNth != 0 && f.uploads == f.failOnNth {
		return nil, apierr.ValidationFailed("unsupported file type")
	}
	mime, size := f.mime, f.size
	if m, ok := f.mimes[f.uploads]; ok {
		mime = m
	}
	if mime == "" {
		mime = "application/pdf"
	}
	if size == 0 {
		size = 100
	}
	return &StoredFile{PublicID: "pid-" + string(rune('a'+f.uploads)), Mime: mime, Size: size}, nil
}

func (f *fakeGuideFiles) Delete(_ context.Context, publicID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, publicID)
	return f.deleteErr
}

func (f *fakeGuideFiles) DownloadURL(publicID, mime string, ttl time.Duration) (string, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dlPublicID, f.dlMime, f.dlTTL = publicID, mime, ttl
	return "https://files.test/signed", time.Date(2027, 1, 1, 0, 5, 0, 0, time.UTC), nil
}

type fakeGuideUsers struct {
	names map[primitive.ObjectID]string
	err   error
}

func (u *fakeGuideUsers) FindByIDs(_ context.Context, _ primitive.ObjectID, ids []primitive.ObjectID) ([]*models.TenantUser, error) {
	if u.err != nil {
		return nil, u.err
	}
	var out []*models.TenantUser
	for _, id := range ids {
		if n, ok := u.names[id]; ok {
			out = append(out, &models.TenantUser{ID: id, Name: n})
		}
	}
	return out, nil
}

type guideSvcEnv struct {
	svc   *GuideApplicationService
	store *fakeGuideStore
	files *fakeGuideFiles
	users *fakeGuideUsers
	now   time.Time
	t     primitive.ObjectID
}

func newGuideSvcEnv() *guideSvcEnv {
	e := &guideSvcEnv{now: guideNow, t: primitive.NewObjectID()}
	clock := func() time.Time { return e.now }
	e.store = newFakeGuideStore(clock)
	e.files = &fakeGuideFiles{}
	e.users = &fakeGuideUsers{names: map[primitive.ObjectID]string{}}
	e.svc = NewGuideApplicationService(e.store, e.files, e.users, clock)
	return e
}

func guideUp(kind models.GuideFileKind) GuideUpload {
	return GuideUpload{
		Kind:         kind,
		OriginalName: "doc.pdf",
		Open:         func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("%PDF-1.4 x")), nil },
	}
}

func wantAPIStatus(t *testing.T, err error, status int) {
	t.Helper()
	var ae *apierr.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if ae.HTTPStatus != status {
		t.Fatalf("status = %d, want %d (%v)", ae.HTTPStatus, status, err)
	}
}

// ── tests ────────────────────────────────────────────────────────────────

func TestSubmitSavesRowAndFiles(t *testing.T) {
	e := newGuideSvcEnv()
	a := validGuideApp()
	a.Personal.Email = "  Guide@Example.com "
	res, err := e.svc.Submit(context.Background(), e.t, a, []GuideUpload{guideUp(models.GuideFileCV), guideUp(models.GuideFilePhoto)})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(e.store.rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(e.store.rows))
	}
	id, _ := primitive.ObjectIDFromHex(res.ID)
	row := e.store.rows[id]
	if row == nil {
		t.Fatal("stored row not found by result id")
	}
	if row.Status != models.GuideNew || len(row.Events) != 0 {
		t.Fatalf("status=%q events=%d", row.Status, len(row.Events))
	}
	if row.Season != "summer-2027" || row.TenantID != e.t {
		t.Fatalf("season=%q tenant ok=%v", row.Season, row.TenantID == e.t)
	}
	if row.Personal.Email != "guide@example.com" {
		t.Fatalf("email not normalised: %q", row.Personal.Email)
	}
	if len(row.Files) != 2 || row.Files[0].PublicID != "pid-b" || row.Files[1].PublicID != "pid-c" {
		t.Fatalf("files = %+v", row.Files)
	}
	if row.Files[0].Kind != models.GuideFileCV || row.Files[0].Mime != "application/pdf" || row.Files[0].Size != 100 || row.Files[0].OriginalName != "doc.pdf" {
		t.Fatalf("file meta = %+v", row.Files[0])
	}
	if row.Files[0].ID == "" || row.Files[0].ID == row.Files[1].ID {
		t.Fatalf("file ids not unique: %q %q", row.Files[0].ID, row.Files[1].ID)
	}
	want := "GA-" + strings.ToUpper(res.ID[len(res.ID)-6:])
	if res.ConfirmationID != want {
		t.Fatalf("confirmation = %q, want %q", res.ConfirmationID, want)
	}
}

func TestSubmitDuplicateWithin24hConflicts(t *testing.T) {
	e := newGuideSvcEnv()
	if _, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), []GuideUpload{guideUp(models.GuideFileCV)}); err != nil {
		t.Fatalf("first: %v", err)
	}
	e.now = e.now.Add(time.Hour)
	b := validGuideApp()
	b.Personal.Email = "  GUIDE@example.COM  "
	_, err := e.svc.Submit(context.Background(), e.t, b, []GuideUpload{guideUp(models.GuideFileCV)})
	wantAPIStatus(t, err, 409)
	if len(e.store.rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(e.store.rows))
	}
	if e.files.uploads != 1 {
		t.Fatalf("uploads = %d, want 1 (no second upload)", e.files.uploads)
	}
}

func TestSubmitAllowsSameEmailAfter24h(t *testing.T) {
	e := newGuideSvcEnv()
	if _, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), []GuideUpload{guideUp(models.GuideFileCV)}); err != nil {
		t.Fatalf("first: %v", err)
	}
	e.now = e.now.Add(25 * time.Hour)
	if _, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), []GuideUpload{guideUp(models.GuideFileCV)}); err != nil {
		t.Fatalf("second after 25h: %v", err)
	}
	if len(e.store.rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(e.store.rows))
	}
}

func TestSubmitUploadFailureDeletesEarlierFiles(t *testing.T) {
	e := newGuideSvcEnv()
	e.files.failOnNth = 3
	ups := []GuideUpload{guideUp(models.GuideFileCV), guideUp(models.GuideFilePhoto), guideUp(models.GuideFileIDCard)}
	_, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), ups)
	wantAPIStatus(t, err, 422)
	if len(e.files.deleted) != 2 {
		t.Fatalf("deleted = %v, want 2", e.files.deleted)
	}
	if len(e.store.rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(e.store.rows))
	}
}

func TestSubmitInsertFailureDeletesFiles(t *testing.T) {
	e := newGuideSvcEnv()
	e.store.createErr = errors.New("boom")
	ups := []GuideUpload{guideUp(models.GuideFileCV), guideUp(models.GuideFilePhoto)}
	if _, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), ups); err == nil {
		t.Fatal("expected error")
	}
	if len(e.files.deleted) != 2 {
		t.Fatalf("deleted = %v, want 2", e.files.deleted)
	}
	if len(e.store.rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(e.store.rows))
	}
}

func TestSubmitFeatureUnavailableWhenFilesNil(t *testing.T) {
	e := newGuideSvcEnv()
	e.files.unavailable = true
	_, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), []GuideUpload{guideUp(models.GuideFileCV)})
	var ae *apierr.APIError
	if !errors.As(err, &ae) || ae.Code != apierr.CodeFeatureUnavailable {
		t.Fatalf("want FeatureUnavailable, got %v", err)
	}
	if len(e.store.rows) != 0 || e.files.uploads != 0 {
		t.Fatalf("rows=%d uploads=%d, want 0/0", len(e.store.rows), e.files.uploads)
	}
}

func TestSubmitValidatesBeforeUploading(t *testing.T) {
	e := newGuideSvcEnv()
	a := validGuideApp()
	a.Personal.Email = "not-an-email"
	_, err := e.svc.Submit(context.Background(), e.t, a, []GuideUpload{guideUp(models.GuideFileCV)})
	expectGuideErr(t, err, "email")
	if e.files.uploads != 0 {
		t.Fatalf("uploads = %d, want 0", e.files.uploads)
	}
	// File rules (cv required) also fail before any upload.
	_, err = e.svc.Submit(context.Background(), e.t, validGuideApp(), []GuideUpload{guideUp(models.GuideFilePhoto)})
	expectGuideErr(t, err, "files.cv")
	if e.files.uploads != 0 {
		t.Fatalf("uploads = %d, want 0", e.files.uploads)
	}
}

func TestSubmitPostUploadValidationFailureDeletesFiles(t *testing.T) {
	e := newGuideSvcEnv()
	e.files.mimes = map[int]string{2: "text/plain"}
	ups := []GuideUpload{guideUp(models.GuideFileCV), guideUp(models.GuideFilePhoto)}
	_, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), ups)
	wantAPIStatus(t, err, 422)
	if len(e.files.deleted) != 2 {
		t.Fatalf("deleted = %v, want 2", e.files.deleted)
	}
	if len(e.store.rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(e.store.rows))
	}
}

func TestSubmitConcurrentSameEmailOnlyOneSucceeds(t *testing.T) {
	for _, n := range []int{2, 5} {
		e := newGuideSvcEnv()
		e.files.delay = 20 * time.Millisecond
		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				ups := []GuideUpload{guideUp(models.GuideFileCV), guideUp(models.GuideFilePhoto)}
				_, errs[i] = e.svc.Submit(context.Background(), e.t, validGuideApp(), ups)
			}(i)
		}
		wg.Wait()
		ok := 0
		for _, err := range errs {
			if err == nil {
				ok++
			} else {
				wantAPIStatus(t, err, 409)
			}
		}
		if ok != 1 {
			t.Fatalf("n=%d successes = %d, want 1", n, ok)
		}
		if len(e.store.rows) != 1 {
			t.Fatalf("n=%d rows = %d, want 1", n, len(e.store.rows))
		}
		if e.files.uploads != 2 {
			t.Fatalf("n=%d uploads = %d, want 2", n, e.files.uploads)
		}
	}
}

// ── admin operations ─────────────────────────────────────────────────────

func (e *guideSvcEnv) seed(t primitive.ObjectID) *models.GuideApplication {
	a := validGuideApp()
	_ = e.store.Create(context.Background(), t, a)
	return a
}

func (e *guideSvcEnv) staff(name string) *primitive.ObjectID {
	id := primitive.NewObjectID()
	e.users.names[id] = name
	return &id
}

func TestSetStatusAppendsEventWithNameAndFromTo(t *testing.T) {
	e := newGuideSvcEnv()
	a := e.seed(e.t)
	actor := e.staff("Ann Staff")
	e.now = e.now.Add(time.Hour)
	if err := e.svc.SetStatus(context.Background(), e.t, a.ID.Hex(), models.GuideShortlisted, actor); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if a.Status != models.GuideShortlisted || len(a.Events) != 1 {
		t.Fatalf("status=%q events=%d", a.Status, len(a.Events))
	}
	ev := a.Events[0]
	if ev.Type != "status" || ev.From != string(models.GuideNew) || ev.To != string(models.GuideShortlisted) ||
		ev.UserName != "Ann Staff" || ev.UserID == nil || *ev.UserID != *actor || !ev.At.Equal(e.now) {
		t.Fatalf("event = %+v", ev)
	}
}

func TestSetStatusFallsBackToStaffName(t *testing.T) {
	e := newGuideSvcEnv()
	a := e.seed(e.t)
	id := primitive.NewObjectID() // unknown user
	if err := e.svc.SetStatus(context.Background(), e.t, a.ID.Hex(), models.GuideHired, &id); err != nil {
		t.Fatal(err)
	}
	if a.Events[0].UserName != "staff" {
		t.Fatalf("name = %q", a.Events[0].UserName)
	}
	e.users.err = errors.New("down")
	if err := e.svc.AddNote(context.Background(), e.t, a.ID.Hex(), "x", &id); err != nil {
		t.Fatal(err)
	}
	if a.Events[1].UserName != "staff" {
		t.Fatalf("name on lookup error = %q", a.Events[1].UserName)
	}
}

func TestSetStatusSameValueWritesNoEvent(t *testing.T) {
	e := newGuideSvcEnv()
	a := e.seed(e.t)
	before := a.UpdatedAt
	e.now = e.now.Add(time.Hour)
	if err := e.svc.SetStatus(context.Background(), e.t, a.ID.Hex(), models.GuideNew, e.staff("Ann")); err != nil {
		t.Fatalf("same status: %v", err)
	}
	if len(a.Events) != 0 || !a.UpdatedAt.Equal(before) {
		t.Fatalf("events=%d updated changed=%v", len(a.Events), !a.UpdatedAt.Equal(before))
	}
}

func TestSetStatusRejectsUnknownValue(t *testing.T) {
	e := newGuideSvcEnv()
	a := e.seed(e.t)
	err := e.svc.SetStatus(context.Background(), e.t, a.ID.Hex(), models.GuideStatus("archived"), e.staff("Ann"))
	expectGuideErr(t, err, "status")
	if len(a.Events) != 0 || a.Status != models.GuideNew {
		t.Fatal("row changed")
	}
}

func TestSetStatusNeedsActor(t *testing.T) {
	e := newGuideSvcEnv()
	a := e.seed(e.t)
	wantAPIStatus(t, e.svc.SetStatus(context.Background(), e.t, a.ID.Hex(), models.GuideHired, nil), 401)
	wantAPIStatus(t, e.svc.AddNote(context.Background(), e.t, a.ID.Hex(), "hi", nil), 401)
	if len(a.Events) != 0 || a.Status != models.GuideNew {
		t.Fatal("row changed")
	}
}

func TestAddNoteBounds(t *testing.T) {
	e := newGuideSvcEnv()
	a := e.seed(e.t)
	actor := e.staff("Ann")
	ctx := context.Background()
	for _, bad := range []string{"", "   \n\t "} {
		expectGuideErr(t, e.svc.AddNote(ctx, e.t, a.ID.Hex(), bad, actor), "text")
	}
	expectGuideErr(t, e.svc.AddNote(ctx, e.t, a.ID.Hex(), strings.Repeat("x", 2001), actor), "text")
	expectGuideErr(t, e.svc.AddNote(ctx, e.t, a.ID.Hex(), strings.Repeat("é", 2001), actor), "text")
	if len(a.Events) != 0 {
		t.Fatalf("events = %d after rejects", len(a.Events))
	}
	if err := e.svc.AddNote(ctx, e.t, a.ID.Hex(), strings.Repeat("é", 2000), actor); err != nil {
		t.Fatalf("2000 runes: %v", err)
	}
	if err := e.svc.AddNote(ctx, e.t, a.ID.Hex(), "  trimmed  ", actor); err != nil {
		t.Fatal(err)
	}
	if a.Events[1].Text != "trimmed" || a.Events[1].Type != "note" {
		t.Fatalf("event = %+v", a.Events[1])
	}
}

func TestNotesAreAppendOnly(t *testing.T) {
	e := newGuideSvcEnv()
	a := e.seed(e.t)
	actor := e.staff("Ann")
	ctx := context.Background()
	_ = e.svc.AddNote(ctx, e.t, a.ID.Hex(), "first", actor)
	_ = e.svc.AddNote(ctx, e.t, a.ID.Hex(), "second", actor)
	if len(a.Events) != 2 || a.Events[0].Text != "first" || a.Events[1].Text != "second" {
		t.Fatalf("events = %+v", a.Events)
	}
	if a.Status != models.GuideNew {
		t.Fatalf("note changed status to %q", a.Status)
	}
}

func TestCrossTenantGetIs404(t *testing.T) {
	e := newGuideSvcEnv()
	a := e.seed(e.t)
	other := primitive.NewObjectID()
	ctx := context.Background()
	_, err := e.svc.Get(ctx, other, a.ID.Hex())
	wantAPIStatus(t, err, 404)
	_, err = e.svc.Get(ctx, e.t, "not-hex")
	wantAPIStatus(t, err, 404)
	wantAPIStatus(t, e.svc.SetStatus(ctx, other, a.ID.Hex(), models.GuideHired, e.staff("Ann")), 404)
	wantAPIStatus(t, e.svc.AddNote(ctx, other, a.ID.Hex(), "x", e.staff("Ann")), 404)
	if got, err := e.svc.Get(ctx, e.t, a.ID.Hex()); err != nil || got.ID != a.ID {
		t.Fatalf("own tenant get: %v", err)
	}
}

func TestCrossTenantFileDownloadIs404(t *testing.T) {
	e := newGuideSvcEnv()
	a := e.seed(e.t)
	a.Files = []models.GuideFile{{ID: "f1", PublicID: "pid-x", Mime: "application/pdf"}}
	_, _, err := e.svc.FileDownload(context.Background(), primitive.NewObjectID(), a.ID.Hex(), "f1")
	wantAPIStatus(t, err, 404)
	if e.files.dlPublicID != "" {
		t.Fatal("signed a URL for a foreign tenant")
	}
}

func TestFileDownloadUnknownFileIs404(t *testing.T) {
	e := newGuideSvcEnv()
	a := e.seed(e.t)
	a.Files = []models.GuideFile{{ID: "f1", PublicID: "pid-x", Mime: "application/pdf"}}
	_, _, err := e.svc.FileDownload(context.Background(), e.t, a.ID.Hex(), "nope")
	wantAPIStatus(t, err, 404)
	_, _, err = e.svc.FileDownload(context.Background(), e.t, "bad", "f1")
	wantAPIStatus(t, err, 404)
}

func TestFileDownloadUsesFiveMinuteTTL(t *testing.T) {
	e := newGuideSvcEnv()
	a := e.seed(e.t)
	a.Files = []models.GuideFile{{ID: "f1", PublicID: "pid-x", Mime: "application/pdf"}}
	url, exp, err := e.svc.FileDownload(context.Background(), e.t, a.ID.Hex(), "f1")
	if err != nil || url == "" || exp.IsZero() {
		t.Fatalf("url=%q exp=%v err=%v", url, exp, err)
	}
	if e.files.dlTTL != 5*time.Minute || e.files.dlPublicID != "pid-x" || e.files.dlMime != "application/pdf" {
		t.Fatalf("ttl=%v pid=%q mime=%q", e.files.dlTTL, e.files.dlPublicID, e.files.dlMime)
	}
	e.files.unavailable = true
	_, _, err = e.svc.FileDownload(context.Background(), e.t, a.ID.Hex(), "f1")
	var ae *apierr.APIError
	if !errors.As(err, &ae) || ae.Code != apierr.CodeFeatureUnavailable {
		t.Fatalf("want FeatureUnavailable, got %v", err)
	}
}

func TestCountsFillsZeroes(t *testing.T) {
	e := newGuideSvcEnv()
	e.seed(e.t)
	e.seed(e.t)
	e.seed(primitive.NewObjectID())
	got, err := e.svc.Counts(context.Background(), e.t)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got[models.GuideNew] != 2 {
		t.Fatalf("counts = %v", got)
	}
	for _, st := range models.GuideStatuses {
		if _, ok := got[st]; !ok {
			t.Fatalf("missing key %q", st)
		}
	}
}

func TestEventKeepsUserNameAfterRename(t *testing.T) {
	e := newGuideSvcEnv()
	a := e.seed(e.t)
	actor := e.staff("Old Name")
	ctx := context.Background()
	_ = e.svc.SetStatus(ctx, e.t, a.ID.Hex(), models.GuideReviewing, actor)
	e.users.names[*actor] = "New Name"
	_ = e.svc.AddNote(ctx, e.t, a.ID.Hex(), "later", actor)
	if a.Events[0].UserName != "Old Name" || a.Events[1].UserName != "New Name" {
		t.Fatalf("names = %q, %q", a.Events[0].UserName, a.Events[1].UserName)
	}
}

type recordingGuideStore struct {
	*fakeGuideStore
	page, limit int
	filter      repository.GuideListFilter
}

func (r *recordingGuideStore) List(ctx context.Context, t primitive.ObjectID, f repository.GuideListFilter, page, limit int) ([]*models.GuideApplication, int64, error) {
	r.page, r.limit, r.filter = page, limit, f
	return r.fakeGuideStore.List(ctx, t, f, page, limit)
}

func TestListClampsPageAndLimit(t *testing.T) {
	cases := []struct{ page, limit, wantPage, wantLimit int }{
		{0, 0, 1, 20}, {-3, -1, 1, 20}, {2, 101, 2, 20}, {3, 100, 3, 100}, {1, 1, 1, 1},
	}
	for _, c := range cases {
		e := newGuideSvcEnv()
		rec := &recordingGuideStore{fakeGuideStore: e.store}
		svc := NewGuideApplicationService(rec, e.files, e.users, nil)
		if _, _, err := svc.List(context.Background(), e.t, repository.GuideListFilter{}, c.page, c.limit); err != nil {
			t.Fatal(err)
		}
		if rec.page != c.wantPage || rec.limit != c.wantLimit {
			t.Fatalf("(%d,%d) -> (%d,%d), want (%d,%d)", c.page, c.limit, rec.page, rec.limit, c.wantPage, c.wantLimit)
		}
	}
}

func TestClampPage(t *testing.T) {
	cases := []struct{ page, limit, wantPage, wantLimit int }{
		{0, 0, 1, 20}, {-1, 500, 1, 20}, {2, 100, 2, 100}, {5, 101, 5, 20},
	}
	for _, c := range cases {
		p, l := ClampPage(c.page, c.limit)
		if p != c.wantPage || l != c.wantLimit {
			t.Errorf("ClampPage(%d,%d) = (%d,%d), want (%d,%d)", c.page, c.limit, p, l, c.wantPage, c.wantLimit)
		}
	}
}

func TestListCapsSearchTextAt100Runes(t *testing.T) {
	e := newGuideSvcEnv()
	rec := &recordingGuideStore{fakeGuideStore: e.store}
	svc := NewGuideApplicationService(rec, e.files, e.users, nil)
	long := strings.Repeat("\u00e9", 250)
	if _, _, err := svc.List(context.Background(), e.t, repository.GuideListFilter{Q: long}, 1, 20); err != nil {
		t.Fatal(err)
	}
	if rec.filter.Q != strings.Repeat("\u00e9", 100) {
		t.Fatalf("q = %d runes, want 100", len([]rune(rec.filter.Q)))
	}
	if _, _, err := svc.List(context.Background(), e.t, repository.GuideListFilter{Q: "short"}, 1, 20); err != nil {
		t.Fatal(err)
	}
	if rec.filter.Q != "short" {
		t.Fatalf("q = %q", rec.filter.Q)
	}
}

// ── file validation before upload, error naming, cleanup ─────────────────

func guideUpBytes(kind models.GuideFileKind, content string) GuideUpload {
	return GuideUpload{
		Kind:         kind,
		OriginalName: "f.bin",
		Open:         func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(content)), nil },
	}
}

func TestSubmitBadThirdFileUploadsAndDeletesNothing(t *testing.T) {
	e := newGuideSvcEnv()
	ups := []GuideUpload{
		guideUp(models.GuideFileCV),
		guideUp(models.GuideFilePhoto),
		guideUpBytes(models.GuideFileIDCard, "just some plain text, not a document"),
	}
	_, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), ups)
	wantAPIStatus(t, err, 422)
	var ae *apierr.APIError
	if !errors.As(err, &ae) || ae.Message != "files.id_card: "+msgUnsupportedType {
		t.Fatalf("message = %v", err)
	}
	if e.files.uploads != 0 || len(e.files.deleted) != 0 || len(e.store.rows) != 0 {
		t.Fatalf("uploads=%d deletes=%d rows=%d, want 0/0/0", e.files.uploads, len(e.files.deleted), len(e.store.rows))
	}
}

func TestSubmitEmptyFileIsNamed(t *testing.T) {
	e := newGuideSvcEnv()
	_, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), []GuideUpload{guideUpBytes(models.GuideFileCV, "")})
	var ae *apierr.APIError
	if !errors.As(err, &ae) || ae.HTTPStatus != 422 || ae.Message != "files.cv: "+msgFileEmpty {
		t.Fatalf("got %v", err)
	}
	if e.files.uploads != 0 {
		t.Fatal("uploaded an empty file")
	}
}

func TestUploadClientErrorsAreNamedWithSameSafeText(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{apierr.ValidationFailed("file exceeds the 10 byte limit"), "files.photo: file exceeds the 10 byte limit"},
		{apierr.ValidationFailed("document could not be read; export it again as a valid PDF or image"), "files.photo: document could not be read; export it again as a valid PDF or image"},
		{apierr.BadRequest("could not read file"), "files.photo: could not read file"},
	}
	for _, c := range cases {
		e := newGuideSvcEnv()
		e.files.uploadErr = c.err
		ups := []GuideUpload{guideUp(models.GuideFileCV), guideUp(models.GuideFilePhoto)}
		_, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), ups)
		_ = err
		// uploadErr fails the first upload (the cv), so the name is files.cv.
		var ae *apierr.APIError
		if !errors.As(err, &ae) || ae.HTTPStatus != 422 {
			t.Fatalf("got %v", err)
		}
		want := strings.Replace(c.want, "files.photo", "files.cv", 1)
		if ae.Message != want {
			t.Fatalf("message = %q, want %q", ae.Message, want)
		}
		low := strings.ToLower(ae.Message)
		for _, bad := range []string{"cloudinary", "http", "api_key", "secret"} {
			if strings.Contains(low, bad) {
				t.Fatalf("message carries provider text %q: %q", bad, ae.Message)
			}
		}
	}
}

func TestUploadUpstreamErrorIsNotRenamed(t *testing.T) {
	e := newGuideSvcEnv()
	e.files.uploadErr = apierr.Upstream(apierr.DomainUpload, errors.New("provider says no"))
	_, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), []GuideUpload{guideUp(models.GuideFileCV)})
	var ae *apierr.APIError
	if !errors.As(err, &ae) || ae.HTTPStatus != 502 || ae.Message != "upstream service unavailable" {
		t.Fatalf("got %v", err)
	}
	// A plain (non-APIError) error also becomes a 502.
	e = newGuideSvcEnv()
	e.files.uploadErr = errors.New("connection reset")
	_, err = e.svc.Submit(context.Background(), e.t, validGuideApp(), []GuideUpload{guideUp(models.GuideFileCV)})
	wantAPIStatus(t, err, 502)
}

func TestSubmitStampsConsentAtFromServerClock(t *testing.T) {
	e := newGuideSvcEnv()
	a := validGuideApp()
	a.ConsentAt = e.now.Add(-72 * time.Hour) // client-supplied, must be replaced
	res, err := e.svc.Submit(context.Background(), e.t, a, []GuideUpload{guideUp(models.GuideFileCV)})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := primitive.ObjectIDFromHex(res.ID)
	if got := e.store.rows[id].ConsentAt; !got.Equal(e.now) {
		t.Fatalf("consent_at = %v, want %v", got, e.now)
	}
	// Missing consent is still refused.
	b := validGuideApp()
	b.Personal.Email = "other@example.com"
	b.ConsentAt = time.Time{}
	_, err = e.svc.Submit(context.Background(), e.t, b, []GuideUpload{guideUp(models.GuideFileCV)})
	expectGuideErr(t, err, "consent_at")
}

func TestFailedCleanupDeletesAreAttachedAsInternalCause(t *testing.T) {
	run := func(deleteErr error) (*apierr.APIError, *fakeGuideFiles) {
		e := newGuideSvcEnv()
		e.files.failOnNth = 3
		e.files.deleteErr = deleteErr
		ups := []GuideUpload{guideUp(models.GuideFileCV), guideUp(models.GuideFilePhoto), guideUp(models.GuideFileIDCard)}
		_, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), ups)
		var ae *apierr.APIError
		if !errors.As(err, &ae) {
			t.Fatalf("got %v", err)
		}
		return ae, e.files
	}
	clean, _ := run(nil)
	failed, files := run(errors.New("destroy rejected"))
	if failed.HTTPStatus != clean.HTTPStatus || failed.Message != clean.Message || failed.Code != clean.Code {
		t.Fatalf("client-facing error changed: %d %q vs %d %q", failed.HTTPStatus, failed.Message, clean.HTTPStatus, clean.Message)
	}
	if len(files.deleted) != 2 {
		t.Fatalf("deleted = %v, want both attempted", files.deleted)
	}
	if failed.Err == nil || !strings.Contains(failed.Err.Error(), "pid-b") || !strings.Contains(failed.Err.Error(), "pid-c") {
		t.Fatalf("cause does not carry the public ids: %v", failed.Err)
	}
	if strings.Contains(failed.Message, "pid-") {
		t.Fatal("public id leaked into the client message")
	}
	if clean.Err != nil {
		t.Fatalf("clean run has a cause: %v", clean.Err)
	}
}

func TestFailedCleanupAfterInsertErrorStillMapsTo500(t *testing.T) {
	e := newGuideSvcEnv()
	e.store.createErr = errors.New("boom")
	e.files.deleteErr = errors.New("destroy rejected")
	_, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), []GuideUpload{guideUp(models.GuideFileCV)})
	var ae *apierr.APIError
	if !errors.As(err, &ae) || ae.HTTPStatus != 500 || ae.Message != "internal server error" {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(ae.Err.Error(), "boom") || !strings.Contains(ae.Err.Error(), "pid-b") {
		t.Fatalf("cause = %v", ae.Err)
	}
}

// ── notifier ─────────────────────────────────────────────────────────────

type notifyCall struct {
	tenant   primitive.ObjectID
	kind     NotifyKind
	recordID string
	summary  string
}

// fakeRequestNotifier records Notify calls. It is shared by the tests of every
// service that takes a requestNotifier.
type fakeRequestNotifier struct {
	mu    sync.Mutex
	calls []notifyCall
}

func (f *fakeRequestNotifier) Notify(_ context.Context, tenantID primitive.ObjectID, kind NotifyKind, recordID, summary string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, notifyCall{tenantID, kind, recordID, summary})
}

func TestSubmitNotifiesAfterSuccessfulWrite(t *testing.T) {
	e := newGuideSvcEnv()
	n := &fakeRequestNotifier{}
	e.svc.WithNotifier(n)
	a := validGuideApp()
	a.Personal.Phone = "+976 99112233"
	a.Personal.Email = "private@example.com"
	res, err := e.svc.Submit(context.Background(), e.t, a, []GuideUpload{guideUp(models.GuideFileCV)})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(n.calls) != 1 {
		t.Fatalf("notify calls = %d, want 1", len(n.calls))
	}
	c := n.calls[0]
	if c.tenant != e.t || c.kind != NotifyGuide || c.recordID != res.ID {
		t.Fatalf("call = %+v, result id %s", c, res.ID)
	}
	if !strings.Contains(c.summary, a.Personal.FullName) {
		t.Fatalf("summary %q lacks the applicant name", c.summary)
	}
	for _, secret := range []string{"99112233", "private@example.com", "@"} {
		if strings.Contains(c.summary, secret) {
			t.Fatalf("summary %q leaks contact detail %q", c.summary, secret)
		}
	}
}

func TestSubmitDoesNotNotifyOnValidationError(t *testing.T) {
	e := newGuideSvcEnv()
	n := &fakeRequestNotifier{}
	e.svc.WithNotifier(n)
	a := validGuideApp()
	a.Personal.Email = "not-an-email"
	if _, err := e.svc.Submit(context.Background(), e.t, a, []GuideUpload{guideUp(models.GuideFileCV)}); err == nil {
		t.Fatal("expected validation error")
	}
	if len(n.calls) != 0 {
		t.Fatalf("notify calls = %d, want 0", len(n.calls))
	}
}

func TestSubmitDoesNotNotifyOnWriteError(t *testing.T) {
	e := newGuideSvcEnv()
	n := &fakeRequestNotifier{}
	e.svc.WithNotifier(n)
	e.store.createErr = errors.New("boom")
	if _, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), []GuideUpload{guideUp(models.GuideFileCV)}); err == nil {
		t.Fatal("expected error")
	}
	if len(n.calls) != 0 {
		t.Fatalf("notify calls = %d, want 0", len(n.calls))
	}
}

func TestSubmitWithoutNotifierStillWorks(t *testing.T) {
	e := newGuideSvcEnv()
	if _, err := e.svc.Submit(context.Background(), e.t, validGuideApp(), []GuideUpload{guideUp(models.GuideFileCV)}); err != nil {
		t.Fatalf("submit: %v", err)
	}
}
