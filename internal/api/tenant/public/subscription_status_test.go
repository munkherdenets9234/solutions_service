package public

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/entitlement"
	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

// keyResolver accepts any non-empty key, like the guard tests' fake, and
// refuses a missing one as the real resolver does.
type keyResolver struct{ id primitive.ObjectID }

func (r keyResolver) Resolve(_ context.Context, raw string) (middleware.TenantRef, error) {
	if raw == "" {
		return middleware.TenantRef{}, apierr.Unauthorized("")
	}
	return middleware.TenantRef{ID: r.id}, nil
}

func entProvider(st entitlement.Status, periodEnd time.Time) entitlement.Provider {
	return entitlement.ProviderFunc(func(_ context.Context, id primitive.ObjectID) (entitlement.Entitlement, error) {
		return entitlement.Entitlement{TenantID: id, Status: st, PeriodEnd: periodEnd}, nil
	})
}

// statusEngine mounts the real public Register behind the real tenant key gate
// and the real subscription gate, the way tenant.Register does.
func statusEngine(p entitlement.Provider, withService bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(middleware.ErrorHandler(zap.NewNop(), false))
	base := e.Group("/api/v1", middleware.NewTenantMiddleware(keyResolver{id: primitive.NewObjectID()}).Require())
	pass := func(c *gin.Context) { c.Next() }
	d := Deps{
		Subscription:  middleware.NewSubscriptionMiddleware(p).Require(),
		AuthRateLimit: pass,
		LeadRateLimit: pass,
	}
	if withService {
		d.SubscriptionStatus = service.NewSubscriptionStatusService(p, zap.NewNop())
	}
	Register(base, d)
	return e
}

func statusReq(e *gin.Engine, method, path string, key bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(`{}`))
	if key {
		r.Header.Set("X-API-Key", "test-key")
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	return w
}

const statusPath = "/api/v1/subscription-status"

func TestStatusReturnsOnlyTheState(t *testing.T) {
	e := statusEngine(entProvider(entitlement.StatusActive, time.Time{}), true)
	w := statusReq(e, http.MethodGet, statusPath, true)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data) != 1 || env.Data["state"] != "active" {
		t.Fatalf("data = %v, want exactly {state: active}", env.Data)
	}
}

func TestStatusExpiredForLapsedTenantStill200(t *testing.T) {
	e := statusEngine(entProvider(entitlement.StatusCanceled, time.Now().Add(-time.Hour)), true)

	w := statusReq(e, http.MethodGet, statusPath, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status route got %d, want 200 outside the gate: %s", w.Code, w.Body.String())
	}
	var env struct {
		Data struct {
			State string `json:"state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.State != "expired" {
		t.Fatalf("state = %q, want expired", env.Data.State)
	}

	// Proof the real gate is mounted and lapsed: a gated write is refused.
	if got := statusReq(e, http.MethodPost, "/api/v1/reviews", true).Code; got != http.StatusPaymentRequired {
		t.Fatalf("gated write got %d, want 402", got)
	}
}

func TestStatusSetsPrivateCacheHeader(t *testing.T) {
	e := statusEngine(entProvider(entitlement.StatusActive, time.Time{}), true)
	w := statusReq(e, http.MethodGet, statusPath, true)
	if got := w.Header().Get("Cache-Control"); got != "private, max-age=60" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestStatusRequiresAPIKey(t *testing.T) {
	e := statusEngine(entProvider(entitlement.StatusActive, time.Time{}), true)
	w := statusReq(e, http.MethodGet, statusPath, false)
	other := statusReq(e, http.MethodGet, "/api/v1/destinations", false)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", w.Code)
	}
	if w.Code != other.Code || w.Body.String() != other.Body.String() {
		t.Fatalf("refusal differs from another tenant route:\n%s\nvs\n%s", w.Body.String(), other.Body.String())
	}
}

func TestStatusIsGetOnly(t *testing.T) {
	e := statusEngine(entProvider(entitlement.StatusActive, time.Time{}), true)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if got := statusReq(e, m, statusPath, true).Code; got != http.StatusNotFound && got != http.StatusMethodNotAllowed {
			t.Errorf("%s got %d, want 404/405", m, got)
		}
	}
}

func TestStatusWithNilServiceIsActive(t *testing.T) {
	e := statusEngine(entProvider(entitlement.StatusCanceled, time.Time{}), false)
	w := statusReq(e, http.MethodGet, statusPath, true)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if len(env.Data) != 1 || env.Data["state"] != "active" {
		t.Fatalf("data = %v, want {state: active}", env.Data)
	}
}
