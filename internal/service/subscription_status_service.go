package service

import (
	"context"

	"github.com/eandstravel/digitalservice/internal/entitlement"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

// SubscriptionState is the only thing the status route tells a client: whether
// the tenant's subscription is currently in a usable state.
type SubscriptionState string

const (
	SubscriptionActive  SubscriptionState = "active"
	SubscriptionExpired SubscriptionState = "expired"
)

// SubscriptionStatusService reduces an entitlement to the one bit a banner
// needs. It never returns an error: the answer drives a courtesy notice, so
// "we could not find out" must read as active, exactly as the subscription
// gate refuses to tell a paying tenant they have not paid on the strength of
// an unreachable platform.
type SubscriptionStatusService struct {
	provider entitlement.Provider
	log      *zap.Logger
}

func NewSubscriptionStatusService(p entitlement.Provider, log *zap.Logger) *SubscriptionStatusService {
	if log == nil {
		log = zap.NewNop()
	}
	return &SubscriptionStatusService{provider: p, log: log}
}

// State is fail-open: a nil service, nil provider, failed lookup, stale answer
// or tenant with no subscription record all report active. Only a fresh
// answer that says the subscription is not usable reports expired.
func (s *SubscriptionStatusService) State(ctx context.Context, tenantID primitive.ObjectID) SubscriptionState {
	if s == nil || s.provider == nil {
		return SubscriptionActive
	}
	ent, err := s.provider.For(ctx, tenantID)
	if err != nil {
		// Identifier and a fixed outcome only: the error text can carry
		// upstream addresses.
		s.log.Warn("subscription status lookup failed",
			zap.String("tenant_id", tenantID.Hex()),
			zap.String("outcome", "lookup_failed_reported_active"))
		return SubscriptionActive
	}
	if ent.Stale || ent.Status == entitlement.StatusUnknown {
		return SubscriptionActive
	}
	if !ent.Active() {
		return SubscriptionExpired
	}
	return SubscriptionActive
}
