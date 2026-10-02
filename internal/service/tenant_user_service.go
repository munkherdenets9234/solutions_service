package service

import (
	"context"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/password"
	"github.com/eandstravel/digitalservice/pkg/token"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// TenantUserStore is the part of the tenant_users collection this service
// uses, as an interface so the admin-user rules can be tested without a
// database. *repository.TenantUserRepo satisfies it.
type TenantUserStore interface {
	Create(ctx context.Context, u *models.TenantUser) error
	FindAll(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.TenantUser, int64, error)
	FindByTenantAndEmail(ctx context.Context, tenantID primitive.ObjectID, email string) (*models.TenantUser, error)
	FindByID(ctx context.Context, tenantID, id primitive.ObjectID) (*models.TenantUser, error)
	FindAdmins(ctx context.Context, tenantID primitive.ObjectID) ([]*models.TenantUser, error)
	UpdatePassword(ctx context.Context, tenantID, id primitive.ObjectID, passwordHash string) error
	UpdateStatus(ctx context.Context, tenantID, id primitive.ObjectID, status models.TenantUserStatus) error
}

type TenantUserService struct {
	repo        TenantUserStore
	maker       *token.Maker
	tokenExpiry time.Duration
}

func NewTenantUserService(repo *repository.TenantUserRepo, maker *token.Maker, tokenExpiryHours int) *TenantUserService {
	return NewTenantUserServiceFromStore(repo, maker, tokenExpiryHours)
}

// NewTenantUserServiceFromStore builds the service over any store; tests use it
// to supply a fake.
func NewTenantUserServiceFromStore(store TenantUserStore, maker *token.Maker, tokenExpiryHours int) *TenantUserService {
	return &TenantUserService{repo: store, maker: maker, tokenExpiry: time.Duration(tokenExpiryHours) * time.Hour}
}

// ListAdmins returns the tenant's admin-role users, any status. The store
// already filters; this re-checks role and tenant so the result stays correct
// whatever the store does.
func (s *TenantUserService) ListAdmins(ctx context.Context, tenantID primitive.ObjectID) ([]*models.TenantUser, error) {
	users, err := s.repo.FindAdmins(ctx, tenantID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]*models.TenantUser, 0, len(users))
	for _, u := range users {
		if u.TenantID == tenantID && u.Role == models.TenantUserAdmin {
			out = append(out, u)
		}
	}
	return out, nil
}

// GetAdmin loads one admin-role user of the tenant. A user that is absent, is
// not an admin, or belongs to another tenant is NotFound: the caller cannot
// tell which, and a reset must never be started for any of them.
func (s *TenantUserService) GetAdmin(ctx context.Context, tenantID primitive.ObjectID, idStr string) (*models.TenantUser, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, apierr.BadRequest("invalid id")
	}
	u, err := s.repo.FindByID(ctx, tenantID, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("admin user")
		}
		return nil, apierr.Internal(err)
	}
	if u.TenantID != tenantID || u.Role != models.TenantUserAdmin {
		return nil, apierr.NotFound("admin user")
	}
	return u, nil
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
			return nil, "", apierr.Internal(err)
		}
	} else if len(rawPassword) < 8 {
		return nil, "", apierr.BadRequest("password must be at least 8 characters")
	}

	hash, err := password.Hash(rawPassword)
	if err != nil {
		return nil, "", apierr.Internal(err)
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
			return nil, "", apierr.Conflict("a login profile with this email already exists for this tenant")
		}
		return nil, "", apierr.Internal(err)
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
			return "", apierr.Unauthorized("")
		}
		return "", apierr.Internal(err)
	}
	if u.Status != models.TenantUserActive {
		return "", apierr.Forbidden("login profile suspended")
	}
	if !password.Verify(u.PasswordHash, plainPassword) {
		return "", apierr.Unauthorized("")
	}

	tok, _, err := s.maker.CreateToken(u.ID.Hex(), string(u.Role), tenantID.Hex(), s.tokenExpiry)
	if err != nil {
		return "", apierr.Internal(err)
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
		return apierr.Internal(err)
	}
	if !password.Verify(u.PasswordHash, currentPassword) {
		return apierr.Unauthorized("")
	}
	if len(newPassword) < 8 {
		return apierr.BadRequest("password must be at least 8 characters")
	}

	hash, err := password.Hash(newPassword)
	if err != nil {
		return apierr.Internal(err)
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
		return "", apierr.Internal(err)
	}

	if newPassword == "" {
		newPassword, err = password.GenerateRandom()
		if err != nil {
			return "", apierr.Internal(err)
		}
	} else if len(newPassword) < 8 {
		return "", apierr.BadRequest("password must be at least 8 characters")
	}

	hash, err := password.Hash(newPassword)
	if err != nil {
		return "", apierr.Internal(err)
	}
	if err := s.repo.UpdatePassword(ctx, tenantID, id, hash); err != nil {
		return "", apierr.Internal(err)
	}
	return newPassword, nil
}
