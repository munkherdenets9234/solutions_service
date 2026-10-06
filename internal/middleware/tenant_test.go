package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/eandstravel/digitalservice/internal/tenantresolve"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

// These pin the tenant gate's contract. X-API-Key is resolved through
// tenantcore only; this service's own tenants collection is never consulted.

const (
	tkKey  = "test-key-1"
	tkSvc  = "svc-test"
	tkHost = "acme.example"
)

var tkTenantID = primitive.NewObjectID()

// gateStatus drives one request through the tenant gate and returns the
// recorder plus the tenant id the handler saw (zero when it was not reached).
func gateStatus(t *testing.T, r TenantResolver, hdr map[string]string) (*httptest.ResponseRecorder, primitive.ObjectID, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(ErrorHandler(zap.NewNop(), false))
	var seen primitive.ObjectID
	reached := false
	e.GET("/x", NewTenantMiddleware(r).Require(), func(c *gin.Context) {
		reached = true
		if v, ok := c.Get(CtxTenantID); ok {
			seen, _ = v.(primitive.ObjectID)
		}
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w, seen, reached
}

// fakeTC is an httptest stand-in for tenantcore's resolve route.
type fakeTC struct {
	srv    *httptest.Server
	mu     sync.Mutex
	mode   string // ok | suspended | unknown | down
	domain string
}

func newFakeTC(t *testing.T, mode, domain string) *fakeTC {
	t.Helper()
	f := &fakeTC{mode: mode, domain: domain}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		mode, domain := f.mode, f.domain
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch mode {
		case "unknown":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"error":{"code":"UNAUTHORIZED","message":"unauthorized","domain":"TENANT"}}`))
		case "down":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			status := "active"
			if mode == "suspended" {
				status = "suspended"
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"tenant_id":"` + tkTenantID.Hex() +
				`","slug":"acme","name":"Acme","status":"` + status + `","domain":"` + domain + `"}}`))
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTC) resolver(t *testing.T) TenantResolver {
	t.Helper()
	c := tenantresolve.NewClient(tenantresolve.ClientConfig{BaseURL: f.srv.URL, ServiceKey: tkSvc})
	if c == nil {
		t.Fatal("client not built")
	}
	t.Cleanup(c.Close)
	return NewTenantcoreResolver(c)
}

func errBody(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Domain  string `json:"domain"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return env.Error.Code + "|" + env.Error.Domain + "|" + env.Error.Message
}

func TestTenantMiddleware_TenantcoreResolver_UnknownKeyBody(t *testing.T) {
	hdr := map[string]string{"X-API-Key": tkKey}
	remote, _, reached := gateStatus(t, newFakeTC(t, "unknown", "").resolver(t), hdr)
	if remote.Code != http.StatusUnauthorized || reached {
		t.Fatalf("got %d reached=%v", remote.Code, reached)
	}
	// The public body is the same generic 401 whatever the cause; the specific
	// cause (WithDetail) is development-only and must not appear here.
	if got := errBody(t, remote); got != "UNAUTHORIZED|AUTH|unauthorized" {
		t.Fatalf("body = %q, want the generic unauthorized", got)
	}
	if strings.Contains(remote.Body.String(), "tenantcore does not recognise") {
		t.Fatalf("the dev-only detail leaked into a non-dev body: %s", remote.Body.String())
	}
}

func TestTenantMiddleware_TenantcoreResolver_SuspendedIs403(t *testing.T) {
	w, _, reached := gateStatus(t, newFakeTC(t, "suspended", "").resolver(t), map[string]string{"X-API-Key": tkKey})
	if w.Code != http.StatusForbidden || reached {
		t.Fatalf("got %d reached=%v", w.Code, reached)
	}
	if got, want := errBody(t, w), "FORBIDDEN|TENANT|tenant suspended"; got != want {
		t.Fatalf("body %q, want %q", got, want)
	}
}

func TestTenantMiddleware_TenantcoreResolver_UnavailableIs503NotUnauthorized(t *testing.T) {
	w, _, reached := gateStatus(t, newFakeTC(t, "down", "").resolver(t), map[string]string{"X-API-Key": tkKey})
	if w.Code != http.StatusServiceUnavailable || reached {
		t.Fatalf("got %d reached=%v, want 503", w.Code, reached)
	}
}

func TestTenantMiddleware_TenantcoreResolver_DomainMismatchUsesIdentityDomain(t *testing.T) {
	r := newFakeTC(t, "ok", tkHost).resolver(t)
	w, _, reached := gateStatus(t, r, map[string]string{"X-API-Key": tkKey, "Origin": "https://evil.example"})
	if w.Code != http.StatusForbidden || reached {
		t.Fatalf("mismatch: got %d reached=%v", w.Code, reached)
	}
	w, id, reached := gateStatus(t, r, map[string]string{"X-API-Key": tkKey, "Origin": "https://" + tkHost})
	if w.Code != http.StatusNoContent || !reached || id != tkTenantID {
		t.Fatalf("match: got %d reached=%v id=%v", w.Code, reached, id)
	}
}

func TestTenantMiddleware_TenantcoreResolver_MissingHeaderStill401(t *testing.T) {
	w, _, reached := gateStatus(t, newFakeTC(t, "ok", "").resolver(t), nil)
	if w.Code != http.StatusUnauthorized || reached {
		t.Fatalf("got %d reached=%v", w.Code, reached)
	}
}

// tenantcore only lowercases and trims a domain; this service has always
// stripped scheme, path and port. The adapter must normalise with the same
// function, or a domain typed as "https://site.example/" would refuse every
// browser request.
func TestTenantMiddleware_TenantcoreResolver_NormalisesDomain(t *testing.T) {
	cases := map[string]string{
		"https://Site.Example/": "site.example",
		"site.example:8443":     "site.example",
		"site.example/path":     "site.example",
		"":                      "",
		"site.example":          "site.example",
	}
	for stored, want := range cases {
		ref, err := newFakeTC(t, "ok", stored).resolver(t).Resolve(context.Background(), tkKey)
		if err != nil {
			t.Fatalf("%q: %v", stored, err)
		}
		if ref.Domain != want {
			t.Errorf("stored %q: Domain = %q, want %q", stored, ref.Domain, want)
		}
	}
}

func TestTenantMiddleware_TenantcoreResolver_OriginPassesForUntidyDomain(t *testing.T) {
	r := newFakeTC(t, "ok", "https://Site.Example/").resolver(t)
	w, id, reached := gateStatus(t, r, map[string]string{"X-API-Key": tkKey, "Origin": "https://site.example"})
	if w.Code != http.StatusNoContent || !reached || id != tkTenantID {
		t.Fatalf("got %d reached=%v", w.Code, reached)
	}
	w, _, reached = gateStatus(t, r, map[string]string{"X-API-Key": tkKey, "Origin": "https://evil.example"})
	if w.Code != http.StatusForbidden || reached {
		t.Fatalf("other origin: got %d reached=%v", w.Code, reached)
	}
}
