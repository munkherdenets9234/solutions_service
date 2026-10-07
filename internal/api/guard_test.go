package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/api/tenant"
	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/eandstravel/digitalservice/internal/entitlement"
	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/token"
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

// tenantcoreRoutes are the platform routes guarded by a tenantcore-signed token
// instead of this service's HMAC one. They are exempt from the generic "no
// token is 401" walk only because, with no public key configured, they answer
// 404 (group off) rather than 401; TestTenantcoreRoutesRefuseHMACAndAnonymous
// and TestTenantcoreRoutesAreOffWithoutAKey assert their own guard instead.
var tenantcoreRoutes = map[string]bool{
	"GET /api/v1/platform/tenants/:id/admin-users":                         true,
	"POST /api/v1/platform/tenants/:id/admin-users/:user_id/reset-password": true,
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
	// Both engines: the plain one, and one with request email on, which mounts
	// the exempt unsubscribe routes below. Every OTHER route in the mail engine
	// must still refuse a caller with no key.
	for name, e := range map[string]*gin.Engine{"mail off": testEngine(t), "mail on": mailEngine(t)} {
		checked := 0
		for _, r := range e.Routes() {
			if !strings.HasPrefix(r.Path, "/api/v1/") || strings.HasPrefix(r.Path, "/api/v1/platform/") {
				continue
			}
			if publicUnauthenticatedRoutes[r.Method+" "+r.Path] != "" {
				continue
			}
			checked++
			t.Run(name+" "+r.Method+" "+r.Path, func(t *testing.T) {
				// No X-API-Key. TenantMiddleware must refuse before any
				// controller or service is touched.
				assertUnauthorized(t, e, r.Method, fillParams(r.Path), nil)
			})
		}
		if checked == 0 {
			t.Fatalf("%s: no tenant routes were checked — the route table or the prefix filter is wrong", name)
		}
		t.Logf("%s: checked %d tenant routes", name, checked)
	}
}

// publicUnauthenticatedRoutes are routes outside /platform that are reachable
// with neither an X-API-Key nor a session, each with the written reason.
// Adding to this list needs the same justification in the pull request.
var publicUnauthenticatedRoutes = map[string]string{
	"GET /api/v1/public/unsubscribe":  unsubscribeJustification,
	"POST /api/v1/public/unsubscribe": unsubscribeJustification,
}

const unsubscribeJustification = "unauthenticated by necessity: the mail recipient's browser has no session and sends no " +
	"X-API-Key. It changes only the receive_emails flag (and cancels queued mail) of the one user named in a signed token; " +
	"the tenant comes only from that token. The token is HMAC-signed and expires, the route is rate limited on the real " +
	"visitor, GET changes nothing, and every invalid token gets an identical response. Not mounted when mail is off."

// mailEngine is the engine with request email on. The key is built at run time
// so no secret-looking literal is committed.
func mailEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return NewServer(Deps{
		Log:               zap.NewNop(),
		Config:            &config.Config{AppEnv: config.EnvTest},
		UnsubscribeKey:    []byte(strings.Repeat("k", 32)),
		UnsubscribeUsers:  noopOptOut{},
		UnsubscribeOutbox: noopOptOut{},
	}).Handler()
}

type noopOptOut struct{}

func (noopOptOut) SetReceiveEmails(context.Context, primitive.ObjectID, primitive.ObjectID, bool) error {
	return nil
}
func (noopOptOut) CancelPendingForUser(context.Context, primitive.ObjectID, primitive.ObjectID) (int64, error) {
	return 0, nil
}

