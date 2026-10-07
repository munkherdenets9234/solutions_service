package bootstrap

import (
	"errors"
	"time"

	unsubscribeapi "github.com/eandstravel/digitalservice/internal/api/unsubscribe"
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
	destination      *repository.DestinationRepo
	booking          *repository.BookingRepo
	blog             *repository.BlogRepo
	customer         *repository.CustomerRepo
	car              *repository.CarRepo
	rental           *repository.RentalRepo
	airportTransfer  *repository.AirportTransferRepo
	contactMessage   *repository.ContactMessageRepo
	guideApplication *repository.GuideApplicationRepo
	newsletter       *repository.NewsletterRepo
	review           *repository.ReviewRepo
	partner          *repository.PartnerRepo
	pkg              *repository.PackageRepo
	quote            *repository.QuoteRepo
	tenant           *repository.TenantRepo
	tenantDetail     *repository.TenantDetailRepo
	tenantReview     *repository.TenantReviewRepo
	tenantPackage    *repository.TenantPackageRepo
	tenantUser       *repository.TenantUserRepo
	passwordReset    *repository.TenantPasswordResetRepo
	sitePage         *repository.SitePageRepo
	platformUser     *repository.PlatformUserRepo
	mailOutbox       *repository.MailOutboxRepo
}

func newRepos(db *mongo.Database) repos {
	return repos{
		destination:      repository.NewDestinationRepo(db),
		booking:          repository.NewBookingRepo(db),
		blog:             repository.NewBlogRepo(db),
		customer:         repository.NewCustomerRepo(db),
		car:              repository.NewCarRepo(db),
		rental:           repository.NewRentalRepo(db),
		airportTransfer:  repository.NewAirportTransferRepo(db),
		contactMessage:   repository.NewContactMessageRepo(db),
		guideApplication: repository.NewGuideApplicationRepo(db),
		newsletter:       repository.NewNewsletterRepo(db),
		review:           repository.NewReviewRepo(db),
		partner:          repository.NewPartnerRepo(db),
		pkg:              repository.NewPackageRepo(db),
		quote:            repository.NewQuoteRepo(db),
		tenant:           repository.NewTenantRepo(db),
		tenantDetail:     repository.NewTenantDetailRepo(db),
		tenantReview:     repository.NewTenantReviewRepo(db),
		tenantPackage:    repository.NewTenantPackageRepo(db),
		tenantUser:       repository.NewTenantUserRepo(db),
		passwordReset:    repository.NewTenantPasswordResetRepo(db),
		sitePage:         repository.NewSitePageRepo(db),
		platformUser:     repository.NewPlatformUserRepo(db),
		mailOutbox:       repository.NewMailOutboxRepo(db),
	}
}

