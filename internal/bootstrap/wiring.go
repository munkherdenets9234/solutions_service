package bootstrap

import (
	"errors"
	"time"

	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/eandstravel/digitalservice/internal/entitlement"
	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/internal/notify"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/internal/tenantresolve"
	"github.com/eandstravel/digitalservice/pkg/token"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// repos and services are unexported bundles that exist to keep New readable.
// They are not a service locator: nothing outside this package can reach into
// them, and each audience package still receives only the services its own
// Register signature names.

type repos struct {
	destination     *repository.DestinationRepo
	booking         *repository.BookingRepo
	blog            *repository.BlogRepo
	customer        *repository.CustomerRepo
	car             *repository.CarRepo
	rental          *repository.RentalRepo
	airportTransfer *repository.AirportTransferRepo
	contactMessage  *repository.ContactMessageRepo
	newsletter      *repository.NewsletterRepo
	review          *repository.ReviewRepo
	partner         *repository.PartnerRepo
	pkg             *repository.PackageRepo
	quote           *repository.QuoteRepo
	tenant          *repository.TenantRepo
	tenantDetail    *repository.TenantDetailRepo
	tenantReview    *repository.TenantReviewRepo
	tenantPackage   *repository.TenantPackageRepo
	tenantUser      *repository.TenantUserRepo
	passwordReset   *repository.TenantPasswordResetRepo
	sitePage        *repository.SitePageRepo
	platformUser    *repository.PlatformUserRepo
}

func newRepos(db *mongo.Database) repos {
	return repos{
		destination:     repository.NewDestinationRepo(db),
		booking:         repository.NewBookingRepo(db),
		blog:            repository.NewBlogRepo(db),
		customer:        repository.NewCustomerRepo(db),
		car:             repository.NewCarRepo(db),
		rental:          repository.NewRentalRepo(db),
		airportTransfer: repository.NewAirportTransferRepo(db),
		contactMessage:  repository.NewContactMessageRepo(db),
		newsletter:      repository.NewNewsletterRepo(db),
		review:          repository.NewReviewRepo(db),
		partner:         repository.NewPartnerRepo(db),
		pkg:             repository.NewPackageRepo(db),
		quote:           repository.NewQuoteRepo(db),
		tenant:          repository.NewTenantRepo(db),
		tenantDetail:    repository.NewTenantDetailRepo(db),
		tenantReview:    repository.NewTenantReviewRepo(db),
		tenantPackage:   repository.NewTenantPackageRepo(db),
		tenantUser:      repository.NewTenantUserRepo(db),
		passwordReset:   repository.NewTenantPasswordResetRepo(db),
		sitePage:        repository.NewSitePageRepo(db),
		platformUser:    repository.NewPlatformUserRepo(db),
	}
}

type services struct {
	destination     *service.DestinationService
	booking         *service.BookingService
	blog            *service.BlogService
	car             *service.CarService
	rental          *service.RentalService
	airportTransfer *service.AirportTransferService
	contactMessage  *service.ContactMessageService
	newsletter      *service.NewsletterService
	customer        *service.CustomerService
	review          *service.ReviewService
	partner         *service.PartnerService
	pkg             *service.PackageService
	quote           *service.QuoteService
	tenant          *service.TenantService
	tenantReview    *service.TenantReviewService
	tenantPackage   *service.TenantPackageService
	tenantUser      *service.TenantUserService
	passwordReset   *service.TenantPasswordResetService
	sitePage        *service.SitePageService
	platformUser    *service.PlatformUserService

	// entitlement is the seam the product split runs through. It is an
	// interface, not a concrete type: the day this went from a local query
	// to an HTTP call against tenantcore, this was the only line that
	// changed.
	entitlement entitlement.Provider

	// entitlementClient is the same object as entitlement when the platform
	// link is configured, kept separately because two things need more than
	// the Provider interface: Close on shutdown, and Degraded for /readyz.
	// It is nil when the link is off.
	entitlementClient *entitlement.Client

	// tenantResolver is what the tenant gate asks to turn an X-API-Key into a
	// tenant: this service's own collection by default, tenantcore when
	// TENANT_RESOLVER=tenantcore.
	tenantResolver middleware.TenantResolver
	// tenantResolveClient is the tenantcore client behind tenantResolver, nil
	// in local mode. Kept for Close on shutdown and Degraded for /readyz.
	tenantResolveClient *tenantresolve.Client

	// upload is nil when CLOUDINARY_URL is unset. See buildUpload.
	upload *service.UploadService
}

func newServices(r repos, tokenMaker *token.Maker, cfg *config.Config, log *zap.Logger) (services, error) {
	entClient := buildEntitlement(cfg, log)

	// A nil *entitlement.Client stored in a Provider interface is NOT nil as
	// an interface value, so the substitution has to happen on the concrete
	// type before it is assigned. Getting this wrong gives every gate a
	// non-nil Provider whose every call errors — a 500 on every write, which
	// looks like an outage rather than a missing setting.
	var entProvider entitlement.Provider = entitlement.Unenforced{}
	if entClient.Available() {
		entProvider = entClient
	}

	tenantSvc := service.NewTenantService(r.tenant, r.tenantDetail, r.platformUser)
	tenantResolver, tenantResolveClient, err := buildTenantResolver(cfg, tenantSvc, log)
	if err != nil {
		return services{}, err
	}

	return services{
		destination:     service.NewDestinationService(r.destination, r.tenantUser),
		booking:         service.NewBookingService(r.booking, r.customer, r.destination, r.tenantUser),
		blog:            service.NewBlogService(r.blog, r.tenantUser),
		car:             service.NewCarService(r.car, r.tenantUser),
		rental:          service.NewRentalService(r.rental, r.customer, r.car, r.tenantUser),
		airportTransfer: service.NewAirportTransferService(r.airportTransfer, r.customer, r.tenantUser),
		contactMessage:  service.NewContactMessageService(r.contactMessage, r.tenantUser),
		newsletter:      service.NewNewsletterService(r.newsletter),
		customer:        service.NewCustomerService(r.customer, r.booking, r.rental, r.airportTransfer, r.tenantUser),
		review:          service.NewReviewService(r.review, r.tenantUser),
		partner:         service.NewPartnerService(r.partner, r.tenantUser),
		pkg:             service.NewPackageService(r.pkg, r.tenantPackage, r.platformUser),
		quote:           service.NewQuoteService(r.quote, r.tenantUser, r.platformUser),
		tenant:          tenantSvc,
		tenantReview:    service.NewTenantReviewService(r.tenantReview, r.tenant),
		tenantPackage:   service.NewTenantPackageService(r.tenantPackage, r.tenant, r.pkg),
		tenantUser:      service.NewTenantUserService(r.tenantUser, tokenMaker, cfg.TokenExpiry),
		// Mail goes through tenantcore, over the same link entitlement uses, so
		// this adds no configuration. A nil client means the link is off; Request
		// then answers 503 rather than pretending to send.
		passwordReset: service.NewTenantPasswordResetService(
			r.tenantUser, r.passwordReset, r.tenant,
			notify.NewClient(notify.Config{BaseURL: cfg.TenantcoreURL, ServiceKey: cfg.TenantcoreServiceKey}),
			log,
		),
		sitePage:     service.NewSitePageService(r.sitePage),
		platformUser: service.NewPlatformUserService(r.platformUser, tokenMaker, cfg.TokenExpiry),

		entitlement:       entProvider,
		entitlementClient: entClient,

		tenantResolver:      tenantResolver,
		tenantResolveClient: tenantResolveClient,

		upload: buildUpload(cfg, log),
	}, nil
}

// buildTenantResolver picks how X-API-Key becomes a tenant.
//
// Anything other than an explicit "tenantcore" is the local resolver: today's
// behaviour, no client, nothing to close.
//
// Tenantcore mode FAILS CLOSED. If the client cannot be built the deployment
// does not start, and in particular it never falls back to the local
// collection: that holds stale hashes after a key is re-issued or revoked, so
// a fallback would keep accepting keys tenantcore has retired. Validate
// refuses this configuration first; this is the backstop for callers that
// reach wiring without it.
func buildTenantResolver(cfg *config.Config, local *service.TenantService, log *zap.Logger) (middleware.TenantResolver, *tenantresolve.Client, error) {
	if !cfg.TenantResolverTenantcoreEnabled() {
		return middleware.NewLocalResolver(local), nil, nil
	}
	c := tenantresolve.NewClient(tenantresolve.ClientConfig{
		BaseURL:    cfg.TenantcoreURL,
		ServiceKey: cfg.TenantcoreServiceKey,
		Log:        log,
	})
	if c == nil {
		return nil, nil, errors.New("TENANT_RESOLVER=tenantcore needs TENANTCORE_URL and TENANTCORE_SERVICE_KEY; " +
			"refusing to start rather than fall back to the local tenants collection")
	}
	log.Info("tenant resolution ready — API keys are resolved through tenantcore",
		zap.String("platform", cfg.TenantcoreURL))
	return middleware.NewTenantcoreResolver(c), c, nil
}

// buildEntitlement returns the tenantcore client, or nil when the link is not
// configured.
//
// Same rule as every other optional dependency here: the process starts, says
// what is missing, and keeps serving. What is different is the consequence —
// an unset CLOUDINARY_URL costs you one route, an unset TENANTCORE_URL costs
// you subscription enforcement across the whole service. Hence the ERROR
// rather than a note, and the explicit sentence about what is now unenforced:
// the operator should be able to read the startup log and know exactly what
// this deployment is not checking.
func buildEntitlement(cfg *config.Config, log *zap.Logger) *entitlement.Client {
	if !cfg.EntitlementEnabled() {
		log.Error("entitlement is UNENFORCED — TENANTCORE_URL/TENANTCORE_SERVICE_KEY are not both set; " +
			"subscription and module gates will let every tenant through")
		return nil
	}
	c := entitlement.NewClient(entitlement.ClientConfig{
		BaseURL:     cfg.TenantcoreURL,
		ServiceKey:  cfg.TenantcoreServiceKey,
		TTL:         time.Duration(cfg.EntitlementTTLSeconds) * time.Second,
		GraceWindow: time.Duration(cfg.EntitlementGraceSeconds) * time.Second,
		Timeout:     time.Duration(cfg.EntitlementTimeoutMS) * time.Millisecond,
		Log:         log,
	})
	log.Info("entitlement ready — subscriptions come from tenantcore",
		zap.String("platform", cfg.TenantcoreURL),
		zap.Int("ttl_seconds", cfg.EntitlementTTLSeconds),
		zap.Int("grace_seconds", cfg.EntitlementGraceSeconds))
	return c
}

// buildUpload returns nil when uploads are not configured, and nil when the
// configured URL is unusable.
//
// Both cases are survivable and neither is silent: config.Features already
// reports the unset case, and a malformed URL is logged at ERROR here. What
// they must not do is stop the process. Every other route on this service
// works perfectly well without an image host, and a deployment that refuses
// to serve its storefronts because one credential is wrong has turned a small
// problem into a total outage.
func buildUpload(cfg *config.Config, log *zap.Logger) *service.UploadService {
	if !cfg.UploadsEnabled() {
		return nil
	}
	svc, err := service.NewUploadService(cfg.CloudinaryURL, cfg.UploadMaxBytes)
	if err != nil {
		log.Error("image uploads unavailable — CLOUDINARY_URL is set but could not be parsed; "+
			"POST /admin/uploads will answer 503 FEATURE_UNAVAILABLE",
			zap.Error(err))
		return nil
	}
	log.Info("image uploads ready", zap.Int64("max_bytes", cfg.UploadMaxBytes))
	return svc
}
