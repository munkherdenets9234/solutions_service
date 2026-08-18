package service

import (
	"context"

	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type TenantReviewService struct {
	repo       *repository.TenantReviewRepo
	tenantRepo *repository.TenantRepo
}

func NewTenantReviewService(repo *repository.TenantReviewRepo, tenantRepo *repository.TenantRepo) *TenantReviewService {
	return &TenantReviewService{repo: repo, tenantRepo: tenantRepo}
}

// List returns active tenant reviews — the public "get all" testimonials
// listing.
func (s *TenantReviewService) List(ctx context.Context, page, limit int) ([]*models.TenantReview, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.repo.FindAll(ctx, bson.M{"is_active": true}, page, limit)
}

func (s *TenantReviewService) Create(ctx context.Context, rev *models.TenantReview, userID *primitive.ObjectID) error {
	if rev.TenantID.IsZero() {
		return apierr.BadRequest("tenant_id is required")
	}
	if _, err := s.tenantRepo.FindByID(ctx, rev.TenantID); err != nil {
		if err == mongo.ErrNoDocuments {
			return apierr.BadRequest("invalid tenant_id")
		}
		return apierr.Internal()
	}
	if rev.Rate < 1 || rev.Rate > 5 {
		return apierr.BadRequest("rate must be between 1 and 5")
	}
	if i18n.Resolve(rev.Comment, i18n.DefaultLocale) == "" {
		return apierr.BadRequest("comment is required")
	}
	if rev.CompanyName == "" {
		return apierr.BadRequest("company_name is required")
	}
	return s.repo.Create(ctx, rev, userID)
}

func (s *TenantReviewService) Update(ctx context.Context, idStr string, update bson.M, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	// A JSON body decodes numbers into float64 in a bson.M.
	if v, ok := update["rate"]; ok {
		f, isNum := v.(float64)
		if !isNum || f != float64(int(f)) || int(f) < 1 || int(f) > 5 {
			return apierr.BadRequest("rate must be between 1 and 5")
		}
	}
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		if err == mongo.ErrNoDocuments {
			return apierr.NotFound("tenant review not found")
		}
		return apierr.Internal()
	}
	return s.repo.Update(ctx, id, update, userID)
}

func (s *TenantReviewService) Delete(ctx context.Context, idStr string, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.Delete(ctx, id, userID)
}
