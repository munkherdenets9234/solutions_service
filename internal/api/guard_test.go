package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/api/tenant"
	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/eandstravel/digitalservice/internal/entitlement"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

// These tests assert the property the audience split exists to provide: a
// caller with no credentials cannot reach a controller behind a gate.
//
// They build the router with a Deps whose services are all nil. That is not a
// shortcut, it is the assertion: route registration only stores pointers, so
// a nil service is harmless right up until a handler is actually invoked. If
// one of these requests ever reaches a controller, it dereferences nil and
// the test panics — which is a much louder failure than a 200 with an empty
// body, and exactly what we want if a gate is ever removed.

// publicPlatformRoutes are the platform routes that are meant to be reachable
// with no credentials at all. Everything else under /platform must refuse an
// unauthenticated caller.
//
// Keep this list in sync deliberately. If a route moves between public and
// private, this test is where it shows up — which is the point.
var publicPlatformRoutes = map[string]bool{
	"POST /api/v1/platform/login":               true,
	"GET /api/v1/platform/admins":               true,
	"GET /api/v1/platform/tenants":              true,
	"GET /api/v1/platform/tenants/:id":          true,
	"GET /api/v1/platform/tenants/:id/packages": true,
	"GET /api/v1/platform/projects":             true,
	"GET /api/v1/platform/projects/:slug":       true,
	"GET /api/v1/platform/reviews":              true,
	"GET /api/v1/platform/packages":             true,
	"GET /api/v1/platform/packages/:id":         true,
	"POST /api/v1/platform/quotes":              true,
	"GET /api/v1/platform/quotes":               true,
}

func testEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return NewServer(Deps{
		Log: zap.NewNop(),
		Config: &config.Config{
			AppEnv:            config.EnvTest,
			RateLimitEnabled:  false,
			AuthRatePerMinute: 10,
			LeadRatePerMinute: 20,
			RateLimitBurst:    5,
		},
	}).Handler()
}

// TestEveryTenantRouteRequiresAPIKey walks the real route table rather than a
// hand-written list, so a route added tomorrow is covered without anyone
// remembering to add it here.
func TestEveryTenantRouteRequiresAPIKey(t *testing.T) {
	e := testEngine(t)

	checked := 0
	for _, r := range e.Routes() {
		if !strings.HasPrefix(r.Path, "/api/v1/") || strings.HasPrefix(r.Path, "/api/v1/platform/") {
			continue
		}
		checked++
		t.Run(r.Method+" "+r.Path, func(t *testing.T) {
			// No X-API-Key. TenantMiddleware must refuse before any
			// controller or service is touched.
			assertUnauthorized(t, e, r.Method, fillParams(r.Path), nil)
		})
	}
	if checked == 0 {
		t.Fatal("no tenant routes were checked — the route table or the prefix filter is wrong")
	}
	t.Logf("checked %d tenant routes", checked)
}

// TestPrivatePlatformRoutesRequireToken is the same assertion for the
// platform audience, minus the routes that are deliberately public.
func TestPrivatePlatformRoutesRequireToken(t *testing.T) {
	e := testEngine(t)

	checked := 0
	for _, r := range e.Routes() {
		if !strings.HasPrefix(r.Path, "/api/v1/platform/") {
			continue
		}
		if publicPlatformRoutes[r.Method+" "+r.Path] {
			continue
		}
		checked++
		t.Run(r.Method+" "+r.Path, func(t *testing.T) {
			assertUnauthorized(t, e, r.Method, fillParams(r.Path), nil)
		})
	}
	if checked == 0 {
		t.Fatal("no private platform routes were checked — publicPlatformRoutes is probably stale")
	}
	t.Logf("checked %d private platform routes", checked)
}

// TestOperationalRoutesAreOpen keeps the health and docs endpoints reachable.
// They carry no data and a monitor cannot authenticate.
func TestOperationalRoutesAreOpen(t *testing.T) {
	e := testEngine(t)

	for _, path := range []string{"/healthz", "/readyz", "/docs", "/docs/openapi.json"} {
		res := do(e, http.MethodGet, path, nil)
		if res.Code != http.StatusOK {
			t.Errorf("%s: got %d, want 200", path, res.Code)
		}
	}
}

