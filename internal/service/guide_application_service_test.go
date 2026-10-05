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

func (f *fakeGuideStore) Delete(_ context.Context, t, id primitive.ObjectID) error {
	if a, ok := f.rows[id]; ok && a.TenantID == t {
		delete(f.rows, id)
	}
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
	return nil
}

func (f *fakeGuideFiles) DownloadURL(string, string, time.Duration) (string, time.Time, error) {
	return "", time.Time{}, errors.New("not used")
}

type guideSvcEnv struct {
	svc   *GuideApplicationService
	store *fakeGuideStore
	files *fakeGuideFiles
	now   time.Time
	t     primitive.ObjectID
}

func newGuideSvcEnv() *guideSvcEnv {
	e := &guideSvcEnv{now: guideNow, t: primitive.NewObjectID()}
	clock := func() time.Time { return e.now }
	e.store = newFakeGuideStore(clock)
	e.files = &fakeGuideFiles{}
	e.svc = NewGuideApplicationService(e.store, e.files, nil, clock)
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
