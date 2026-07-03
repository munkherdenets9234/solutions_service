package testutil

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/eandstravel/digitalservice/internal/handler"
	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/logger"
	"github.com/eandstravel/digitalservice/pkg/token"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
)

// initLoggerOnce guards logger.Init, which panics on nil-deref if a request
// comes in before it's called - main.go calls it at startup, but nothing
// does that for the router when it's wired up directly in tests.
var initLoggerOnce sync.Once

// TokenSecret is used to sign tokens for every test server, so tests can
// mint tokens directly via pkg/token (e.g. to simulate a forged/smuggled
// claim) without going through the HTTP login flow.
const TokenSecret = "test-secret-at-least-32-characters-long"

// App bundles a running test server with the pieces a test might want direct
// access to (e.g. the token maker, to mint tokens that shouldn't be
// obtainable through the normal API, for security regression tests).
type App struct {
	Server *httptest.Server
	Maker  *token.Maker
}

// NewApp wires the full application - repos, services, handlers, router -
// against the given database, exactly as cmd/api/main.go does, and serves it
// via an httptest.Server.
func NewApp(t testing.TB, db *mongo.Database) *App {
	t.Helper()
	gin.SetMode(gin.TestMode)
	initLoggerOnce.Do(func() { logger.Init("test") })

	if err := repository.EnsureIndexes(context.Background(), db); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	maker, err := token.NewMaker(TokenSecret)
	if err != nil {
		t.Fatalf("new token maker: %v", err)
	}

	destRepo := repository.NewDestinationRepo(db)
	bookingRepo := repository.NewBookingRepo(db)
	blogRepo := repository.NewBlogRepo(db)
	customerRepo := repository.NewCustomerRepo(db)
	carRepo := repository.NewCarRepo(db)
	rentalRepo := repository.NewRentalRepo(db)
	airportTransferRepo := repository.NewAirportTransferRepo(db)
	contactMessageRepo := repository.NewContactMessageRepo(db)
	tenantRepo := repository.NewTenantRepo(db)
	tenantUserRepo := repository.NewTenantUserRepo(db)
	platformUserRepo := repository.NewPlatformUserRepo(db)
	subscriptionRepo := repository.NewSubscriptionRepo(db)

	destSvc := service.NewDestinationService(destRepo)
	bookingSvc := service.NewBookingService(bookingRepo, customerRepo, destRepo)
	blogSvc := service.NewBlogService(blogRepo)
	carSvc := service.NewCarService(carRepo)
	rentalSvc := service.NewRentalService(rentalRepo, customerRepo, carRepo)
	airportTransferSvc := service.NewAirportTransferService(airportTransferRepo, customerRepo)
	contactMessageSvc := service.NewContactMessageService(contactMessageRepo)
	tenantSvc := service.NewTenantService(tenantRepo)
	tenantUserSvc := service.NewTenantUserService(tenantUserRepo, maker, 24)
	platformUserSvc := service.NewPlatformUserService(platformUserRepo, maker, 24)
	subscriptionSvc := service.NewSubscriptionService(subscriptionRepo)

	if err := platformUserSvc.EnsureBootstrap(context.Background(), SuperadminName, SuperadminEmail, SuperadminPassword); err != nil {
		t.Fatalf("bootstrap superadmin: %v", err)
	}

	router := handler.NewRouter(
		handler.NewDestinationHandler(destSvc),
		handler.NewBookingHandler(bookingSvc),
		handler.NewBlogHandler(blogSvc),
		handler.NewCarHandler(carSvc),
		handler.NewRentalHandler(rentalSvc),
		handler.NewAirportTransferHandler(airportTransferSvc),
		handler.NewContactMessageHandler(contactMessageSvc),
		handler.NewTenantHandler(tenantSvc, tenantUserSvc),
		handler.NewTenantUserHandler(tenantUserSvc),
		handler.NewPlatformUserHandler(platformUserSvc),
		handler.NewSubscriptionHandler(subscriptionSvc),
		middleware.NewAuthMiddleware(maker),
		middleware.NewTenantMiddleware(tenantSvc),
	)

	engine := gin.New()
	router.Register(engine)

	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)

	return &App{Server: srv, Maker: maker}
}
