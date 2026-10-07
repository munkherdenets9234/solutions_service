// Package api builds the HTTP surface.
//
// The audience split lives one level down, in platform/ and tenant/. This
// package only does the wiring: engine-wide middleware, the operational
// endpoints, and mounting each audience on the group that carries its
// authentication.
package api

import (
	"github.com/eandstravel/digitalservice/internal/api/tenant"
	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/eandstravel/digitalservice/internal/entitlement"
	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/internal/tenantresolve"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Deps is everything the HTTP layer needs. It is deliberately a flat struct
// of services rather than a container the handlers can reach into: a
// controller gets the services its own Register passes it and nothing else.
type Deps struct {
	Config *config.Config
	Log    *zap.Logger

	Auth           *middleware.AuthMiddleware
	TenantcoreAuth *middleware.TenantcoreAuth
	TenantMW       *middleware.TenantMiddleware
	SubscriptionMW *middleware.SubscriptionMiddleware
	RateLimiter    *middleware.RateLimiter

	// Entitlement answers what a tenant's plan allows. It is backed by an
	// HTTP client against tenantcore, which owns subscriptions and plans —
	// this service holds no copy of either.
	Entitlement entitlement.Provider

	// EntitlementClient is the same object when the platform link is
	// configured, and nil otherwise. Only /readyz uses it, to report whether
	// the link is currently degraded; everything else goes through the
	// Provider interface above.
	EntitlementClient *entitlement.Client

	// TenantResolveClient is the tenantcore resolver's client when
	// TENANT_RESOLVER=tenantcore, nil otherwise. Only /readyz uses it.
	TenantResolveClient *tenantresolve.Client

	// Modules are the additional products this deployment serves, each
	// mounted at /api/v1/<name> behind its own entitlement gate. Empty today:
	// the brochure surface predates modules. See tenant.Module.
	Modules []tenant.Module

	Destination     *service.DestinationService
	Blog            *service.BlogService
	Car             *service.CarService
	Review          *service.ReviewService
	Partner         *service.PartnerService
	Package         *service.PackageService
	Booking         *service.BookingService
	Rental          *service.RentalService
	AirportTransfer *service.AirportTransferService
	ContactMessage  *service.ContactMessageService
	Newsletter      *service.NewsletterService
	Quote           *service.QuoteService
	Customer        *service.CustomerService
	Tenants         *service.TenantService
	TenantReview    *service.TenantReviewService
	TenantPackage   *service.TenantPackageService
	TenantUser      *service.TenantUserService
	PasswordReset   *service.TenantPasswordResetService
	SitePage        *service.SitePageService
	SubscriptionStatus *service.SubscriptionStatusService
	PlatformUser    *service.PlatformUserService

	// Upload is nil-safe: a nil service means uploads were not configured,
	// and the route says so rather than disappearing.
	Upload *service.UploadService

	// GuideApplication is the guide recruitment service. It is always built;
	// with private file storage off its Submit answers 503 on its own.
	GuideApplication    *service.GuideApplicationService
	GuideUploadMaxBytes int64
}

// Server is the built HTTP engine.
type Server struct {
	engine *gin.Engine
	deps   Deps
}

// NewServer wires the router.
func NewServer(d Deps) *Server {
	s := &Server{deps: d}
	s.engine = s.buildEngine()
	return s
}

// Handler exposes the engine, for http.Server and for tests.
func (s *Server) Handler() *gin.Engine { return s.engine }
