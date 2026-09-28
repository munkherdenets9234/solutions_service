package middleware

import (
	"errors"

	"net/http"

	"github.com/eandstravel/digitalservice/internal/entitlement"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/gin-gonic/gin"
)

// SubscriptionMiddleware is the "is this tenant paying" gate.
//
// It reads through entitlement.Provider, which means the answer now comes
// from tenantcore rather than from this service's own subscriptions
// collection. That is the entire point of the change: this gate used to call
// SubscriptionService directly, reaching AROUND the seam that was built for
// the split, so swapping the provider would have left the gate reading a
// local copy that drifts the moment the platform console writes.
//
// It is not the same question as RequireModule. This one asks "is this tenant
// paying", and deliberately lets reads through so a lapsed tenant's
// storefront stays up; that one asks "did they buy this product at all", and
// applies to reads too.
type SubscriptionMiddleware struct {
	provider entitlement.Provider
}

func NewSubscriptionMiddleware(p entitlement.Provider) *SubscriptionMiddleware {
	return &SubscriptionMiddleware{provider: p}
}

// Require blocks mutating requests (POST/PUT/PATCH/DELETE) for tenants whose
// subscription is not in a usable state. GET/HEAD/OPTIONS always pass, so a
// tenant who has lapsed can still see their own data and the storefront their
// customers visit stays up.
//
// Must run after TenantMiddleware.Require(), which sets CtxTenantID — and
// which has already refused a SUSPENDED tenant with a 403, so suspension
// never reaches this gate.
//
// Three outcomes, and the difference between the last two is the whole
// contract:
//
//   - StatusUnknown — no subscription record at all. Passes. Provisioning
//     one is a separate, explicit act by a platform superadmin, and a tenant
//     created five minutes ago should not have their storefront start
//     refusing writes in the gap. This is the behaviour this service has
//     always had; it is preserved here deliberately.
//   - Not active — lapsed, past due, canceled. 402.
//   - Lookup FAILED — we could not find out. Never 402. The error travels up
//     as a 500 with its cause logged, because telling a paying customer they
//     had not paid on the strength of an unreachable platform is the worst
//     available answer. The client's cache means this branch is only reached
//     when tenantcore has been down long enough to exhaust the grace window,
//     or has never been asked about this tenant at all.
func (s *SubscriptionMiddleware) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			c.Next()
			return
		}

		tenantID, ok := tenantFromContext(c)
		if !ok {
			// Mounting this group without TenantMiddleware ahead of it is a
			// wiring bug, not a client error. Name it in the log rather than
			// reporting a bare 500 with no cause.
			fail(c, apierr.Internal(errors.New("subscription middleware mounted without tenant middleware")))
			return
		}

		ent, err := s.provider.For(c.Request.Context(), tenantID)
		if err != nil {
			fail(c, err)
			return
		}

		if ent.Status == entitlement.StatusUnknown {
			c.Next()
			return
		}

		if !ent.Active() {
			fail(c, apierr.SubscriptionRequired(""))
			return
		}

		c.Next()
	}
}
