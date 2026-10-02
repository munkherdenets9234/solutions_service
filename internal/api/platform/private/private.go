// Package private holds the platform surface that only a superadmin may
// reach.
//
// Every controller here is registered on a group that already carries the
// superadmin check (see platform.Register). That is the whole reason this is
// a separate package from platform/public: the guarantee is structural, not a
// convention each handler has to remember. Adding a route to this package
// cannot produce an unauthenticated endpoint, and no edit inside a controller
// can weaken it.
package private

import (
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/httpx"
	"github.com/gin-gonic/gin"
)

// Deps bundles everything the private platform controllers need.
type Deps struct {
	Tenant        *service.TenantService
	TenantUser    *service.TenantUserService
	TenantReview  *service.TenantReviewService
	TenantPackage *service.TenantPackageService
	Package       *service.PackageService
	Quote         *service.QuoteService
	PlatformUser  *service.PlatformUserService
}

// Register mounts the superadmin-only platform routes onto base, which the
// caller has already gated.
func Register(base *gin.RouterGroup, d Deps) {
	tenants := &tenantsController{svc: d.Tenant, userSvc: d.TenantUser, quote: d.Quote}
	catalog := &catalogController{pkg: d.Package, tenantPackage: d.TenantPackage, review: d.TenantReview, quote: d.Quote}
	admins := &adminsController{svc: d.PlatformUser}

	g := httpx.Wrap(base)

	t := g.Group("/tenants")
	t.POST("", tenants.Create)
	t.PUT("/:id/status", tenants.UpdateStatus)
	t.POST("/:id/rotate-key", tenants.RotateAPIKey)
	t.PUT("/:id/domain", tenants.UpdateDomain)
	t.PUT("/:id/project", tenants.UpdateProject)
	t.GET("/:id/quotes", tenants.ListQuotes)
	// Subscriptions are tenantcore's: it owns the record, the console writes
	// there, and this service only ever asks what a tenant is entitled to.
	// Mounting a second write path here is how the two copies drifted.
	t.POST("/:id/packages", catalog.AssignPackage)
	t.DELETE("/:id/packages/:package_id", catalog.UnassignPackage)

	r := g.Group("/reviews")
	r.POST("", catalog.CreateReview)
	r.PUT("/:id", catalog.UpdateReview)
	r.DELETE("/:id", catalog.DeleteReview)

	p := g.Group("/packages")
	p.POST("", catalog.CreatePackage)
	p.PUT("/:id", catalog.UpdatePackage)
	p.DELETE("/:id", catalog.DeletePackage)

	g.Group("/quotes").PUT("/:id/status", catalog.UpdateQuoteStatus)

	a := g.Group("/admins")
	a.POST("", admins.Create)
	a.PUT("/:id/status", admins.UpdateStatus)
	a.PUT("/:id/password", admins.ResetPassword)

	g.Group("/account").PUT("/password", admins.ChangePassword)
}
