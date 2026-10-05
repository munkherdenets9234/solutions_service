package service

import (
	"context"

	"github.com/eandstravel/digitalservice/internal/domainnorm"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/apikey"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type TenantService struct {
	repo             *repository.TenantRepo
	detailRepo       *repository.TenantDetailRepo
	platformUserRepo *repository.PlatformUserRepo
}

func NewTenantService(repo *repository.TenantRepo, detailRepo *repository.TenantDetailRepo, platformUserRepo *repository.PlatformUserRepo) *TenantService {
	return &TenantService{repo: repo, detailRepo: detailRepo, platformUserRepo: platformUserRepo}
}

// Create returns the created tenant and the raw API key — the raw key is
// only ever available here, at creation time.
func (s *TenantService) Create(ctx context.Context, t *models.Tenant) (*models.Tenant, string, error) {
	if t.Slug == "" {
		return nil, "", apierr.BadRequest("slug is required")
	}

	raw, hash, err := apikey.Generate()
	if err != nil {
		return nil, "", apierr.Internal(err)
	}
	t.APIKeyHash = hash
	t.APIKeyLast4 = apikey.Last4(raw)

	if err := s.repo.Create(ctx, t); err != nil {
		return nil, "", apierr.Internal(err)
	}
	return t, raw, nil
}

func (s *TenantService) List(ctx context.Context, page, limit int) ([]*models.Tenant, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	tenants, total, err := s.repo.FindAll(ctx, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.attachProjects(ctx, tenants); err != nil {
		return nil, 0, apierr.Internal(err)
	}
	return tenants, total, nil
}

func (s *TenantService) GetByID(ctx context.Context, idStr string) (*models.Tenant, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, apierr.BadRequest("invalid id")
	}
	t, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("tenant not found")
		}
		return nil, apierr.Internal(err)
	}
	if err := s.attachProjects(ctx, []*models.Tenant{t}); err != nil {
		return nil, apierr.Internal(err)
	}
	return t, nil
}

// attachProjects populates each tenant's Project with its linked
// TenantDetail (see the tenant_details table), when one exists — GET
// /platform/tenants and /platform/tenants/{id} embed it in full (raw locale
// maps, like every other admin-facing read here), so the platform's tenant
// management UI gets a client's "Our Projects" content without a second
// round-trip.
func (s *TenantService) attachProjects(ctx context.Context, tenants []*models.Tenant) error {
	ids := make([]primitive.ObjectID, len(tenants))
	for i, t := range tenants {
		ids[i] = t.ID
	}
	details, err := s.detailRepo.FindByTenantIDs(ctx, ids)
	if err != nil {
		return err
	}

	byTenant := make(map[primitive.ObjectID]*models.TenantDetail, len(details))
	for _, d := range details {
		if err := s.resolveDetailEditedBy(ctx, d); err != nil {
			return err
		}
		byTenant[d.TenantID] = d
	}
	for _, t := range tenants {
		if d, ok := byTenant[t.ID]; ok {
			t.Project = d
		}
	}
	return nil
}

// resolveDetailEditedBy populates d's LastEditedBy with the display name of
// the platform superadmin referenced by its UserID — TenantDetail is only
// ever managed via /platform routes, so this resolves against
// PlatformUserRepo, the same as SubscriptionService.
func (s *TenantService) resolveDetailEditedBy(ctx context.Context, d *models.TenantDetail) error {
	if d.UserID == nil {
		return nil
	}
	u, err := s.platformUserRepo.FindByID(ctx, *d.UserID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil
		}
		return err
	}
	d.LastEditedBy = &u.Name
	return nil
}

func (s *TenantService) UpdateStatus(ctx context.Context, idStr string, status models.TenantStatus) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.UpdateStatus(ctx, id, status)
}

// RotateAPIKey issues a new API key for the tenant and invalidates the old one.
func (s *TenantService) RotateAPIKey(ctx context.Context, idStr string) (string, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return "", apierr.BadRequest("invalid id")
	}
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		if err == mongo.ErrNoDocuments {
			return "", apierr.NotFound("tenant not found")
		}
		return "", apierr.Internal(err)
	}

	raw, hash, err := apikey.Generate()
	if err != nil {
		return "", apierr.Internal(err)
	}
	if err := s.repo.RotateAPIKey(ctx, id, hash, apikey.Last4(raw)); err != nil {
		return "", apierr.Internal(err)
	}
	return raw, nil
}

