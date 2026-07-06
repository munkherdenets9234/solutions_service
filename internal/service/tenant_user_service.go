package service

import (
	"context"
	"net/http"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/password"
	"github.com/eandstravel/digitalservice/pkg/token"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type TenantUserService struct {
	repo        *repository.TenantUserRepo
	maker       *token.Maker
	tokenExpiry time.Duration
}

func NewTenantUserService(repo *repository.TenantUserRepo, maker *token.Maker, tokenExpiryHours int) *TenantUserService {
	return &TenantUserService{repo: repo, maker: maker, tokenExpiry: time.Duration(tokenExpiryHours) * time.Hour}
}

// Create adds a login profile for the tenant. If rawPassword is empty, one
// is generated and returned — the only time it is ever available in
// plaintext, so callers must surface it to the caller immediately.
func (s *TenantUserService) Create(ctx context.Context, tenantID primitive.ObjectID, name, email, rawPassword string, role models.TenantUserRole) (*models.TenantUser, string, error) {
	if email == "" {
		return nil, "", apierr.BadRequest("email is required")
	}
	if role == "" {
		role = models.TenantUserStaff
	} else if role != models.TenantUserAdmin && role != models.TenantUserStaff {
		return nil, "", apierr.BadRequest("invalid role")
	}

	generated := rawPassword == ""
	if generated {
		var err error
		rawPassword, err = password.GenerateRandom()
		if err != nil {
			return nil, "", apierr.Internal()
		}
	} else if len(rawPassword) < 8 {
		return nil, "", apierr.BadRequest("password must be at least 8 characters")
	}

	hash, err := password.Hash(rawPassword)
	if err != nil {
		return nil, "", apierr.Internal()
	}

	u := &models.TenantUser{
		TenantID:     tenantID,
		Name:         name,
		Email:        email,
		PasswordHash: hash,
		Role:         role,
	}
	if err := s.repo.Create(ctx, u); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, "", apierr.New(http.StatusConflict, "a login profile with this email already exists for this tenant")
		}
		return nil, "", apierr.Internal()
	}
	return u, rawPassword, nil
}

func (s *TenantUserService) List(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.TenantUser, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.repo.FindAll(ctx, tenantID, page, limit)
}

// Login verifies email/password against the tenant scoped by tenantID (as
// resolved by TenantMiddleware from X-API-Key) and issues a bearer token
// carrying the user's role and tenant.
func (s *TenantUserService) Login(ctx context.Context, tenantID primitive.ObjectID, email, plainPassword string) (string, error) {
	u, err := s.repo.FindByTenantAndEmail(ctx, tenantID, email)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return "", apierr.Unauthorized()
		}
		return "", apierr.Internal()
	}
	if u.Status != models.TenantUserActive {
		return "", apierr.New(http.StatusForbidden, "login profile suspended")
	}
	if !password.Verify(u.PasswordHash, plainPassword) {
		return "", apierr.Unauthorized()
	}

	tok, _, err := s.maker.CreateToken(u.ID.Hex(), string(u.Role), tenantID.Hex(), s.tokenExpiry)
	if err != nil {
		return "", apierr.Internal()
	}
	return tok, nil
}

func (s *TenantUserService) UpdateStatus(ctx context.Context, tenantID primitive.ObjectID, idStr string, status models.TenantUserStatus) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.UpdateStatus(ctx, tenantID, id, status)
}

// ChangePassword lets a logged-in tenant user (any role) change their own
// password, after verifying the current one.
func (s *TenantUserService) ChangePassword(ctx context.Context, tenantID, userID primitive.ObjectID, currentPassword, newPassword string) error {
	u, err := s.repo.FindByID(ctx, tenantID, userID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return apierr.NotFound("login profile not found")
		}
		return apierr.Internal()
	}
	if !password.Verify(u.PasswordHash, currentPassword) {
		return apierr.Unauthorized()
	}
	if len(newPassword) < 8 {
		return apierr.BadRequest("password must be at least 8 characters")
	}

	hash, err := password.Hash(newPassword)
	if err != nil {
		return apierr.Internal()
	}
	return s.repo.UpdatePassword(ctx, tenantID, userID, hash)
}

// ResetPassword lets a tenant admin reset another login profile's password
// in their own tenant (e.g. a staff member who forgot theirs), without
// knowing the current one. If newPassword is empty, one is generated and
// returned — the only time it is ever available in plaintext.
func (s *TenantUserService) ResetPassword(ctx context.Context, tenantID primitive.ObjectID, idStr, newPassword string) (string, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return "", apierr.BadRequest("invalid id")
	}
	if _, err := s.repo.FindByID(ctx, tenantID, id); err != nil {
		if err == mongo.ErrNoDocuments {
			return "", apierr.NotFound("login profile not found")
		}
		return "", apierr.Internal()
	}

	if newPassword == "" {
		newPassword, err = password.GenerateRandom()
		if err != nil {
			return "", apierr.Internal()
		}
	} else if len(newPassword) < 8 {
		return "", apierr.BadRequest("password must be at least 8 characters")
	}

	hash, err := password.Hash(newPassword)
	if err != nil {
		return "", apierr.Internal()
	}
	if err := s.repo.UpdatePassword(ctx, tenantID, id, hash); err != nil {
		return "", apierr.Internal()
	}
	return newPassword, nil
}
