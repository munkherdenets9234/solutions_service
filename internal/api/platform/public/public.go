// Package public holds the platform surface that needs no credentials at all.
//
// Everything here is reachable by anyone on the internet: the platform
// operator's own marketing site reads it, and a prospect with no tenant
// relationship submits to it. Nothing in this package may read the caller's
// identity, because there isn't one.
//
// Its counterpart, platform/private, is mounted behind a superadmin token by
// platform.Register. The split is the point: a controller in that package
// cannot be reached without a token, whatever a future router edit does,
// because the group it is registered on carries the middleware.
package public

import (
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/httpx"
	"github.com/gin-gonic/gin"
)

// Deps bundles everything the public platform controllers need.
type Deps struct {
	Tenant        *service.TenantService
	TenantReview  *service.TenantReviewService
	TenantPackage *service.TenantPackageService
	Package       *service.PackageService
	Quote         *service.QuoteService
	PlatformUser  *service.PlatformUserService

	// AuthRateLimit guards the login endpoint; LeadRateLimit guards the
	// anonymous quote intake. Both are gin middleware supplied by the router
	// so the limiter's buckets are shared process-wide rather than per group.
	AuthRateLimit gin.HandlerFunc
	LeadRateLimit gin.HandlerFunc
}

// Register mounts the credential-free platform routes onto base.
func Register(base *gin.RouterGroup, d Deps) {
	admins := &adminsController{svc: d.PlatformUser}
	tenants := &tenantsController{svc: d.Tenant}
	catalog := &catalogController{
		pkg:           d.Package,
		tenantPackage: d.TenantPackage,
		tenantReview:  d.TenantReview,
		quote:         d.Quote,
	}

	g := httpx.Wrap(base)

	// Login is rate limited because it answers differently for a known and an
	// unknown email, which makes it an account-existence oracle as well as a
	// password guessing target.
	g.Group("", d.AuthRateLimit).POST("/login", admins.Login)

	g.GET("/admins", admins.List)

	g.GET("/tenants", tenants.List)
	g.GET("/tenants/:id", tenants.GetByID)

	// Our Projects — public case-study cards/detail, joining active tenants
	// against their showcase-enabled TenantDetail record. Separate from
	// /tenants above, which stays the platform's own full-detail tenant
	// management read.
	g.GET("/projects", tenants.ListProjects)
	g.GET("/projects/:slug", tenants.GetProjectBySlug)

	// GET /tenants/:id/subscription used to live here, unauthenticated, and
	// returned a named tenant's billing state to anyone who asked. It is gone
	// rather than moved: tenantcore owns subscriptions now and the console
	// reads them from there, so re-exposing them here would recreate both the
	// leak and the second copy.
	g.GET("/tenants/:id/packages", catalog.ListTenantPackages)

	// Tenant reviews — testimonials from the platform's own onboarded tenants
	// (clients), for the platform operator's own Review page. Distinct from
	// the tenant-scoped /reviews (a tenant's own customers reviewing that
	// tenant's tours/partners).
	g.GET("/reviews", catalog.ListReviews)

	// Packages — the platform's own global price-list catalog, managed
	// centrally rather than per-tenant. Which tenants show a given package on
	// their own storefront is a separate assignment (see
	// /platform/tenants/{id}/packages and models.TenantPackage). Distinct
	// from the tenant-scoped /packages (a tenant's own, assignment-filtered
	// storefront read).
	g.GET("/packages", catalog.ListPackages)
	g.GET("/packages/:id", catalog.GetPackage)

	// Quotes — a quote is a lead for a *potential* tenant; not every one has
	// an existing tenant relationship. This intake is fully public for
	// exactly that reason: a prospect inquiring here has no tenant to
	// authenticate as. Distinct from the tenant-scoped /quotes (submitted
	// through an existing tenant's own storefront, which does set tenant_id
	// from its X-API-Key).
	g.Group("", d.LeadRateLimit).POST("/quotes", catalog.CreateQuote)
	g.GET("/quotes", catalog.ListQuotes)
}
