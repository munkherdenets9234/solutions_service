package api

import (
	"net/http"
	"time"

	"github.com/eandstravel/digitalservice/internal/api/docs"
	"github.com/eandstravel/digitalservice/internal/api/platform"
	"github.com/eandstravel/digitalservice/internal/api/tenant"
	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/gin-gonic/gin"
)

func (s *Server) buildEngine() *gin.Engine {
	d := s.deps
	devMode := d.Config.IsDev()

	gin.SetMode(gin.ReleaseMode)
	if devMode {
		gin.SetMode(gin.DebugMode)
	}

	e := gin.New()

	// Order matters. Recovery is outermost so a panic in any later
	// middleware is still rendered as a proper envelope; ErrorHandler wraps
	// everything below it so it sees errors from middleware as well as from
	// controllers.
	e.Use(middleware.Recovery(d.Log, devMode))
	e.Use(middleware.Logger(d.Log))
	e.Use(middleware.CORS())
	e.Use(middleware.ErrorHandler(d.Log, devMode))

	s.registerOperational(e)

	api := e.Group("/api/v1")

	platform.Register(api.Group("/platform"), platform.Deps{
		Auth:          d.Auth,
		Tenantcore:    d.TenantcoreAuth,
		Reset:         d.PasswordReset,
		Tenant:        d.Tenants,
		TenantUser:    d.TenantUser,
		TenantReview:  d.TenantReview,
		TenantPackage: d.TenantPackage,
		Package:       d.Package,
		Quote:         d.Quote,
		PlatformUser:  d.PlatformUser,
		AuthRateLimit: s.limit("platform-auth", d.Config.AuthRatePerMinute),
		LeadRateLimit: s.limit("platform-lead", d.Config.LeadRatePerMinute),
	})

	tenant.Register(api, tenant.Deps{
		Auth:         d.Auth,
		Tenant:       d.TenantMW,
		Subscription: d.SubscriptionMW,
		Entitlement:  d.Entitlement,
		Modules:      d.Modules,
		// Before the tenant gate (see tenant.Register): keyed by client IP and
		// generous, because storefront servers share few IPs.
		ResolveRateLimit: s.limit("tenant-resolve", d.Config.TenantResolveRatePerMinute),
		AuthRateLimit:    s.limit("tenant-auth", d.Config.AuthRatePerMinute),
		LeadRateLimit:    s.limit("tenant-lead", d.Config.LeadRatePerMinute),
		Destination:      d.Destination,
		Blog:             d.Blog,
		Car:              d.Car,
		Review:           d.Review,
		Partner:          d.Partner,
		Package:          d.Package,
		Booking:          d.Booking,
		Rental:           d.Rental,
		AirportTransfer:  d.AirportTransfer,
		ContactMessage:   d.ContactMessage,
		Newsletter:       d.Newsletter,
		Quote:            d.Quote,
		Customer:         d.Customer,
		TenantUser:       d.TenantUser,
		PasswordReset:    d.PasswordReset,
		SitePage:         d.SitePage,
		Upload:           d.Upload,
	})

	return e
}

// limit returns the named rate-limit middleware, or a pass-through when rate
// limiting is switched off. Returning a no-op rather than skipping the
// middleware keeps the route tree identical in both cases, so a limit that is
// off cannot also change which middleware a route runs.
func (s *Server) limit(name string, perMinute int) gin.HandlerFunc {
	if !s.deps.Config.RateLimitEnabled || s.deps.RateLimiter == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return s.deps.RateLimiter.Limit(name, perMinute, s.deps.Config.RateLimitBurst)
}

// registerOperational mounts the endpoints that describe the service rather
// than serve its data. None are versioned or tenant-scoped.
func (s *Server) registerOperational(e *gin.Engine) {
	// healthz answers "is the process up". Keep it cheap and dependency-free:
	// a health check that talks to the database turns a slow query into a
	// restart loop.
	e.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// readyz answers "what is this deployment actually able to do".
	//
	// This is the counterweight to letting optional dependencies degrade
	// instead of crashing. Degrading quietly is the right behaviour and a
	// liability on its own: a feature can sit unmounted for a week with
	// nothing to say so. The same list is written to the log at startup, but
	// a log line scrolls away — this endpoint keeps answering for as long as
	// the process runs, so a monitor can alert on it and a human can check it
	// without shell access.
	//
	// It reports 200 with degraded:true rather than a failure status: a
	// deployment missing an optional feature is degraded, not unhealthy, and
	// returning 503 here would make an orchestrator restart a process that is
	// working exactly as configured.
	e.GET("/readyz", func(c *gin.Context) {
		features := s.deps.Config.Features()
		degraded := false
		list := make([]gin.H, 0, len(features))
		for _, f := range features {
			if !f.Enabled {
				degraded = true
			}
			entry := gin.H{"name": f.Name, "enabled": f.Enabled}
			if !f.Enabled {
				entry["detail"] = f.Detail
			}
			list = append(list, entry)
		}
		body := gin.H{
			"status":   "ok",
			"env":      string(s.deps.Config.AppEnv),
			"degraded": degraded,
			"features": list,
		}

		// The entitlement link has a second, live state that config cannot
		// know: configured, but currently answering from cache because
		// tenantcore is unreachable. That is the loud half of serving stale
		// entitlements — without it a platform that has been down for a day
		// looks exactly like one that is fine, right up until a cache entry
		// ages out and a tenant is refused for no visible reason.
		if stale, since := s.deps.EntitlementClient.Degraded(); stale {
			body["degraded"] = true
			entry := gin.H{"stale": true, "detail": "tenantcore is unreachable — serving cached entitlements"}
			if since != nil {
				entry["since"] = since.UTC().Format(time.RFC3339)
			}
			body["entitlement"] = entry
		}

		// Same for tenant resolution through tenantcore: while it is down,
		// known keys are served from cache and unseen ones answer 503.
		if stale, since := s.deps.TenantResolveClient.Degraded(); stale {
			body["degraded"] = true
			entry := gin.H{"stale": true, "detail": "tenantcore is unreachable — resolving tenant API keys from cache; unseen keys answer 503"}
			if since != nil {
				entry["since"] = since.UTC().Format(time.RFC3339)
			}
			body["tenant_resolver"] = entry
		}

		c.JSON(http.StatusOK, body)
	})

	// Swagger UI API reference. Not tenant-scoped — no X-API-Key required,
	// it just describes the routes below.
	e.GET("/docs", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", docs.HTML)
	})
	e.GET("/docs/openapi.json", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json; charset=utf-8", docs.OpenAPISpec)
	})
}