type services struct {
	destination      *service.DestinationService
	booking          *service.BookingService
	blog             *service.BlogService
	car              *service.CarService
	rental           *service.RentalService
	airportTransfer  *service.AirportTransferService
	contactMessage   *service.ContactMessageService
	guideApplication *service.GuideApplicationService
	mailOutbox       *service.MailOutboxService
	newsletter       *service.NewsletterService
	customer         *service.CustomerService
	review           *service.ReviewService
	partner          *service.PartnerService
	pkg              *service.PackageService
	quote            *service.QuoteService
	tenant           *service.TenantService
	tenantReview     *service.TenantReviewService
	tenantPackage    *service.TenantPackageService
	tenantUser       *service.TenantUserService
	passwordReset    *service.TenantPasswordResetService
	sitePage         *service.SitePageService
	platformUser     *service.PlatformUserService
	// subscriptionStatus reads the same provider the subscription gate does.
	subscriptionStatus *service.SubscriptionStatusService

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
	// tenant: always tenantcore, never this service's own collection.
	tenantResolver middleware.TenantResolver
	// tenantResolveClient is the tenantcore client behind tenantResolver. Kept
	// for Close on shutdown and Degraded for /readyz.
	tenantResolveClient *tenantresolve.Client

	// upload is nil when CLOUDINARY_URL is unset. See buildUpload.
	upload *service.UploadService

	// mailWorker is nil when request email is off (see buildRequestMail).
	// Held so NewForDatabase can start it and App.Close stop it.
	mailWorker *service.MailWorker

	// unsubKey, unsubUsers and unsubOutbox back the public unsubscribe route.
	// Set only when request email is on (see wireUnsubscribe); otherwise the
	// key is nil and the interfaces are true nils, so the route is not mounted.
	unsubKey    []byte
	unsubUsers  unsubscribeapi.UserOptOut
	unsubOutbox unsubscribeapi.OutboxCanceller
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
	tenantResolver, tenantResolveClient, err := buildTenantResolver(cfg, log)
	if err != nil {
		return services{}, err
	}

	// ONE mail client, shared by password reset and request email.
	mailClient := notify.NewClient(notify.Config{BaseURL: cfg.TenantcoreURL, ServiceKey: cfg.TenantcoreServiceKey})
	reqNotifier, mailWorker := buildRequestMail(cfg, r, mailClient, tenantResolveClient, log)

	// ONE upload service, shared by POST /admin/uploads and manual customers.
	uploadSvc := buildUpload(cfg, log)

	s := services{
		destination:      service.NewDestinationService(r.destination, r.tenantUser),
		booking:          service.NewBookingService(r.booking, r.customer, r.destination, r.tenantUser),
		blog:             service.NewBlogService(r.blog, r.tenantUser),
		car:              service.NewCarService(r.car, r.tenantUser),
		rental:           service.NewRentalService(r.rental, r.customer, r.car, r.tenantUser),
		airportTransfer:  service.NewAirportTransferService(r.airportTransfer, r.customer, r.tenantUser),
		contactMessage:   service.NewContactMessageService(r.contactMessage, r.tenantUser),
		guideApplication: service.NewGuideApplicationService(r.guideApplication, buildPrivateFiles(cfg, log), r.tenantUser, time.Now),
		mailOutbox:       service.NewMailOutboxService(r.mailOutbox),
		newsletter:       service.NewNewsletterService(r.newsletter),
		customer:         service.NewCustomerService(r.customer, r.booking, r.rental, r.airportTransfer, r.tenantUser).WithAvatarUploader(uploadSvc),
		review:           service.NewReviewService(r.review, r.tenantUser).WithCustomers(r.customer).WithLogger(log),
		partner:          service.NewPartnerService(r.partner, r.tenantUser),
		pkg:              service.NewPackageService(r.pkg, r.tenantPackage, r.platformUser),
		quote:            service.NewQuoteService(r.quote, r.tenantUser, r.platformUser),
		tenant:           tenantSvc,
		tenantReview:     service.NewTenantReviewService(r.tenantReview, r.tenant),
		tenantPackage:    service.NewTenantPackageService(r.tenantPackage, r.tenant, r.pkg),
		tenantUser:       service.NewTenantUserService(r.tenantUser, tokenMaker, cfg.TokenExpiry),
		// Mail goes through tenantcore, over the same link entitlement uses, so
		// this adds no configuration. A nil client means the link is off; Request
		// then answers 503 rather than pretending to send.
		passwordReset: service.NewTenantPasswordResetService(
			r.tenantUser, r.passwordReset, r.tenant,
			mailClient,
			log,
		),
		sitePage:     service.NewSitePageService(r.sitePage),
		platformUser: service.NewPlatformUserService(r.platformUser, tokenMaker, cfg.TokenExpiry),

		subscriptionStatus: service.NewSubscriptionStatusService(entProvider, log),

		entitlement:       entProvider,
		entitlementClient: entClient,

		tenantResolver:      tenantResolver,
		tenantResolveClient: tenantResolveClient,

		upload: uploadSvc,

		mailWorker: mailWorker,
	}
	wireNotifier(&s, reqNotifier)
	wireUnsubscribe(&s, reqNotifier, cfg, r)
	return s, nil
}

// wireUnsubscribe enables the public unsubscribe route exactly when the
// notifier exists, i.e. when mail links are being generated. Off, the fields
// stay zero and the route 404s.
func wireUnsubscribe(s *services, n *service.RequestNotifier, cfg *config.Config, r repos) {
	if n == nil {
		return
	}
	s.unsubKey = []byte(cfg.MailUnsubscribeKey)
	s.unsubUsers = r.tenantUser
	s.unsubOutbox = r.mailOutbox
}

// requestMailAppName is the product name shown in request emails.
const requestMailAppName = "digitalservice"

