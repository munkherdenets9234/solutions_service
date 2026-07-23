package service

import (
	"context"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type NewsletterService struct {
	repo *repository.NewsletterRepo
}

func NewNewsletterService(repo *repository.NewsletterRepo) *NewsletterService {
	return &NewsletterService{repo: repo}
}

func (s *NewsletterService) Subscribe(ctx context.Context, tenantID primitive.ObjectID, m *models.NewsletterSubscriber) error {
	if m.Email == "" {
		return apierr.BadRequest("email is required")
	}
	return s.repo.Create(ctx, tenantID, m)
}

func (s *NewsletterService) List(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.NewsletterSubscriber, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.repo.FindAll(ctx, tenantID, page, limit)
}

func (s *NewsletterService) Delete(ctx context.Context, tenantID primitive.ObjectID, idStr string) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.Delete(ctx, tenantID, id)
}
