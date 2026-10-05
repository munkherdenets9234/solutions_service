// Package platform is the audience that manages tenants: the platform
// operator's own staff, and the public marketing site that reads their
// catalog.
//
// It mounts two sub-packages. public is reachable by anyone; private is
// mounted on a group carrying the superadmin check, so a controller there
// cannot be reached without a token no matter how its own file is edited.
// The two never share a controller type, which is what stops an internal
// field from leaking into a public response by way of a widened struct.
package platform

import (
	privateapi "github.com/eandstravel/digitalservice/internal/api/platform/private"
	publicapi "github.com/eandstravel/digitalservice/internal/api/platform/public"
	tenantcoreapi "github.com/eandstravel/digitalservice/internal/api/platform/tenantcore"
	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/gin-gonic/gin"
)

// Deps bundles everything the platform audience needs.
type Deps struct {
	Auth *middleware.AuthMiddleware

	// Tenantcore guards the one group reached with a tenantcore-signed token.
	// A nil value (or one with no verifier) switches that group off: 404.
	Tenantcore *middleware.TenantcoreAuth
	Reset      *service.TenantPasswordResetService

	Tenant        *service.TenantService
	TenantUser    *service.TenantUserService
	TenantReview  *service.TenantReviewService
	TenantPackage *service.TenantPackageService
	Package       *service.PackageService
	Quote         *service.QuoteService
	PlatformUser  *service.PlatformUserService

	AuthRateLimit gin.HandlerFunc
	LeadRateLimit gin.HandlerFunc
}

// Register mounts the platform audience onto base, which the caller has
// already prefixed with /platform.
func Register(base *gin.RouterGroup, d Deps) {
	publicapi.Register(base, publicapi.Deps{
		Tenant:        d.Tenant,
		TenantReview:  d.TenantReview,
		TenantPackage: d.TenantPackage,
		Package:       d.Package,
		Quote:         d.Quote,
		PlatformUser:  d.PlatformUser,
		AuthRateLimit: d.AuthRateLimit,
		LeadRateLimit: d.LeadRateLimit,
	})

	// A separate group for the paths tenantcore's operators reach, guarded by
	// their own token (Register applies the guard). Kept off priv: that group
	// carries this service's HMAC check, which these callers cannot pass.
	tenantcoreapi.Register(base, tenantcoreapi.Deps{
		Auth:      d.Tenantcore,
		Users:     d.TenantUser,
		Reset:     d.Reset,
		RateLimit: d.AuthRateLimit,
	})

	// The gate for everything below. It is applied here, once, on the group
	// itself — not inside the private package, which must not be able to
	// choose its own authentication.
	priv := base.Group("", d.Auth.Require("superadmin"))

	privateapi.Register(priv, privateapi.Deps{
		Tenant:        d.Tenant,
		TenantUser:    d.TenantUser,
		TenantReview:  d.TenantReview,
		TenantPackage: d.TenantPackage,
		Package:       d.Package,
		Quote:         d.Quote,
		PlatformUser:  d.PlatformUser,
	})
}