// The unsubscribe routes exist only with mail on, and when they do exist they
// are reachable with no X-API-Key (the tenant gate is not on them).
func TestUnsubscribeRoutesMountOnlyWithMailAndNeedNoAPIKey(t *testing.T) {
	has := func(e *gin.Engine, key string) bool {
		for _, r := range e.Routes() {
			if r.Method+" "+r.Path == key {
				return true
			}
		}
		return false
	}
	off, on := testEngine(t), mailEngine(t)
	for key := range publicUnauthenticatedRoutes {
		if has(off, key) {
			t.Errorf("%s registered with mail off", key)
		}
		if !has(on, key) {
			t.Errorf("%s not registered with mail on", key)
		}
	}
	if w := do(off, http.MethodGet, "/api/v1/public/unsubscribe", nil); w.Code != http.StatusNotFound && w.Code != http.StatusUnauthorized {
		t.Errorf("mail off: got %d", w.Code)
	}
	if w := do(on, http.MethodGet, "/api/v1/public/unsubscribe?token=x", nil); w.Code != http.StatusOK {
		t.Errorf("mail on, no API key: got %d, want 200", w.Code)
	}
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
		if publicPlatformRoutes[r.Method+" "+r.Path] || tenantcoreRoutes[r.Method+" "+r.Path] {
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

// tenantcoreEngine is the production engine with a verifier configured, as in
// a deployment that sets TENANTCORE_PUBLIC_KEY.
func tenantcoreEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	v, err := token.NewVerifier(base64.StdEncoding.EncodeToString(pub))
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(Deps{
		Log:            zap.NewNop(),
		Config:         &config.Config{AppEnv: config.EnvTest},
		TenantcoreAuth: middleware.NewTenantcoreAuth(v),
	}).Handler()
}

// The two tenantcore-token routes must be registered, must refuse a caller with
// no token, and must refuse this service's own HMAC platform token: a login
// here must not open a group meant for tenantcore's operators.
func TestTenantcoreRoutesRefuseHMACAndAnonymous(t *testing.T) {
	e := tenantcoreEngine(t)

	maker, err := token.NewMaker(strings.Repeat("k", 32))
	if err != nil {
		t.Fatal(err)
	}
	hmacTok, _, err := maker.CreateToken("u1", "superadmin", "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	found := 0
	for _, r := range e.Routes() {
		if !tenantcoreRoutes[r.Method+" "+r.Path] {
			continue
		}
		found++
		path := fillParams(r.Path)
		assertUnauthorized(t, e, r.Method, path, nil)

		req := httptest.NewRequest(r.Method, path, nil)
		req.Header.Set("Authorization", "Bearer "+hmacTok)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with an HMAC platform token: got %d, want 401", r.Method, r.Path, w.Code)
		}
	}
	if found != len(tenantcoreRoutes) {
		t.Fatalf("found %d of %d tenantcore routes in the route table", found, len(tenantcoreRoutes))
	}
}

// With no public key the group is off: 404, not a half-open route.
func TestTenantcoreRoutesAreOffWithoutAKey(t *testing.T) {
	e := testEngine(t)
	for _, r := range e.Routes() {
		if !tenantcoreRoutes[r.Method+" "+r.Path] {
			continue
		}
		if w := do(e, r.Method, fillParams(r.Path), nil); w.Code != http.StatusNotFound {
			t.Errorf("%s %s with no key: got %d, want 404", r.Method, r.Path, w.Code)
		}
	}
}

// The public reads that share the /platform/tenants/:id prefix stay public.
func TestPublicTenantRoutesUnaffectedByTenantcoreGroup(t *testing.T) {
	e := tenantcoreEngine(t)
	for _, route := range []string{"GET /api/v1/platform/tenants/:id", "GET /api/v1/platform/tenants/:id/packages"} {
		if !publicPlatformRoutes[route] {
			t.Fatalf("%s should be listed as public", route)
		}
		found := false
		for _, r := range e.Routes() {
			if r.Method+" "+r.Path == route {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is no longer registered", route)
		}
	}
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

// Password reset is reachable without a session (nobody who has forgotten their
// password has one) but NOT without a tenant: it is scoped by X-API-Key like
// every other tenant route, which is also what keeps one tenant's reset from
// touching another's.
func TestPasswordResetRoutesExistAndRequireAnAPIKey(t *testing.T) {
	e := testEngine(t)

	want := map[string]bool{
		"POST /api/v1/password-reset/request": false,
		"POST /api/v1/password-reset/confirm": false,
	}
	for _, r := range e.Routes() {
		key := r.Method + " " + r.Path
		if _, ok := want[key]; ok {
			want[key] = true
			assertUnauthorized(t, e, r.Method, r.Path, []byte(`{"email":"a@example.com"}`))
		}
	}
	for route, found := range want {
		if !found {
			t.Fatalf("route %s is not registered", route)
		}
	}
}

// The translation routes: three for the tenant admin (behind the token and the
// subscription gate) and one public read for the storefront. All four sit under
// the tenant's X-API-Key like every other route.
func TestTranslationRoutesExistAndRequireAnAPIKey(t *testing.T) {
	e := testEngine(t)

	want := map[string]bool{
		"GET /api/v1/admin/translations":       false,
		"GET /api/v1/admin/translations/:page": false,
		"PUT /api/v1/admin/translations/:page": false,
		"GET /api/v1/translations":             false,
	}
	for _, r := range e.Routes() {
		key := r.Method + " " + r.Path
		if _, ok := want[key]; ok {
			want[key] = true
			assertUnauthorized(t, e, r.Method, fillParams(r.Path), []byte(`{"entries":[]}`))
		}
	}
	for route, found := range want {
		if !found {
			t.Fatalf("route %s is not registered", route)
		}
	}
}

// The editor routes must live under /admin, the group that carries
// Auth("admin"); the public read must not.
func TestAdminTranslationRoutesAreInTheTokenGroup(t *testing.T) {
	e := testEngine(t)

	have := map[string]bool{}
	for _, r := range e.Routes() {
		have[r.Method+" "+r.Path] = true
	}
	for _, route := range []string{
		"GET /api/v1/admin/translations",
		"GET /api/v1/admin/translations/:page",
		"PUT /api/v1/admin/translations/:page",
		"GET /api/v1/translations",
	} {
		if !have[route] {
			t.Errorf("route %s is not registered", route)
		}
	}
	for _, route := range []string{
		"PUT /api/v1/translations/:page",
		"POST /api/v1/translations",
		"DELETE /api/v1/translations/:page",
	} {
		if have[route] {
			t.Errorf("unexpected public write route %s", route)
		}
	}
}

// fakeResolver lets a request past TenantMiddleware with any non-empty key, so
// the next gate down (the bearer) is the one under test.
type fakeResolver struct{}

func (fakeResolver) Resolve(_ context.Context, _ string) (middleware.TenantRef, error) {
	return middleware.TenantRef{ID: primitive.NewObjectID()}, nil
}

// TestGuideAdminRoutesRequireBearer: applicant PII sits behind the tenant
// admin's bearer, not the storefront key alone. The key is published in the
// storefront's JavaScript, so a key with no token must be refused.
func TestGuideAdminRoutesRequireBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	maker, err := token.NewMaker(strings.Repeat("k", 32))
	if err != nil {
		t.Fatal(err)
	}
	e := NewServer(Deps{
		Log:            zap.NewNop(),
		Config:         &config.Config{AppEnv: config.EnvTest},
		Auth:           middleware.NewAuthMiddleware(maker),
		TenantMW:       middleware.NewTenantMiddleware(fakeResolver{}),
		SubscriptionMW: middleware.NewSubscriptionMiddleware(entitlement.Unenforced{}),
	}).Handler()

	const prefix = "/api/v1/admin/guide-applications"
	checked := 0
	for _, r := range e.Routes() {
		if !strings.HasPrefix(r.Path, prefix) {
			continue
		}
		checked++
		t.Run(r.Method+" "+r.Path, func(t *testing.T) {
			req := httptest.NewRequest(r.Method, fillParams(r.Path), strings.NewReader(`{}`))
			req.Header.Set("X-API-Key", "test-key")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			e.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("got %d, want 401 - applicant data reachable with the key alone\nbody: %s", w.Code, w.Body.String())
			}
		})
	}
	if checked != 6 {
		t.Fatalf("found %d guide admin routes, want 6", checked)
	}
}
