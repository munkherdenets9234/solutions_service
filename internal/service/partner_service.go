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

type PartnerService struct {
	repo           *repository.PartnerRepo
	tenantUserRepo *repository.TenantUserRepo
}

func NewPartnerService(repo *repository.PartnerRepo, tenantUserRepo *repository.TenantUserRepo) *PartnerService {
	return &PartnerService{repo: repo, tenantUserRepo: tenantUserRepo}
}

type ListPartnersFilter struct {
	Tag   string
	Page  int
	Limit int
}

func (s *PartnerService) List(ctx context.Context, tenantID primitive.ObjectID, f ListPartnersFilter) ([]*models.Partner, int64, error) {
	filter := bson.M{"is_active": true}
	if f.Tag != "" {
		filter["tag"] = f.Tag
	}

	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}

	return s.repo.FindAll(ctx, tenantID, filter, f.Page, f.Limit)
}

// ListAdmin returns every partner for a tenant regardless of active status,
// for the admin CMS.
func (s *PartnerService) ListAdmin(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.Partner, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	partners, total, err := s.repo.FindAll(ctx, tenantID, bson.M{}, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, partners); err != nil {
		return nil, 0, apierr.Internal()
	}
	return partners, total, nil
}

func (s *PartnerService) GetByID(ctx context.Context, tenantID primitive.ObjectID, idStr string) (*models.Partner, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, apierr.BadRequest("invalid id")
	}
	p, err := s.repo.FindByID(ctx, tenantID, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("partner not found")
		}
		return nil, apierr.Internal()
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, []*models.Partner{p}); err != nil {
		return nil, apierr.Internal()
	}
	return p, nil
}

// resolveLastEditedBy populates each partner's LastEditedBy with the display
// name of the tenant user referenced by its UserID, for admin GET responses.
func (s *PartnerService) resolveLastEditedBy(ctx context.Context, tenantID primitive.ObjectID, partners []*models.Partner) error {
	ids := make([]primitive.ObjectID, 0, len(partners))
	seen := make(map[primitive.ObjectID]bool, len(partners))
	for _, p := range partners {
		if p.UserID != nil && !seen[*p.UserID] {
			seen[*p.UserID] = true
			ids = append(ids, *p.UserID)
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

	for _, p := range partners {
		if p.UserID == nil {
			continue
		}
		if name, ok := names[*p.UserID]; ok {
			p.LastEditedBy = &name
		}
	}
	return nil
}

func (s *PartnerService) GetBySlug(ctx context.Context, tenantID primitive.ObjectID, slug string) (*models.Partner, error) {
	p, err := s.repo.FindBySlug(ctx, tenantID, slug)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("partner not found")
		}
		return nil, apierr.Internal()
	}
	return p, nil
}

func (s *PartnerService) Create(ctx context.Context, tenantID primitive.ObjectID, p *models.Partner, userID *primitive.ObjectID) error {
	if p.Slug == "" {
		return apierr.BadRequest("slug is required")
	}
	if p.Name == "" {
		return apierr.BadRequest("name is required")
	}
	p.IsActive = true
	return s.repo.Create(ctx, tenantID, p, userID)
}

func (s *PartnerService) Update(ctx context.Context, tenantID primitive.ObjectID, idStr string, update bson.M, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	if _, err := s.repo.FindByID(ctx, tenantID, id); err != nil {
		return apierr.NotFound("partner not found")
	}
	return s.repo.Update(ctx, tenantID, id, update, userID)
}

func (s *PartnerService) Delete(ctx context.Context, tenantID primitive.ObjectID, idStr string, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.Delete(ctx, tenantID, id, userID)
}
