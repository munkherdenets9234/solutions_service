package service

import (
	"context"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ContactMessageService struct {
	repo           *repository.ContactMessageRepo
	tenantUserRepo *repository.TenantUserRepo
}

func NewContactMessageService(repo *repository.ContactMessageRepo, tenantUserRepo *repository.TenantUserRepo) *ContactMessageService {
	return &ContactMessageService{repo: repo, tenantUserRepo: tenantUserRepo}
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
	messages, total, err := s.repo.FindAll(ctx, tenantID, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, messages); err != nil {
		return nil, 0, apierr.Internal()
	}
	return messages, total, nil
}

// resolveLastEditedBy populates each message's LastEditedBy with the display
// name of the tenant user referenced by its UserID, for admin GET responses.
func (s *ContactMessageService) resolveLastEditedBy(ctx context.Context, tenantID primitive.ObjectID, messages []*models.ContactMessage) error {
	ids := make([]primitive.ObjectID, 0, len(messages))
	seen := make(map[primitive.ObjectID]bool, len(messages))
	for _, m := range messages {
		if m.UserID != nil && !seen[*m.UserID] {
			seen[*m.UserID] = true
			ids = append(ids, *m.UserID)
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

	for _, m := range messages {
		if m.UserID == nil {
			continue
		}
		if name, ok := names[*m.UserID]; ok {
			m.LastEditedBy = &name
		}
	}
	return nil
}

func (s *ContactMessageService) UpdateStatus(ctx context.Context, tenantID primitive.ObjectID, idStr string, status models.ContactStatus, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.UpdateStatus(ctx, tenantID, id, status, userID)
}