// UpdateDomain assigns the browser-facing domain a tenant's API key is bound
// to (see TenantMiddleware) — e.g. "acme.yourapp.com" or a client's own
// "www.acmetravel.com". Stored lowercase, without scheme or path, so it can
// be compared directly against a request's Origin/Referer host.
func (s *TenantService) UpdateDomain(ctx context.Context, idStr, domain string) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}

	domain = domainnorm.Normalize(domain)
	if domain == "" {
		return apierr.BadRequest("domain is required")
	}

	if err := s.repo.UpdateDomain(ctx, id, domain); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return apierr.Conflict("domain is already assigned to another tenant")
		}
		return apierr.Internal(err)
	}
	return nil
}

// ListProjects returns active, showcase-enabled tenants for the public "Our
// Projects" listing, joined against their TenantDetail record — the
// returned slices are parallel (details[i] belongs to tenants[i]).
// TenantDetail.Showcase is the query's driving filter, so total reflects
// showcase-enabled details; a handful may be dropped from the page itself if
// their tenant has since gone inactive.
func (s *TenantService) ListProjects(ctx context.Context, page, limit int) ([]*models.Tenant, []*models.TenantDetail, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	details, total, err := s.detailRepo.FindAllShowcase(ctx, page, limit)
	if err != nil {
		return nil, nil, 0, apierr.Internal(err)
	}

	ids := make([]primitive.ObjectID, len(details))
	for i, d := range details {
		ids[i] = d.TenantID
	}
	tenants, err := s.repo.FindByIDsActive(ctx, ids)
	if err != nil {
		return nil, nil, 0, apierr.Internal(err)
	}
	byID := make(map[primitive.ObjectID]*models.Tenant, len(tenants))
	for _, t := range tenants {
		byID[t.ID] = t
	}

	matchedTenants := make([]*models.Tenant, 0, len(details))
	matchedDetails := make([]*models.TenantDetail, 0, len(details))
	for _, d := range details {
		if t, ok := byID[d.TenantID]; ok {
			matchedTenants = append(matchedTenants, t)
			matchedDetails = append(matchedDetails, d)
		}
	}
	return matchedTenants, matchedDetails, total, nil
}

// GetProjectBySlug is ListProjects' single-project counterpart, for the
// case-study detail page.
func (s *TenantService) GetProjectBySlug(ctx context.Context, slug string) (*models.Tenant, *models.TenantDetail, error) {
	t, err := s.repo.FindBySlugActive(ctx, slug)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil, apierr.NotFound("project not found")
		}
		return nil, nil, apierr.Internal(err)
	}

	d, err := s.detailRepo.FindByTenantID(ctx, t.ID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil, apierr.NotFound("project not found")
		}
		return nil, nil, apierr.Internal(err)
	}
	if !d.Showcase {
		return nil, nil, apierr.NotFound("project not found")
	}
	return t, d, nil
}

// UpdateProject upserts the tenant's TenantDetail record (see
// dto.UpdateProjectRequest) — the caller controls exactly which keys
// `update` contains, so this never touches the tenant's identity/billing
// fields.
func (s *TenantService) UpdateProject(ctx context.Context, idStr string, update bson.M, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		if err == mongo.ErrNoDocuments {
			return apierr.NotFound("tenant not found")
		}
		return apierr.Internal(err)
	}
	return s.detailRepo.Upsert(ctx, id, update, userID)
}

// Resolve looks up an active tenant by its raw API key, for request-time
// tenant resolution (used by TenantMiddleware).
func (s *TenantService) Resolve(ctx context.Context, rawAPIKey string) (*models.Tenant, error) {
	t, err := s.repo.FindByAPIKeyHash(ctx, apikey.Hash(rawAPIKey))
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.Unauthorized("")
		}
		return nil, apierr.Internal(err)
	}
	if t.Status != models.TenantActive {
		return nil, apierr.Forbidden("tenant suspended").In(apierr.DomainTenant)
	}
	return t, nil
}
