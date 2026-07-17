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

type CarService struct {
	repo           *repository.CarRepo
	tenantUserRepo *repository.TenantUserRepo
}

func NewCarService(repo *repository.CarRepo, tenantUserRepo *repository.TenantUserRepo) *CarService {
	return &CarService{repo: repo, tenantUserRepo: tenantUserRepo}
}

type ListCarsFilter struct {
	Type  string
	Fuel  string
	Page  int
	Limit int
}

func (s *CarService) List(ctx context.Context, tenantID primitive.ObjectID, f ListCarsFilter) ([]*models.Car, int64, error) {
	filter := bson.M{"is_active": true}
	if f.Type != "" {
		filter["type"] = f.Type
	}
	if f.Fuel != "" {
		filter["fuel"] = f.Fuel
	}

	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}

	cars, total, err := s.repo.FindAll(ctx, tenantID, filter, f.Page, f.Limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, cars); err != nil {
		return nil, 0, apierr.Internal()
	}
	return cars, total, nil
}

func (s *CarService) GetBySlug(ctx context.Context, tenantID primitive.ObjectID, slug string) (*models.Car, error) {
	c, err := s.repo.FindBySlug(ctx, tenantID, slug)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("car not found")
		}
		return nil, apierr.Internal()
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, []*models.Car{c}); err != nil {
		return nil, apierr.Internal()
	}
	return c, nil
}

// resolveLastEditedBy populates each car's LastEditedBy with the display
// name of the tenant user referenced by its UserID.
func (s *CarService) resolveLastEditedBy(ctx context.Context, tenantID primitive.ObjectID, cars []*models.Car) error {
	ids := make([]primitive.ObjectID, 0, len(cars))
	seen := make(map[primitive.ObjectID]bool, len(cars))
	for _, c := range cars {
		if c.UserID != nil && !seen[*c.UserID] {
			seen[*c.UserID] = true
			ids = append(ids, *c.UserID)
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

	for _, c := range cars {
		if c.UserID == nil {
			continue
		}
		if name, ok := names[*c.UserID]; ok {
			c.LastEditedBy = &name
		}
	}
	return nil
}

func (s *CarService) Create(ctx context.Context, tenantID primitive.ObjectID, c *models.Car, userID *primitive.ObjectID) error {
	if c.Slug == "" {
		return apierr.BadRequest("slug is required")
	}
	c.IsActive = true
	return s.repo.Create(ctx, tenantID, c, userID)
}

func (s *CarService) Update(ctx context.Context, tenantID primitive.ObjectID, idStr string, update bson.M, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	if _, err := s.repo.FindByID(ctx, tenantID, id); err != nil {
		return apierr.NotFound("car not found")
	}
	return s.repo.Update(ctx, tenantID, id, update, userID)
}

func (s *CarService) Delete(ctx context.Context, tenantID primitive.ObjectID, idStr string, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.Delete(ctx, tenantID, id, userID)
}
