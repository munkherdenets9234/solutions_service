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
	repo *repository.DestinationRepo
}

func NewDestinationService(repo *repository.DestinationRepo) *DestinationService {
	return &DestinationService{repo: repo}
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

func (s *DestinationService) Create(ctx context.Context, tenantID primitive.ObjectID, d *models.Destination) error {
	if d.Slug == "" {
		return apierr.BadRequest("slug is required")
	}
	d.IsActive = true
	return s.repo.Create(ctx, tenantID, d)
}

func (s *DestinationService) Update(ctx context.Context, tenantID primitive.ObjectID, idStr string, update bson.M) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	if _, err := s.repo.FindByID(ctx, tenantID, id); err != nil {
		return apierr.NotFound("destination not found")
	}
	return s.repo.Update(ctx, tenantID, id, update)
}

func (s *DestinationService) AddImage(ctx context.Context, tenantID primitive.ObjectID, idStr string, img models.Image) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.AddImage(ctx, tenantID, id, img)
}

func (s *DestinationService) Delete(ctx context.Context, tenantID primitive.ObjectID, idStr string) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.Delete(ctx, tenantID, id)
}
