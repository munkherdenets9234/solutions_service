package middleware

import (
	"github.com/eandstravel/digitalservice/internal/entitlement"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ctxEntitlement holds the entitlement fetched by RequireModule, so a handler
// or service on the same request can read limits without a second lookup.
const ctxEntitlement = "entitlement"

// RequireModule gates a whole product behind the tenant's plan.
//
// Mount it on the group a product is registered under, the way
// Auth.Require is mounted on the private groups: every route inside is then
// entitlement-checked by construction, and a route added next month is
// covered without anyone remembering. This is the one place the "one
// deployment per product" split shows up in this service's routing — the car
// wash routes hang off a group carrying RequireModule("carwash"), and until
// a tenant's plan includes it they cannot reach any of them.
//
// Must run after TenantMiddleware.Require, which resolves the tenant.
//
// It does NOT replace SubscriptionMiddleware. That one asks "is this tenant
// paying" and deliberately lets reads through so a lapsed tenant's storefront
// stays up; this one asks "did they buy this product at all", and the answer
// applies to reads too — an unsold product should not serve its data.
func RequireModule(p entitlement.Provider, module string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, ok := tenantFromContext(c)
		if !ok {
			fail(c, apierr.Internal(errMissingTenant("RequireModule")))
			return
		}

		ent, err := p.For(c.Request.Context(), tenantID)
		if err != nil {
			// We could not find out. Deciding "no" here would make the
			// entitlement source a single point of failure for every
			// product at once — worse than the monolith this replaces — so
			// the error travels up as a 500 and is logged with its cause,
			// rather than being quietly converted into a denial the tenant
			// cannot argue with. Once the HTTP client lands, its cache
			// answers from last-known state and this branch becomes the rare
			// "never fetched this tenant" case.
			fail(c, err)
			return
		}

		if !ent.HasModule(module) {
			fail(c, apierr.ModuleNotEntitled(module))
			return
		}

		c.Set(ctxEntitlement, ent)
		c.Next()
	}
}

// RequireFeature gates a single capability within a product the tenant
// already has — the business-level axis rather than the which-product one.
//
// Use it for a route that exists only on higher tiers. For a ceiling on how
// many of something a tenant may create, this is the wrong tool: see
// Entitlement.Within, which is checked in the service layer because it needs
// a count of existing rows and middleware has none.
func RequireFeature(p entitlement.Provider, feature string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, ok := tenantFromContext(c)
		if !ok {
			fail(c, apierr.Internal(errMissingTenant("RequireFeature")))
			return
		}

		ent, err := p.For(c.Request.Context(), tenantID)
		if err != nil {
			fail(c, err)
			return
		}

		if !ent.Feature(feature) {
			fail(c, apierr.ModuleNotEntitled(feature))
			return
		}

		c.Set(ctxEntitlement, ent)
		c.Next()
	}
}

// EntitlementFrom returns the entitlement a gate already fetched for this
// request, so a service-layer limit check costs no second lookup. ok is false
// on a route with no gate mounted — callers that need a limit there must ask
// the provider themselves rather than assume an empty entitlement, which
// would read as "unlimited".
func EntitlementFrom(c *gin.Context) (entitlement.Entitlement, bool) {
	v, exists := c.Get(ctxEntitlement)
	if !exists {
		return entitlement.Entitlement{}, false
	}
	ent, ok := v.(entitlement.Entitlement)
	return ent, ok
}

func tenantFromContext(c *gin.Context) (primitive.ObjectID, bool) {
	v, exists := c.Get(CtxTenantID)
	if !exists {
		return primitive.NilObjectID, false
	}
	id, ok := v.(primitive.ObjectID)
	return id, ok
}

// errMissingTenant names the wiring mistake rather than reporting a bare
// 500: mounting an entitlement gate without TenantMiddleware ahead of it is a
// routing bug, and the log line should say which gate and what is missing.
type errMissingTenant string

func (e errMissingTenant) Error() string {
	return string(e) + " mounted without tenant middleware ahead of it"
}
