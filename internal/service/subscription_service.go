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
}

func NewSubscriptionService(repo *repository.SubscriptionRepo, platformUserRepo *repository.PlatformUserRepo) *SubscriptionService {
	return &SubscriptionService{repo: repo, platformUserRepo: platformUserRepo}
}

var validPlans = map[models.SubscriptionPlan]bool{
	models.PlanFree:       true,
	models.PlanBasic:      true,
	models.PlanPro:        true,
	models.PlanEnterprise: true,
}

const subscriptionPeriodDays = 30

// Create starts a subscription for a tenant on the given plan, covering the
// next 30 days from now. A tenant can only ever have one subscription
// record - use UpdatePlan/Cancel to change it afterward.
func (s *SubscriptionService) Create(ctx context.Context, tenantID primitive.ObjectID, plan models.SubscriptionPlan, userID *primitive.ObjectID) (*models.Subscription, error) {
	if plan == "" {
		plan = models.PlanFree
	} else if !validPlans[plan] {
		return nil, apierr.BadRequest("invalid plan")
	}

	now := time.Now()
	sub := &models.Subscription{
		TenantID:           tenantID,
		Plan:               plan,
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
	return sub, nil
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
	return sub, nil
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

// UpdatePlan changes the tenant's plan and starts a fresh billing period.
func (s *SubscriptionService) UpdatePlan(ctx context.Context, tenantID primitive.ObjectID, plan models.SubscriptionPlan, userID *primitive.ObjectID) error {
	if !validPlans[plan] {
		return apierr.BadRequest("invalid plan")
	}
	if _, err := s.repo.FindByTenantID(ctx, tenantID); err != nil {
		if err == mongo.ErrNoDocuments {
			return apierr.NotFound("tenant has no subscription")
		}
		return apierr.Internal()
	}

	now := time.Now()
	return s.repo.UpdatePlan(ctx, tenantID, plan, now, now.AddDate(0, 0, subscriptionPeriodDays), userID)
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
