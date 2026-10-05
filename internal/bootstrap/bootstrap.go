// Package bootstrap wires every dependency the service needs and hands back
// a running application.
//
// It exists so main.go states the startup sequence and nothing else, and so
// the same wiring can be built for tests without copying it.
//
// The rule it enforces: a missing REQUIRED setting stops the process before
// anything is served, and a missing OPTIONAL one disables exactly one feature
// and says so. Every buildX below returns a nil (or disabled) dependency
// instead of exiting, and the feature that needed it reports its own absence
// on the routes that need it — see config.Features, /readyz and
// apierr.FeatureUnavailable.
//
// The reason is not tidiness. Refusing to boot the whole API because an image
// host was misconfigured is a worse outage than the one it prevents: every
// storefront goes dark to protect one upload button.
package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eandstravel/digitalservice/internal/api"
	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/eandstravel/digitalservice/internal/entitlement"
	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/internal/tenantresolve"
	"github.com/eandstravel/digitalservice/pkg/logger"
	"github.com/eandstravel/digitalservice/pkg/token"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// App is a fully wired application.
type App struct {
	Config *config.Config
	Log    *zap.Logger
	Engine *gin.Engine

	mongo   *mongo.Client
	limiter *middleware.RateLimiter
	// entitlement is nil when the platform link is off. Held only so Close
	// can stop its cache janitor.
	entitlement *entitlement.Client
	// tenantResolve is nil unless TENANT_RESOLVER=tenantcore. Held so Close
	// can stop its cache janitor.
	tenantResolve *tenantresolve.Client

	// passwordReset sends its mail after responding, so shutdown drains it: a
	// reset requested a moment before a restart should still arrive.
	passwordReset *service.TenantPasswordResetService
}

// New wires everything from cfg.
//
// It returns an error rather than exiting so a test can assert on a bad
// configuration instead of killing the test binary. main.go is the one place
// that turns the error into a non-zero exit.
func New(ctx context.Context, cfg *config.Config) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	client, db, err := connectMongo(ctx, cfg)
	if err != nil {
		return nil, err
	}

	app, err := NewForDatabase(ctx, cfg, db, logger.Log)
	if err != nil {
		_ = client.Disconnect(ctx)
		return nil, err
	}
	app.mongo = client
	return app, nil
}

// NewForDatabase wires the app against an already-open database.
//
// It is the half of New that integration tests need: they bring their own
// disposable database and must exercise the same wiring the binary uses, not
// a second copy of it that can drift from it. New is then just "connect, then
// this".
func NewForDatabase(ctx context.Context, cfg *config.Config, db *mongo.Database, log *zap.Logger) (*App, error) {
	logFeatures(log, cfg)
	warnUntrustedProxies(log, cfg)

	// Indexes are NOT optional. They carry the uniqueness constraints the
	// service relies on for correctness — a duplicate tenant API key or user
	// email is not a degraded feature, it is a security problem — so a
	// failure here stops startup rather than degrading.
	if err := repository.EnsureIndexes(ctx, db); err != nil {
		return nil, fmt.Errorf("index setup: %w", err)
	}

	tokenMaker, err := token.NewMaker(cfg.TokenSecret)
	if err != nil {
		return nil, fmt.Errorf("token maker: %w", err)
	}

	repos := newRepos(db)
	svcs, err := newServices(repos, tokenMaker, cfg, log)
	if err != nil {
		return nil, err
	}

	// Bootstrapping the first platform user is optional: a deployment that
	// already has one does not need the env vars, and one that has neither
	// the vars nor a user is a deployment nobody can log into — which is
	// worth a loud warning, not a refusal to start.
	if cfg.SuperadminBootstrapEnabled() {
		if err := svcs.platformUser.EnsureBootstrap(ctx, cfg.SuperadminName, cfg.SuperadminEmail, cfg.SuperadminPassword); err != nil {
			// Not fatal: the rest of the API is fine, and an operator can
			// create the account by other means. Loud, because until they
			// do, nobody can administer the platform.
			log.Error("superadmin bootstrap failed — no platform user was created from the environment",
				zap.Error(err))
		}
	}

	// The tenantcore-token group exists only when the public key is set; with
	// none, a nil verifier makes the group answer 404.
	var tcVerifier *token.TenantcoreVerifier
	if cfg.TenantcorePublicKey != "" {
		tcVerifier, err = token.NewVerifier(cfg.TenantcorePublicKey)
		if err != nil {
			return nil, fmt.Errorf("tenantcore verifier: %w", err)
		}
	}

	limiter := middleware.NewRateLimiter()

	srv := api.NewServer(api.Deps{
		Config: cfg,
		Log:    log,

		Auth:           middleware.NewAuthMiddleware(tokenMaker),
		TenantcoreAuth: middleware.NewTenantcoreAuth(tcVerifier),
		TenantMW:       middleware.NewTenantMiddleware(svcs.tenantResolver),
		SubscriptionMW: middleware.NewSubscriptionMiddleware(svcs.entitlement),
		RateLimiter:    limiter,
		Entitlement:    svcs.entitlement,
		// Nil when the platform link is off; /readyz reads Degraded off it.
		EntitlementClient: svcs.entitlementClient,
		// Nil in local resolver mode; /readyz reads Degraded off it.
		TenantResolveClient: svcs.tenantResolveClient,

		Destination:     svcs.destination,
		Blog:            svcs.blog,
		Car:             svcs.car,
		Review:          svcs.review,
		Partner:         svcs.partner,
		Package:         svcs.pkg,
		Booking:         svcs.booking,
		Rental:          svcs.rental,
		AirportTransfer: svcs.airportTransfer,
		ContactMessage:  svcs.contactMessage,
		Newsletter:      svcs.newsletter,
		Quote:           svcs.quote,
		Customer:        svcs.customer,
		Tenants:         svcs.tenant,
		TenantReview:    svcs.tenantReview,
		TenantPackage:   svcs.tenantPackage,
		TenantUser:      svcs.tenantUser,
		PasswordReset:   svcs.passwordReset,
		SitePage:        svcs.sitePage,
		PlatformUser:    svcs.platformUser,
		Upload:          svcs.upload,
	})

	return &App{
		Config:      cfg,
		Log:         log,
		Engine:      srv.Handler(),
		limiter:     limiter,
		entitlement: svcs.entitlementClient,

		tenantResolve: svcs.tenantResolveClient,

		passwordReset: svcs.passwordReset,
	}, nil
}

