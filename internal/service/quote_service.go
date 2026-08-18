package service

import (
	"context"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type QuoteService struct {
	repo             *repository.QuoteRepo
	tenantUserRepo   *repository.TenantUserRepo
	platformUserRepo *repository.PlatformUserRepo
}

func NewQuoteService(repo *repository.QuoteRepo, tenantUserRepo *repository.TenantUserRepo, platformUserRepo *repository.PlatformUserRepo) *QuoteService {
	return &QuoteService{repo: repo, tenantUserRepo: tenantUserRepo, platformUserRepo: platformUserRepo}
}

// Create files a new lead. tenantID is nil for a general inquiry with no
// existing tenant relationship (the platform-wide intake route), or set
// when submitted through a specific tenant's own storefront.
func (s *QuoteService) Create(ctx context.Context, tenantID *primitive.ObjectID, q *models.Quote) error {
	if q.Name == "" || q.Email == "" {
		return apierr.BadRequest("name and email are required")
	}
	q.TenantID = tenantID
	return s.repo.Create(ctx, q)
}

// List returns a single tenant's own leads — the tenant admin panel's view.
func (s *QuoteService) List(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.Quote, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	quotes, total, err := s.repo.FindAll(ctx, bson.M{"tenant_id": tenantID}, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.resolveLastEditedByTenantUser(ctx, tenantID, quotes); err != nil {
		return nil, 0, apierr.Internal()
	}
	return quotes, total, nil
}

// ListAll returns every lead across the platform, tenant-linked and
// tenant-less alike — the platform admin's consolidated view.
func (s *QuoteService) ListAll(ctx context.Context, page, limit int) ([]*models.Quote, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	quotes, total, err := s.repo.FindAll(ctx, bson.M{}, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.resolveLastEditedByPlatformUser(ctx, quotes); err != nil {
		return nil, 0, apierr.Internal()
	}
	return quotes, total, nil
}

// resolveLastEditedByTenantUser populates each quote's LastEditedBy with the
// display name of the tenant user referenced by its UserID — valid for
// List, whose quotes all share the given tenantID.
func (s *QuoteService) resolveLastEditedByTenantUser(ctx context.Context, tenantID primitive.ObjectID, quotes []*models.Quote) error {
	ids := make([]primitive.ObjectID, 0, len(quotes))
	seen := make(map[primitive.ObjectID]bool, len(quotes))
	for _, q := range quotes {
		if q.UserID != nil && !seen[*q.UserID] {
			seen[*q.UserID] = true
			ids = append(ids, *q.UserID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	users, err := s.tenantUserRepo.FindByIDs(ctx, tenantID, ids)
	if err != nil {
		return err
	}
	names := make(map[primitive.ObjectID]string, len(users))
	for _, u := range users {
		names[u.ID] = u.Name
	}

	for _, q := range quotes {
		if q.UserID == nil {
			continue
		}
		if name, ok := names[*q.UserID]; ok {
			q.LastEditedBy = &name
		}
	}
	return nil
}

// resolveLastEditedByPlatformUser is resolveLastEditedByTenantUser's
// counterpart for ListAll, where quotes may belong to different tenants (or
// none) — UserID there is only ever meaningfully resolvable against
// platform_users, since PUT /platform/quotes/{id}/status is the only write
// path that can touch a tenant-less quote.
func (s *QuoteService) resolveLastEditedByPlatformUser(ctx context.Context, quotes []*models.Quote) error {
	names := make(map[primitive.ObjectID]string)
	for _, q := range quotes {
		if q.UserID == nil {
			continue
		}
		if name, ok := names[*q.UserID]; ok {
			q.LastEditedBy = &name
			continue
		}
		u, err := s.platformUserRepo.FindByID(ctx, *q.UserID)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				continue
			}
			return err
		}
		names[*q.UserID] = u.Name
		q.LastEditedBy = &u.Name
	}
	return nil
}

// UpdateStatus updates any quote regardless of tenant — the platform-wide
// route, the only one that can act on a tenant-less lead.
func (s *QuoteService) UpdateStatus(ctx context.Context, idStr string, status models.QuoteStatus, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.UpdateStatus(ctx, id, status, userID)
}

// UpdateStatusForTenant is UpdateStatus scoped to a single tenant's own
// leads — the tenant admin panel's route.
func (s *QuoteService) UpdateStatusForTenant(ctx context.Context, tenantID primitive.ObjectID, idStr string, status models.QuoteStatus, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.UpdateStatusForTenant(ctx, tenantID, id, status, userID)
}
