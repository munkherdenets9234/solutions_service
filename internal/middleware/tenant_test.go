package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/tenantresolve"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

// These pin the tenant gate's contract for BOTH resolvers. The local one is
// the default and must behave exactly as it did before the resolver became
// swappable; the tenantcore one must be indistinguishable to a caller except
// where tenantcore itself cannot be reached.

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

func localFn(t *models.Tenant, err error) TenantResolver {
	return localResolver{lookup: fakeLookup{t: t, err: err}}
}

func TestTenantMiddleware_LocalResolverBehaviourUnchanged(t *testing.T) {
	ok := &models.Tenant{ID: tkTenantID, Domain: tkHost}

	t.Run("unknown 401", func(t *testing.T) {
		w, _, reached := gateStatus(t, localFn(nil, apierr.Unauthorized("")), map[string]string{"X-API-Key": tkKey})
		if w.Code != http.StatusUnauthorized || reached {
			t.Fatalf("got %d reached=%v", w.Code, reached)
		}
	})
	t.Run("suspended 403", func(t *testing.T) {
		w, _, _ := gateStatus(t, localFn(nil, apierr.Forbidden("tenant suspended").In(apierr.DomainTenant)), map[string]string{"X-API-Key": tkKey})
		if w.Code != http.StatusForbidden {
			t.Fatalf("got %d", w.Code)
		}
	})
	t.Run("domain mismatch 403", func(t *testing.T) {
		w, _, reached := gateStatus(t, localFn(ok, nil), map[string]string{"X-API-Key": tkKey, "Origin": "https://evil.example"})
		if w.Code != http.StatusForbidden || reached {
			t.Fatalf("got %d reached=%v", w.Code, reached)
		}
	})
	t.Run("ok sets CtxTenantID", func(t *testing.T) {
		w, id, reached := gateStatus(t, localFn(ok, nil), map[string]string{"X-API-Key": tkKey, "Origin": "https://" + tkHost})
		if w.Code != http.StatusNoContent || !reached || id != tkTenantID {
			t.Fatalf("got %d reached=%v id=%v", w.Code, reached, id)
		}
	})
	t.Run("missing header 401", func(t *testing.T) {
		w, _, _ := gateStatus(t, localFn(ok, nil), nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("got %d", w.Code)
		}
	})
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
	local, _, _ := gateStatus(t, localFn(nil, apierr.Unauthorized("")), hdr)
	remote, _, reached := gateStatus(t, newFakeTC(t, "unknown", "").resolver(t), hdr)
	if remote.Code != http.StatusUnauthorized || reached {
		t.Fatalf("got %d reached=%v", remote.Code, reached)
	}
	if local.Body.String() != remote.Body.String() {
		t.Fatalf("bodies differ:\nlocal  %s\nremote %s", local.Body.String(), remote.Body.String())
	}
}

func TestTenantMiddleware_TenantcoreResolver_SuspendedIs403(t *testing.T) {
	w, _, reached := gateStatus(t, newFakeTC(t, "suspended", "").resolver(t), map[string]string{"X-API-Key": tkKey})
	if w.Code != http.StatusForbidden || reached {
		t.Fatalf("got %d reached=%v", w.Code, reached)
	}
	local, _, _ := gateStatus(t, localFn(nil, apierr.Forbidden("tenant suspended").In(apierr.DomainTenant)), map[string]string{"X-API-Key": tkKey})
	if got, want := errBody(t, w), errBody(t, local); got != want {
		t.Fatalf("body %q differs from the local resolver's %q", got, want)
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

type fakeLookup struct {
	t   *models.Tenant
	err error
}

func (f fakeLookup) Resolve(context.Context, string) (*models.Tenant, error) { return f.t, f.err }

// The local adapter must pass the service's taxonomy errors through untouched
// and map a tenant to exactly its ID and Domain.
func TestLocalResolverAdapter(t *testing.T) {
	ctx := context.Background()

	got, err := localResolver{lookup: fakeLookup{t: &models.Tenant{ID: tkTenantID, Domain: tkHost}}}.Resolve(ctx, tkKey)
	if err != nil || got.ID != tkTenantID || got.Domain != tkHost {
		t.Fatalf("active: got %+v err=%v", got, err)
	}

	_, err = localResolver{lookup: fakeLookup{err: apierr.Unauthorized("")}}.Resolve(ctx, tkKey)
	var ae *apierr.APIError
	if !errors.As(err, &ae) || ae.HTTPStatus != http.StatusUnauthorized || ae.Message != "unauthorized" {
		t.Fatalf("unknown: got %v", err)
	}

	_, err = localResolver{lookup: fakeLookup{err: apierr.Forbidden("tenant suspended").In(apierr.DomainTenant)}}.Resolve(ctx, tkKey)
	if !errors.As(err, &ae) || ae.HTTPStatus != http.StatusForbidden || ae.Domain != apierr.DomainTenant || ae.Message != "tenant suspended" {
		t.Fatalf("suspended: got %v", err)
	}
}

// NewLocalResolver must hand the real service to the adapter; a nil service is
// fine to construct with (the method value is only bound, never called here).
func TestNewLocalResolverWrapsTheService(t *testing.T) {
	r, ok := NewLocalResolver(nil).(localResolver)
	if !ok || r.lookup == nil {
		t.Fatalf("NewLocalResolver did not build a localResolver with a lookup: %#v", r)
	}
}
