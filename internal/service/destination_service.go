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

type DestinationService struct {
	repo           *repository.DestinationRepo
	tenantUserRepo *repository.TenantUserRepo
}

func NewDestinationService(repo *repository.DestinationRepo, tenantUserRepo *repository.TenantUserRepo) *DestinationService {
	return &DestinationService{repo: repo, tenantUserRepo: tenantUserRepo}
}

type ListDestinationsFilter struct {
	Category string
	Region   string
	Season   string
	Featured *bool
	Page     int
	Limit    int
}

func (s *DestinationService) List(ctx context.Context, tenantID primitive.ObjectID, f ListDestinationsFilter) ([]*models.Destination, int64, error) {
	filter := bson.M{"is_active": true}
	if f.Category != "" {
		filter["categories"] = f.Category
	}
	if f.Region != "" {
		filter["region"] = f.Region
	}
	if f.Season != "" {
		filter["best_seasons"] = f.Season
	}
	if f.Featured != nil {
		filter["featured"] = *f.Featured
	}

	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}

	return s.repo.FindAll(ctx, tenantID, filter, f.Page, f.Limit)
}

// ListAdmin returns every destination for a tenant regardless of active
// status, for the admin CMS.
func (s *DestinationService) ListAdmin(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.Destination, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	destinations, total, err := s.repo.FindAll(ctx, tenantID, bson.M{}, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, destinations); err != nil {
		return nil, 0, apierr.Internal()
	}
	return destinations, total, nil
}

func (s *DestinationService) GetByID(ctx context.Context, tenantID primitive.ObjectID, idStr string) (*models.Destination, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, apierr.BadRequest("invalid id")
	}
	d, err := s.repo.FindByID(ctx, tenantID, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("destination not found")
		}
		return nil, apierr.Internal()
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, []*models.Destination{d}); err != nil {
		return nil, apierr.Internal()
	}
	return d, nil
}

// resolveLastEditedBy populates each destination's LastEditedBy with the
// display name of the tenant user referenced by its UserID, for admin GET
// responses.
func (s *DestinationService) resolveLastEditedBy(ctx context.Context, tenantID primitive.ObjectID, destinations []*models.Destination) error {
	ids := make([]primitive.ObjectID, 0, len(destinations))
	seen := make(map[primitive.ObjectID]bool, len(destinations))
	for _, d := range destinations {
		if d.UserID != nil && !seen[*d.UserID] {
			seen[*d.UserID] = true
			ids = append(ids, *d.UserID)
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

	for _, d := range destinations {
		if d.UserID == nil {
			continue
		}
		if name, ok := names[*d.UserID]; ok {
			d.LastEditedBy = &name
		}
	}
	return nil
}

func (s *DestinationService) GetBySlug(ctx context.Context, tenantID primitive.ObjectID, slug string) (*models.Destination, error) {
	d, err := s.repo.FindBySlug(ctx, tenantID, slug)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("destination not found")
		}
		return nil, apierr.Internal()
	}
	return d, nil
}

func (s *DestinationService) Create(ctx context.Context, tenantID primitive.ObjectID, d *models.Destination, userID *primitive.ObjectID) error {
	if d.Slug == "" {
		return apierr.BadRequest("slug is required")
	}
	d.IsActive = true
	return s.repo.Create(ctx, tenantID, d, userID)
}

func (s *DestinationService) Update(ctx context.Context, tenantID primitive.ObjectID, idStr string, update bson.M, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	if _, err := s.repo.FindByID(ctx, tenantID, id); err != nil {
		return apierr.NotFound("destination not found")
	}
	return s.repo.Update(ctx, tenantID, id, update, userID)
}

func (s *DestinationService) AddImage(ctx context.Context, tenantID primitive.ObjectID, idStr string, img models.Image) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.AddImage(ctx, tenantID, id, img)
}

func (s *DestinationService) Delete(ctx context.Context, tenantID primitive.ObjectID, idStr string, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.Delete(ctx, tenantID, id, userID)
}