// TestReadyzReportsDisabledFeatures is the regression test for the thing that
// went unnoticed twice: a feature switched off by configuration must be
// visible somewhere that keeps answering, not only in a startup log line that
// scrolled away days ago.
func TestReadyzReportsDisabledFeatures(t *testing.T) {
	e := testEngine(t) // uploads unset, rate limiting off

	res := do(e, http.MethodGet, "/readyz", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 — a degraded deployment is not an unhealthy one", res.Code)
	}

	var body struct {
		Degraded bool `json:"degraded"`
		Features []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
			Detail  string `json:"detail"`
		} `json:"features"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !body.Degraded {
		t.Error("degraded should be true when uploads are unconfigured")
	}

	found := false
	for _, f := range body.Features {
		if f.Name != "uploads" {
			continue
		}
		found = true
		if f.Enabled {
			t.Error("uploads reported enabled with no CLOUDINARY_URL")
		}
		if !strings.Contains(f.Detail, "CLOUDINARY_URL") {
			t.Errorf("detail should name the setting to fix, got %q", f.Detail)
		}
	}
	if !found {
		t.Error("uploads missing from the feature list")
	}
}

// assertUnauthorized checks both the status and the error envelope: a 401
// with the wrong body shape would still break every client.
func assertUnauthorized(t *testing.T, e *gin.Engine, method, path string, body []byte) {
	t.Helper()

	res := do(e, method, path, body)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 — this route is reachable without credentials\nbody: %s",
			res.Code, res.Body.String())
	}

	var env struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Error   struct {
			Domain  string `json:"domain"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (body %s)", err, res.Body.String())
	}
	if env.Success {
		t.Error("success should be false on an error response")
	}
	if env.Error.Code != apierr.CodeUnauthorized {
		t.Errorf("code = %q, want %q", env.Error.Code, apierr.CodeUnauthorized)
	}
	if env.Error.Domain != apierr.DomainAuth && env.Error.Domain != apierr.DomainTenant {
		t.Errorf("domain = %q, want AUTH or TENANT", env.Error.Domain)
	}
	// The legacy top-level message must survive: existing clients read it.
	if env.Message == "" {
		t.Error("top-level message is empty — existing clients read this field")
	}
}

func do(e *gin.Engine, method, path string, body []byte) *httptest.ResponseRecorder {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, path, strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	return w
}

// fillParams substitutes a placeholder for each :param so the request reaches
// the route it is meant to test. The values are never looked up: every one of
// these requests is refused before a handler runs.
func fillParams(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "placeholder"
		}
	}
	return strings.Join(parts, "/")
}

// ── Module gate ──────────────────────────────────────────────────────────────
//
// These cover the seam the "one deployment per product" split runs through.
// A product mounted as a tenant.Module must be, in order: reachable only with
// the tenant's API key, refused when the plan does not include it, and served
// when it does. Proving that here — against the real engine, the real
// middleware order and the real error renderer — is what makes it safe to
// move carwash in behind it later, rather than discovering the gate order was
// wrong after the routes exist.

const testModule = "carwash"

// moduleEngine builds the production engine with one extra product mounted,
// entitled to the modules given.
func moduleEngine(t *testing.T, modules ...string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	provider := entitlement.ProviderFunc(func(_ context.Context, id primitive.ObjectID) (entitlement.Entitlement, error) {
		return entitlement.Entitlement{
			TenantID:  id,
			Status:    entitlement.StatusActive,
			PeriodEnd: time.Now().Add(24 * time.Hour),
			Modules:   modules,
		}, nil
	})

	return NewServer(Deps{
		Log:         zap.NewNop(),
		Config:      &config.Config{AppEnv: config.EnvTest},
		Entitlement: provider,
		// A stand-in for a real product's Register. What is under test is the
		// gate in front of it, not what it serves.
		Modules: []tenant.Module{{
			Name: testModule,
			Register: func(g *gin.RouterGroup) {
				g.GET("/bays", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
			},
		}},
	}).Handler()
}

// A module route is tenant-scoped like everything else. This is the property
// TestEveryTenantRouteRequiresAPIKey asserts for the whole tree; it is
// restated here because a module is mounted by a different code path and
// could plausibly miss it.
func TestModuleRoutesStillRequireAPIKey(t *testing.T) {
	e := moduleEngine(t, testModule)

	assertUnauthorized(t, e, http.MethodGet, "/api/v1/"+testModule+"/bays", nil)
}

// The gate refuses before the tenant resolution can succeed, so this test
// cannot get past TenantMiddleware with a fake key — which is itself the
// correct order and worth stating: entitlement is checked for a *known*
// tenant, never for an anonymous caller.
//
// TestEveryTenantRouteRequiresAPIKey covers the anonymous case. The entitled
// and unentitled cases are covered against the gate directly in
// internal/middleware/entitlement_test.go, where a tenant can be placed in
// context without a database. What this test pins down is the wiring: the
// module is mounted where it claims to be.
func TestModuleIsMountedUnderItsOwnPrefix(t *testing.T) {
	e := moduleEngine(t, testModule)

	var found bool
	for _, r := range e.Routes() {
		if r.Method == http.MethodGet && r.Path == "/api/v1/"+testModule+"/bays" {
			found = true
		}
	}
	if !found {
		t.Fatalf("module route not mounted at /api/v1/%s/bays; routes: %v", testModule, e.Routes())
	}

	// And it must not have leaked into the unprefixed brochure surface,
	// where /cars already means something else entirely.
	for _, r := range e.Routes() {
		if r.Path == "/api/v1/bays" {
			t.Error("module route mounted without its prefix — this is how /cars collides")
		}
	}
}

// A module adds routes; it must not silently remove or re-gate existing ones.
func TestMountingAModuleLeavesTheBrochureSurfaceIntact(t *testing.T) {
	base := testEngine(t)
	withModule := moduleEngine(t, testModule)

	count := func(e *gin.Engine) map[string]bool {
		out := map[string]bool{}
		for _, r := range e.Routes() {
			out[r.Method+" "+r.Path] = true
		}
		return out
	}

	before, after := count(base), count(withModule)
	for route := range before {
		if !after[route] {
			t.Errorf("mounting a module dropped an existing route: %s", route)
		}
	}
}
