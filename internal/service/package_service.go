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

type PackageService struct {
	repo             *repository.PackageRepo
	tenantPackageRepo *repository.TenantPackageRepo
	platformUserRepo *repository.PlatformUserRepo
}

func NewPackageService(repo *repository.PackageRepo, tenantPackageRepo *repository.TenantPackageRepo, platformUserRepo *repository.PlatformUserRepo) *PackageService {
	return &PackageService{repo: repo, tenantPackageRepo: tenantPackageRepo, platformUserRepo: platformUserRepo}
}

// ListForTenant returns the active packages assigned to a tenant (see
// TenantPackage) — the tenant-scoped storefront price list.
func (s *PackageService) ListForTenant(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.Package, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	ids, err := s.tenantPackageRepo.FindPackageIDsByTenant(ctx, tenantID)
	if err != nil {
		return nil, 0, apierr.Internal()
	}
	return s.repo.FindByIDsActive(ctx, ids, page, limit)
}

// GetBySlugForTenant is ListForTenant's single-package counterpart — 404s if
// the package doesn't exist/is inactive, or isn't assigned to this tenant.
func (s *PackageService) GetBySlugForTenant(ctx context.Context, tenantID primitive.ObjectID, slug string) (*models.Package, error) {
	p, err := s.repo.FindBySlug(ctx, slug)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("package not found")
		}
		return nil, apierr.Internal()
	}
	ids, err := s.tenantPackageRepo.FindPackageIDsByTenant(ctx, tenantID)
	if err != nil {
		return nil, apierr.Internal()
	}
	for _, id := range ids {
		if id == p.ID {
			return p, nil
		}
	}
	return nil, apierr.NotFound("package not found")
}

// ListAll returns every package in the platform's catalog regardless of
// active status, with full locale maps intact, for the platform admin.
func (s *PackageService) ListAll(ctx context.Context, page, limit int) ([]*models.Package, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	pkgs, total, err := s.repo.FindAll(ctx, bson.M{}, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.resolveLastEditedBy(ctx, pkgs); err != nil {
		return nil, 0, apierr.Internal()
	}
	return pkgs, total, nil
}

func (s *PackageService) GetByID(ctx context.Context, idStr string) (*models.Package, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, apierr.BadRequest("invalid id")
	}
	p, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("package not found")
		}
		return nil, apierr.Internal()
	}
	if err := s.resolveLastEditedBy(ctx, []*models.Package{p}); err != nil {
		return nil, apierr.Internal()
	}
	return p, nil
}

// resolveLastEditedBy populates each package's LastEditedBy with the display
// name of the platform superadmin referenced by its UserID — packages are
// only ever managed via /platform routes, so this resolves against
// PlatformUserRepo, not TenantUserRepo.
func (s *PackageService) resolveLastEditedBy(ctx context.Context, pkgs []*models.Package) error {
	ids := make([]primitive.ObjectID, 0, len(pkgs))
	seen := make(map[primitive.ObjectID]bool, len(pkgs))
	for _, p := range pkgs {
		if p.UserID != nil && !seen[*p.UserID] {
			seen[*p.UserID] = true
			ids = append(ids, *p.UserID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	names := make(map[primitive.ObjectID]string, len(ids))
	for _, id := range ids {
		u, err := s.platformUserRepo.FindByID(ctx, id)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				continue
			}
			return err
		}
		names[id] = u.Name
	}

	for _, p := range pkgs {
		if p.UserID == nil {
			continue
		}
		if name, ok := names[*p.UserID]; ok {
			p.LastEditedBy = &name
		}
	}
	return nil
}

func (s *PackageService) Create(ctx context.Context, p *models.Package, userID *primitive.ObjectID) error {
	if p.Slug == "" {
		return apierr.BadRequest("slug is required")
	}
	if i18n.Resolve(p.Name, i18n.DefaultLocale) == "" {
		return apierr.BadRequest("name is required")
	}
	p.IsActive = true
	return s.repo.Create(ctx, p, userID)
}

func (s *PackageService) Update(ctx context.Context, idStr string, update bson.M, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		return apierr.NotFound("package not found")
	}
	return s.repo.Update(ctx, id, update, userID)
}

func (s *PackageService) Delete(ctx context.Context, idStr string, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.Delete(ctx, id, userID)
}
