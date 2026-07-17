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
	repo *repository.CarRepo
}

func NewCarService(repo *repository.CarRepo) *CarService {
	return &CarService{repo: repo}
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

	return s.repo.FindAll(ctx, tenantID, filter, f.Page, f.Limit)
}

func (s *CarService) GetBySlug(ctx context.Context, tenantID primitive.ObjectID, slug string) (*models.Car, error) {
	c, err := s.repo.FindBySlug(ctx, tenantID, slug)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("car not found")
		}
		return nil, apierr.Internal()
	}
	return c, nil
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

func (s *CarService) Delete(ctx context.Context, tenantID primitive.ObjectID, idStr string) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.Delete(ctx, tenantID, id)
}