// Run serves until SIGINT/SIGTERM, then shuts down cleanly.
func (a *App) Run() error {
	srv := &http.Server{
		Addr:         ":" + a.Config.AppPort,
		Handler:      a.Engine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		a.Log.Info("server starting",
			zap.String("port", a.Config.AppPort),
			zap.String("env", string(a.Config.AppEnv)))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-quit:
		a.Log.Info("shutdown signal received", zap.String("signal", sig.String()))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	a.Close(shutdownCtx)
	a.Log.Info("server stopped")
	return nil
}

// Close releases everything New acquired.
func (a *App) Close(ctx context.Context) {
	if a.limiter != nil {
		a.limiter.Close()
	}
	a.entitlement.Close()   // nil-safe
	a.tenantResolve.Close() // nil-safe
	if a.passwordReset != nil {
		a.passwordReset.Drain()
	}
	if a.mongo != nil {
		_ = a.mongo.Disconnect(ctx)
	}
}

// logFeatures states every optional capability and its status at startup.
//
// This is the loud half of "degrade instead of crash". Degrading silently is
// how a feature ends up switched off in production for a week with nobody
// noticing; a disabled feature is logged at WARN precisely so it shows up in
// whatever filters out the routine lines.
func logFeatures(log *zap.Logger, cfg *config.Config) {
	for _, f := range cfg.Features() {
		if f.Enabled {
			log.Info("feature enabled", zap.String("feature", f.Name))
			continue
		}
		log.Warn("feature DISABLED — this deployment is running degraded",
			zap.String("feature", f.Name),
			zap.String("detail", f.Detail))
	}
}

// warnUntrustedProxies says, once at startup, that the resolve limiter can be
// bypassed: in tenantcore mode with TRUSTED_PROXIES unset, gin believes any
// client-supplied X-Forwarded-For.
func warnUntrustedProxies(log *zap.Logger, cfg *config.Config) {
	if cfg.TenantResolverTenantcoreEnabled() && !cfg.TrustedProxiesSet() {
		log.Warn("TRUSTED_PROXIES is not set — the per-IP tenant resolve limiter trusts a client-supplied "+
			"X-Forwarded-For and can be bypassed with one header; set it to the hosting provider's proxy ranges",
			zap.String("setting", "TRUSTED_PROXIES"))
	}
}

func connectMongo(ctx context.Context, cfg *config.Config) (*mongo.Client, *mongo.Database, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return nil, nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, nil, fmt.Errorf("mongo ping: %w", err)
	}
	logger.Log.Info("mongodb connected", zap.String("database", cfg.MongoDB))
	return client, client.Database(cfg.MongoDB), nil
}
