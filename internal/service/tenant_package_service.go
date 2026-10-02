package service

import (
	"context"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// TenantPackageService manages the many-to-many assignment between tenants
// and the platform's global Package catalog (see models.TenantPackage).
type TenantPackageService struct {
	repo       *repository.TenantPackageRepo
	tenantRepo *repository.TenantRepo
	pkgRepo    *repository.PackageRepo
}

func NewTenantPackageService(repo *repository.TenantPackageRepo, tenantRepo *repository.TenantRepo, pkgRepo *repository.PackageRepo) *TenantPackageService {
	return &TenantPackageService{repo: repo, tenantRepo: tenantRepo, pkgRepo: pkgRepo}
}

// Assign links a package to a tenant's storefront. 400 if either id doesn't
// resolve, 409 if the pair is already assigned.
func (s *TenantPackageService) Assign(ctx context.Context, tenantIDStr, packageIDStr string, userID *primitive.ObjectID) error {
	tenantID, err := primitive.ObjectIDFromHex(tenantIDStr)
	if err != nil {
		return apierr.BadRequest("invalid tenant id")
	}
	packageID, err := primitive.ObjectIDFromHex(packageIDStr)
	if err != nil {
		return apierr.BadRequest("invalid package_id")
	}

	if _, err := s.tenantRepo.FindByID(ctx, tenantID); err != nil {
		if err == mongo.ErrNoDocuments {
			return apierr.NotFound("tenant not found")
		}
		return apierr.Internal(err)
	}
	if _, err := s.pkgRepo.FindByID(ctx, packageID); err != nil {
		if err == mongo.ErrNoDocuments {
			return apierr.BadRequest("invalid package_id")
		}
		return apierr.Internal(err)
	}

	if err := s.repo.Assign(ctx, tenantID, packageID, userID); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return apierr.Conflict("package is already assigned to this tenant")
		}
		return apierr.Internal(err)
	}
	return nil
}

func (s *TenantPackageService) Unassign(ctx context.Context, tenantIDStr, packageIDStr string) error {
	tenantID, err := primitive.ObjectIDFromHex(tenantIDStr)
	if err != nil {
		return apierr.BadRequest("invalid tenant id")
	}
	packageID, err := primitive.ObjectIDFromHex(packageIDStr)
	if err != nil {
		return apierr.BadRequest("invalid package_id")
	}

	deleted, err := s.repo.Unassign(ctx, tenantID, packageID)
	if err != nil {
		return apierr.Internal(err)
	}
	if deleted == 0 {
		return apierr.NotFound("assignment not found")
	}
	return nil
}

// ListForTenant returns every package assigned to a tenant, regardless of
// active status — the platform admin's view of that tenant's assignments.
func (s *TenantPackageService) ListForTenant(ctx context.Context, tenantIDStr string, page, limit int) ([]*models.Package, int64, error) {
	tenantID, err := primitive.ObjectIDFromHex(tenantIDStr)
	if err != nil {
		return nil, 0, apierr.BadRequest("invalid tenant id")
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	ids, err := s.repo.FindPackageIDsByTenant(ctx, tenantID)
	if err != nil {
		return nil, 0, apierr.Internal(err)
	}
	return s.pkgRepo.FindByIDs(ctx, ids, page, limit)
}
