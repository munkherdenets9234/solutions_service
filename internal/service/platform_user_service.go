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

type PlatformUserService struct {
	repo        *repository.PlatformUserRepo
	maker       *token.Maker
	tokenExpiry time.Duration
}

func NewPlatformUserService(repo *repository.PlatformUserRepo, maker *token.Maker, tokenExpiryHours int) *PlatformUserService {
	return &PlatformUserService{repo: repo, maker: maker, tokenExpiry: time.Duration(tokenExpiryHours) * time.Hour}
}

// EnsureBootstrap creates the first platform user from startup config if no
// account with that email exists yet. It never overwrites an existing
// account's password — restarting the server with different env values
// must not silently change who can log in.
func (s *PlatformUserService) EnsureBootstrap(ctx context.Context, name, email, rawPassword string) error {
	if email == "" || rawPassword == "" {
		return nil
	}
	if _, err := s.repo.FindByEmail(ctx, email); err == nil {
		return nil
	} else if err != mongo.ErrNoDocuments {
		return err
	}

	hash, err := password.Hash(rawPassword)
	if err != nil {
		return err
	}
	return s.repo.Create(ctx, &models.PlatformUser{Name: name, Email: email, PasswordHash: hash})
}

// Create adds another platform user. If rawPassword is empty, one is
// generated and returned — the only time it is ever available in plaintext.
func (s *PlatformUserService) Create(ctx context.Context, name, email, rawPassword string) (*models.PlatformUser, string, error) {
	if email == "" {
		return nil, "", apierr.BadRequest("email is required")
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

	u := &models.PlatformUser{Name: name, Email: email, PasswordHash: hash}
	if err := s.repo.Create(ctx, u); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, "", apierr.New(http.StatusConflict, "a platform user with this email already exists")
		}
		return nil, "", apierr.Internal()
	}
	return u, rawPassword, nil
}

func (s *PlatformUserService) List(ctx context.Context, page, limit int) ([]*models.PlatformUser, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.repo.FindAll(ctx, page, limit)
}

// Login verifies email/password and issues a bearer token with the
// "superadmin" role and no tenant scope.
func (s *PlatformUserService) Login(ctx context.Context, email, plainPassword string) (string, error) {
	u, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return "", apierr.Unauthorized()
		}
		return "", apierr.Internal()
	}
	if u.Status != models.PlatformUserActive {
		return "", apierr.New(http.StatusForbidden, "account suspended")
	}
	if !password.Verify(u.PasswordHash, plainPassword) {
		return "", apierr.Unauthorized()
	}

	tok, _, err := s.maker.CreateToken(u.ID.Hex(), "superadmin", "", s.tokenExpiry)
	if err != nil {
		return "", apierr.Internal()
	}
	return tok, nil
}

// UpdateStatus refuses to suspend the last active platform user — otherwise
// nothing could ever log in to reactivate one again.
func (s *PlatformUserService) UpdateStatus(ctx context.Context, idStr string, status models.PlatformUserStatus) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}

	if status == models.PlatformUserSuspended {
		active, err := s.repo.CountActive(ctx)
		if err != nil {
			return apierr.Internal()
		}
		if active <= 1 {
			return apierr.New(http.StatusConflict, "cannot suspend the last active platform user")
		}
	}

	return s.repo.UpdateStatus(ctx, id, status)
}
