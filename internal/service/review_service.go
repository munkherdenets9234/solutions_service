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

type ReviewService struct {
	repo *repository.ReviewRepo
}

func NewReviewService(repo *repository.ReviewRepo) *ReviewService {
	return &ReviewService{repo: repo}
}

type ListReviewsFilter struct {
	Tour    string
	Partner string
	Page    int
	Limit   int
}

func (s *ReviewService) List(ctx context.Context, tenantID primitive.ObjectID, f ListReviewsFilter) ([]*models.Review, int64, error) {
	filter := bson.M{}
	if f.Tour != "" {
		filter["related_tour"] = f.Tour
	}
	if f.Partner != "" {
		filter["related_partner"] = f.Partner
	}

	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}

	return s.repo.FindAll(ctx, tenantID, filter, f.Page, f.Limit)
}

func (s *ReviewService) GetByID(ctx context.Context, tenantID primitive.ObjectID, idStr string) (*models.Review, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, apierr.BadRequest("invalid id")
	}
	rev, err := s.repo.FindByID(ctx, tenantID, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("review not found")
		}
		return nil, apierr.Internal()
	}
	return rev, nil
}

func (s *ReviewService) Create(ctx context.Context, tenantID primitive.ObjectID, rev *models.Review) error {
	if rev.Star < 1 || rev.Star > 5 {
		return apierr.BadRequest("star must be between 1 and 5")
	}
	if rev.Review == "" {
		return apierr.BadRequest("review is required")
	}
	return s.repo.Create(ctx, tenantID, rev)
}

func (s *ReviewService) Update(ctx context.Context, tenantID primitive.ObjectID, idStr string, update bson.M) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	// A JSON body decodes numbers into float64 in a bson.M.
	if v, ok := update["star"]; ok {
		f, isNum := v.(float64)
		if !isNum || f != float64(int(f)) || int(f) < 1 || int(f) > 5 {
			return apierr.BadRequest("star must be between 1 and 5")
		}
	}
	if _, err := s.repo.FindByID(ctx, tenantID, id); err != nil {
		return apierr.NotFound("review not found")
	}
	return s.repo.Update(ctx, tenantID, id, update)
}

func (s *ReviewService) Delete(ctx context.Context, tenantID primitive.ObjectID, idStr string) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	deleted, err := s.repo.Delete(ctx, tenantID, id)
	if err != nil {
		return apierr.Internal()
	}
	if deleted == 0 {
		return apierr.NotFound("review not found")
	}
	return nil
}
