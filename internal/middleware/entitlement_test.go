package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/entitlement"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

var testTenant = primitive.NewObjectID()

// gatedEngine mounts the production pairing: a tenant resolved upstream, the
// gate, and the one renderer. ErrorHandler is present because the gate never
// writes a response itself — without it an aborted request falls through to
// gin's default 200, which is the trap the rate limiter tests already found.
func gatedEngine(t *testing.T, gate gin.HandlerFunc, withTenant bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	e := gin.New()
	e.Use(ErrorHandler(zap.NewNop(), false))
	if withTenant {
		e.Use(func(c *gin.Context) { c.Set(CtxTenantID, testTenant); c.Next() })
	}
	e.GET("/washes", gate, func(c *gin.Context) {
		ent, ok := EntitlementFrom(c)
		c.JSON(http.StatusOK, gin.H{"reached": true, "entitlement_in_context": ok, "status": ent.Status})
	})
	return e
}

func get(e *gin.Engine) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/washes", nil))
	return w
}

func staticProvider(ent entitlement.Entitlement) entitlement.Provider {
	return entitlement.ProviderFunc(func(context.Context, primitive.ObjectID) (entitlement.Entitlement, error) {
		return ent, nil
	})
}

func failingProvider(err error) entitlement.Provider {
	return entitlement.ProviderFunc(func(context.Context, primitive.ObjectID) (entitlement.Entitlement, error) {
		return entitlement.Entitlement{}, err
	})
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (body %s)", err, w.Body.String())
	}
	return env.Error.Code
}

func entitled(modules ...string) entitlement.Entitlement {
	return entitlement.Entitlement{
		TenantID:  testTenant,
		Status:    entitlement.StatusActive,
		PeriodEnd: time.Now().Add(24 * time.Hour),
		Modules:   modules,
	}
}

func TestRequireModuleAllowsAnEntitledTenant(t *testing.T) {
	e := gatedEngine(t, RequireModule(staticProvider(entitled("travel", "carwash")), "carwash"), true)

	w := get(e)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 — an entitled tenant was refused\nbody: %s", w.Code, w.Body.String())
	}
}

func TestRequireModuleRefusesAnUnentitledTenant(t *testing.T) {
	e := gatedEngine(t, RequireModule(staticProvider(entitled("travel")), "carwash"), true)

	w := get(e)
	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("got %d, want 402 — the caller is one plan change away, not permanently barred", w.Code)
	}
	if code := errCode(t, w); code != apierr.CodeModuleNotEntitled {
		t.Errorf("code = %q, want %q", code, apierr.CodeModuleNotEntitled)
	}
}

// The gate hands the fetched entitlement down so a service-layer limit check
// on the same request costs no second lookup.
func TestRequireModulePassesEntitlementDown(t *testing.T) {
	e := gatedEngine(t, RequireModule(staticProvider(entitled("carwash")), "carwash"), true)

	w := get(e)
	var body struct {
		InContext bool   `json:"entitlement_in_context"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.InContext {
		t.Error("the handler could not read the entitlement the gate already fetched")
	}
	if body.Status != string(entitlement.StatusActive) {
		t.Errorf("status in context = %q, want %q", body.Status, entitlement.StatusActive)
	}
}

// The most important case. A lookup failure means "we could not find out",
// never "no". Converting it into a 402 would make the entitlement source a
// single point of failure for every product at once — worse than the monolith
// the split replaces — and would tell the tenant they had not paid when in
// fact we had not asked.
func TestRequireModuleDoesNotTurnALookupFailureIntoADenial(t *testing.T) {
	e := gatedEngine(t, RequireModule(failingProvider(apierr.Internal(errors.New("platform unreachable"))), "carwash"), true)

	w := get(e)
	if w.Code == http.StatusPaymentRequired {
		t.Fatal("a failed lookup was reported as unpaid — this is the failure mode the design exists to avoid")
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", w.Code)
	}
	if code := errCode(t, w); code != apierr.CodeInternal {
		t.Errorf("code = %q, want %q", code, apierr.CodeInternal)
	}
	if body := w.Body.String(); strings.Contains(body, "platform unreachable") {
		t.Errorf("the underlying cause leaked to the client: %s", body)
	}
}

// Mounting a gate without TenantMiddleware ahead of it is a routing bug. It
// must say so in the log rather than produce a bare 500 nobody can trace, and
// it must not be mistaken for the tenant being unentitled.
func TestRequireModuleReportsMissingTenantMiddleware(t *testing.T) {
	e := gatedEngine(t, RequireModule(staticProvider(entitled("carwash")), "carwash"), false)

	w := get(e)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500 — a missing tenant is our wiring bug, not the caller's problem", w.Code)
	}
}

func TestRequireFeature(t *testing.T) {
	granted := entitlement.Entitlement{
		TenantID: testTenant,
		Status:   entitlement.StatusActive,
		Features: map[string]bool{"custom_domain": true},
	}

	e := gatedEngine(t, RequireFeature(staticProvider(granted), "custom_domain"), true)
	if w := get(e); w.Code != http.StatusOK {
		t.Errorf("granted feature: got %d, want 200", w.Code)
	}

	e = gatedEngine(t, RequireFeature(staticProvider(granted), "sso"), true)
	w := get(e)
	if w.Code != http.StatusPaymentRequired {
		t.Errorf("ungranted feature: got %d, want 402", w.Code)
	}
	if code := errCode(t, w); code != apierr.CodeModuleNotEntitled {
		t.Errorf("code = %q, want %q", code, apierr.CodeModuleNotEntitled)
	}
}
