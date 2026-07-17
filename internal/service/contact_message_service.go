package service

import (
	"context"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ContactMessageService struct {
	repo *repository.ContactMessageRepo
}

func NewContactMessageService(repo *repository.ContactMessageRepo) *ContactMessageService {
	return &ContactMessageService{repo: repo}
}

func (s *ContactMessageService) Create(ctx context.Context, tenantID primitive.ObjectID, m *models.ContactMessage) error {
	if m.Name == "" || m.Email == "" || m.Message == "" {
		return apierr.BadRequest("name, email and message are required")
	}
	return s.repo.Create(ctx, tenantID, m)
}

func (s *ContactMessageService) List(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.ContactMessage, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.repo.FindAll(ctx, tenantID, page, limit)
}

func (s *ContactMessageService) UpdateStatus(ctx context.Context, tenantID primitive.ObjectID, idStr string, status models.ContactStatus, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.UpdateStatus(ctx, tenantID, id, status, userID)
}
