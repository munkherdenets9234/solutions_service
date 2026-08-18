package service

import (
	"context"
	"net/http"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type SubscriptionService struct {
	repo             *repository.SubscriptionRepo
	platformUserRepo *repository.PlatformUserRepo
	packageRepo      *repository.PackageRepo
}

func NewSubscriptionService(repo *repository.SubscriptionRepo, platformUserRepo *repository.PlatformUserRepo, packageRepo *repository.PackageRepo) *SubscriptionService {
	return &SubscriptionService{repo: repo, platformUserRepo: platformUserRepo, packageRepo: packageRepo}
}

const subscriptionPeriodDays = 30

// Create starts a subscription for a tenant on the given package, covering
// the next 30 days from now. A tenant can only ever have one subscription
// record - use UpdatePackage/Cancel to change it afterward.
func (s *SubscriptionService) Create(ctx context.Context, tenantID primitive.ObjectID, packageID primitive.ObjectID, userID *primitive.ObjectID) (*models.Subscription, error) {
	if err := s.validatePackage(ctx, packageID); err != nil {
		return nil, err
	}

	now := time.Now()
	sub := &models.Subscription{
		TenantID:           tenantID,
		PackageID:          packageID,
		Status:             models.SubscriptionActive,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(0, 0, subscriptionPeriodDays),
	}
	if err := s.repo.Create(ctx, sub, userID); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, apierr.New(http.StatusConflict, "tenant already has a subscription")
		}
		return nil, apierr.Internal()
	}
	if err := s.resolvePackage(ctx, sub); err != nil {
		return nil, apierr.Internal()
	}
	return sub, nil
}

func (s *SubscriptionService) validatePackage(ctx context.Context, packageID primitive.ObjectID) error {
	if _, err := s.packageRepo.FindByID(ctx, packageID); err != nil {
		if err == mongo.ErrNoDocuments {
			return apierr.BadRequest("invalid package_id")
		}
		return apierr.Internal()
	}
	return nil
}

func (s *SubscriptionService) Get(ctx context.Context, tenantID primitive.ObjectID) (*models.Subscription, error) {
	sub, err := s.repo.FindByTenantID(ctx, tenantID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("tenant has no subscription")
		}
		return nil, apierr.Internal()
	}
	if err := s.resolveLastEditedBy(ctx, sub); err != nil {
		return nil, apierr.Internal()
	}
	if err := s.resolvePackage(ctx, sub); err != nil {
		return nil, apierr.Internal()
	}
	return sub, nil
}

// resolvePackage populates sub.Package with the referenced package, if it
// still exists — left nil (not an error) if the package was since deleted,
// so a subscription record never 500s just because its package went away.
func (s *SubscriptionService) resolvePackage(ctx context.Context, sub *models.Subscription) error {
	pkg, err := s.packageRepo.FindByID(ctx, sub.PackageID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil
		}
		return err
	}
	sub.Package = pkg
	return nil
}

// resolveLastEditedBy populates sub's LastEditedBy with the display name of
// the platform superadmin referenced by its UserID, for the platform GET
// response. Unlike every other entity, subscriptions are only ever managed
// via /platform routes, so this resolves against PlatformUserRepo rather
// than TenantUserRepo.
func (s *SubscriptionService) resolveLastEditedBy(ctx context.Context, sub *models.Subscription) error {
	if sub.UserID == nil {
		return nil
	}
	u, err := s.platformUserRepo.FindByID(ctx, *sub.UserID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil
		}
		return err
	}
	sub.LastEditedBy = &u.Name
	return nil
}

// UpdatePackage moves the tenant onto a different package and starts a
// fresh billing period.
func (s *SubscriptionService) UpdatePackage(ctx context.Context, tenantID primitive.ObjectID, packageID primitive.ObjectID, userID *primitive.ObjectID) error {
	if err := s.validatePackage(ctx, packageID); err != nil {
		return err
	}
	if _, err := s.repo.FindByTenantID(ctx, tenantID); err != nil {
		if err == mongo.ErrNoDocuments {
			return apierr.NotFound("tenant has no subscription")
		}
		return apierr.Internal()
	}

	now := time.Now()
	return s.repo.UpdatePackage(ctx, tenantID, packageID, now, now.AddDate(0, 0, subscriptionPeriodDays), userID)
}

// Cancel marks the subscription canceled but leaves plan/period intact so
// access can still be checked against current_period_end if needed.
func (s *SubscriptionService) Cancel(ctx context.Context, tenantID primitive.ObjectID, userID *primitive.ObjectID) error {
	if _, err := s.repo.FindByTenantID(ctx, tenantID); err != nil {
		if err == mongo.ErrNoDocuments {
			return apierr.NotFound("tenant has no subscription")
		}
		return apierr.Internal()
	}
	return s.repo.UpdateStatus(ctx, tenantID, models.SubscriptionCanceled, userID)
}
