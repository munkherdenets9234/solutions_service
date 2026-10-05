package service

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
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
	Delete(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) error
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
}

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

// GuideUpload is one file offered with an application. Open is called once.
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
	kinds := make([]GuideUploadMeta, len(uploads))
	for i, u := range uploads {
		kinds[i] = GuideUploadMeta{Kind: u.Kind}
	}
	if err := ValidateGuideFiles(kinds); err != nil {
		return nil, err
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
	rollback := func() {
		cctx := context.WithoutCancel(ctx)
		for _, f := range stored {
			_ = s.files.Delete(cctx, f.PublicID, f.Mime)
		}
	}

	for _, u := range uploads {
		sf, err := s.uploadOne(ctx, tenantID, u)
		if err != nil {
			// An object created remotely before a timeout error has no public id
			// to delete; accepted.
			rollback()
			return nil, err
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
		rollback()
		return nil, err
	}

	a.Personal.Email = email
	a.Season = guideSeason
	a.Files = stored
	if err := s.store.Create(ctx, tenantID, a); err != nil {
		rollback()
		return nil, apierr.Internal(err)
	}

	hexID := a.ID.Hex()
	return &SubmitResult{ID: hexID, ConfirmationID: "GA-" + upperTail(hexID, 6)}, nil
}

func (s *GuideApplicationService) uploadOne(ctx context.Context, tenantID primitive.ObjectID, u GuideUpload) (*StoredFile, error) {
	rc, err := u.Open()
	if err != nil {
		return nil, apierr.BadRequest("could not read file")
	}
	defer rc.Close()
	sf, err := s.files.Upload(ctx, rc, tenantID)
	if err != nil {
		if _, ok := err.(*apierr.APIError); ok {
			return nil, err
		}
		return nil, apierr.Upstream(apierr.DomainUpload, err)
	}
	return sf, nil
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