// buildRequestMail returns the staff-mail notifier and its worker, or two nils
// when request email is off. It is on only when the operator opted in
// (cfg.RequestEmailEnabled: MAIL_UNSUBSCRIBE_KEY, PUBLIC_BASE_URL and
// ADMIN_BASE_URL, validated at startup) AND the shared mail client is
// available (TENANTCORE_URL and TENANTCORE_SERVICE_KEY). Off is a supported state: startup succeeds, the services skip the
// notify call, and the state is logged here and reported by config.Features
// (the same startup log and /readyz path as password reset).
func buildRequestMail(cfg *config.Config, r repos, mail *notify.Client, ids *tenantresolve.Client, log *zap.Logger) (*service.RequestNotifier, *service.MailWorker) {
	if !cfg.RequestEmailEnabled() || !mail.Available() {
		log.Warn("request email is OFF - new bookings, rentals, transfers and guide applications will not email staff; " +
			"to turn it on set MAIL_UNSUBSCRIBE_KEY, PUBLIC_BASE_URL and ADMIN_BASE_URL (and TENANTCORE_URL, TENANTCORE_SERVICE_KEY)")
		return nil, nil
	}
	notifier := service.NewRequestNotifier(
		r.tenantUser, r.mailOutbox, service.NewTenantLinkBuilder(ids, cfg.PublicBaseURL, cfg.AdminBaseURL),
		[]byte(cfg.MailUnsubscribeKey), requestMailAppName, time.Now, log,
	)
	worker := service.NewMailWorker(r.mailOutbox, mail, r.tenantUser, time.Now, log)
	log.Info("request email ready - staff are emailed through tenantcore when a request arrives")
	return notifier, worker
}

// wireNotifier hands the notifier to the four request services. A nil notifier
// leaves them with a true nil interface (mail off): the guard is on the
// concrete pointer, so a typed nil is never stored in an interface.
func wireNotifier(s *services, n *service.RequestNotifier) {
	if n == nil {
		return
	}
	s.booking.WithNotifier(n)
	s.rental.WithNotifier(n)
	s.airportTransfer.WithNotifier(n)
	s.guideApplication.WithNotifier(n)
}

// buildTenantResolver wires how X-API-Key becomes a tenant: always through
// tenantcore, which owns tenants.
//
// It FAILS CLOSED. If the client cannot be built the deployment does not
// start, and it never falls back to this service's own tenants collection:
// that holds stale hashes after a key is re-issued or revoked, so a fallback
// would keep accepting keys tenantcore has retired. Validate refuses this
// configuration first; this is the backstop for callers that reach wiring
// without it.
func buildTenantResolver(cfg *config.Config, log *zap.Logger) (middleware.TenantResolver, *tenantresolve.Client, error) {
	c := tenantresolve.NewClient(tenantresolve.ClientConfig{
		BaseURL:    cfg.TenantcoreURL,
		ServiceKey: cfg.TenantcoreServiceKey,
		Log:        log,
	})
	if c == nil {
		return nil, nil, errors.New("tenant resolution needs TENANTCORE_URL and TENANTCORE_SERVICE_KEY; " +
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

// buildPrivateFiles returns the private applicant-file store, or a TRUE nil
// interface when private storage is off or misconfigured. The service treats a
// nil PrivateFiles as "uploads unavailable" and answers 503.
//
// The return type is the interface, and the concrete pointer is only converted
// after the nil checks: returning a nil *PrivateFileService through the
// interface would give a non-nil interface holding a nil pointer, and the
// service's `files == nil` guard would pass it straight through to a panic.
//
// The URL is a credential and is never logged.
func buildPrivateFiles(cfg *config.Config, log *zap.Logger) service.PrivateFiles {
	if !cfg.PrivateFilesEnabled() {
		return nil
	}
	svc, err := service.NewPrivateFileService(cfg.PrivateFilesURL(), cfg.UploadMaxBytes)
	if err != nil {
		log.Error("private file storage unavailable - initialisation failed (check the private files URL setting); " +
			"POST /guide-applications will answer 503 FEATURE_UNAVAILABLE")
		return nil
	}
	if svc == nil {
		log.Error("private file storage unavailable - no store was created; " +
			"POST /guide-applications will answer 503 FEATURE_UNAVAILABLE")
		return nil
	}
	log.Info("private file storage ready", zap.Int64("max_bytes", cfg.UploadMaxBytes))
	return svc
}
