package testutil

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/eandstravel/digitalservice/internal/bootstrap"
	"github.com/eandstravel/digitalservice/internal/config"
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
// claim) without going through the HTTP login flow. Generated per test
// binary run rather than hardcoded. 40 hex characters, comfortably over the
// 32-character minimum config.Validate enforces.
var TokenSecret = randomHex(20)

// App bundles a running test server with the pieces a test might want direct
// access to (e.g. the token maker, to mint tokens that shouldn't be
// obtainable through the normal API, for security regression tests).
type App struct {
	Server *httptest.Server
	Maker  *token.Maker
}

// NewApp wires the full application against the given database and serves it
// via an httptest.Server.
//
// It goes through bootstrap.NewForDatabase — the same wiring cmd/api uses —
// rather than repeating the repo/service/handler graph here. The previous
// version duplicated that graph, which meant a route added to the real router
// was not necessarily present in the one the tests exercised, and the tests
// would still pass.
func NewApp(t testing.TB, db *mongo.Database) *App {
	t.Helper()
	gin.SetMode(gin.TestMode)
	initLoggerOnce.Do(func() { logger.Init("test") })

	cfg := TestConfig()

	app, err := bootstrap.NewForDatabase(context.Background(), cfg, db, logger.Log)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(func() { app.Close(context.Background()) })

	maker, err := token.NewMaker(cfg.TokenSecret)
	if err != nil {
		t.Fatalf("new token maker: %v", err)
	}

	srv := httptest.NewServer(app.Engine)
	t.Cleanup(srv.Close)

	return &App{Server: srv, Maker: maker}
}

// TestConfig is the configuration every test server runs on.
//
// Rate limiting is OFF here. A test suite makes hundreds of requests from one
// address in seconds, which is precisely the shape the limiter exists to
// refuse; leaving it on would make unrelated tests fail in whatever order
// they happened to run. The limiter has its own targeted test instead.
func TestConfig() *config.Config {
	return &config.Config{
		AppEnv:      config.EnvTest,
		AppPort:     "0",
		MongoURI:    "mongodb://test",
		MongoDB:     "testdb",
		TokenSecret: TokenSecret,
		TokenExpiry: 24,

		// Uploads are deliberately configured so the upload route is mounted
		// and reachable. The credentials are fake: any test that gets as far
		// as talking to Cloudinary fails at that call, which is the correct
		// outcome for a suite that must not depend on a third party.
		CloudinaryURL:  "cloudinary://key:secret@test-cloud",
		UploadMaxBytes: 10 << 20,

		SuperadminName:     SuperadminName,
		SuperadminEmail:    SuperadminEmail,
		SuperadminPassword: SuperadminPassword,

		RateLimitEnabled: false,
	}
}
