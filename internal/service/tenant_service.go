package service

import (
	"context"
	"net/http"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/apikey"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type TenantService struct {
	repo *repository.TenantRepo
}

func NewTenantService(repo *repository.TenantRepo) *TenantService {
	return &TenantService{repo: repo}
}

// Create returns the created tenant and the raw API key — the raw key is
// only ever available here, at creation time.
func (s *TenantService) Create(ctx context.Context, t *models.Tenant) (*models.Tenant, string, error) {
	if t.Slug == "" {
		return nil, "", apierr.BadRequest("slug is required")
	}

	raw, hash, err := apikey.Generate()
	if err != nil {
		return nil, "", apierr.Internal()
	}
	t.APIKeyHash = hash
	t.APIKeyLast4 = apikey.Last4(raw)

	if err := s.repo.Create(ctx, t); err != nil {
		return nil, "", apierr.Internal()
	}
	return t, raw, nil
}

func (s *TenantService) List(ctx context.Context, page, limit int) ([]*models.Tenant, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.repo.FindAll(ctx, page, limit)
}

func (s *TenantService) GetByID(ctx context.Context, idStr string) (*models.Tenant, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, apierr.BadRequest("invalid id")
	}
	t, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("tenant not found")
		}
		return nil, apierr.Internal()
	}
	return t, nil
}

func (s *TenantService) UpdateStatus(ctx context.Context, idStr string, status models.TenantStatus) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.UpdateStatus(ctx, id, status)
}

// RotateAPIKey issues a new API key for the tenant and invalidates the old one.
func (s *TenantService) RotateAPIKey(ctx context.Context, idStr string) (string, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return "", apierr.BadRequest("invalid id")
	}
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		if err == mongo.ErrNoDocuments {
			return "", apierr.NotFound("tenant not found")
		}
		return "", apierr.Internal()
	}

	raw, hash, err := apikey.Generate()
	if err != nil {
		return "", apierr.Internal()
	}
	if err := s.repo.RotateAPIKey(ctx, id, hash, apikey.Last4(raw)); err != nil {
		return "", apierr.Internal()
	}
	return raw, nil
}

// Resolve looks up an active tenant by its raw API key, for request-time
// tenant resolution (used by TenantMiddleware).
func (s *TenantService) Resolve(ctx context.Context, rawAPIKey string) (*models.Tenant, error) {
	t, err := s.repo.FindByAPIKeyHash(ctx, apikey.Hash(rawAPIKey))
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.Unauthorized()
		}
		return nil, apierr.Internal()
	}
	if t.Status != models.TenantActive {
		return nil, apierr.New(http.StatusForbidden, "tenant suspended")
	}
	return t, nil
}
