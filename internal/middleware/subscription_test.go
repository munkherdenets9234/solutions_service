package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/entitlement"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

// These assert the contract of the gate that moved off the local
// subscriptions collection and onto tenantcore. They use a fake Provider, so
// they run on the dev toolchain with no database and no platform — which is
// the point: these rules decide whether a paying tenant can write, and a rule
// that can only be exercised against a live stack is a rule that stops being
// exercised.

type fakeProvider struct {
	ent entitlement.Entitlement
	err error
}

func (f fakeProvider) For(context.Context, primitive.ObjectID) (entitlement.Entitlement, error) {
	return f.ent, f.err
}

// run drives one request through TenantMiddleware's contract (a tenant id
// already in the context) plus the subscription gate, and reports the status
// the caller would see.
func run(t *testing.T, p entitlement.Provider, method string, withTenant bool) int {
	t.Helper()
	gin.SetMode(gin.TestMode)

	e := gin.New()
	e.Use(ErrorHandler(zap.NewNop(), false))
	e.Use(func(c *gin.Context) {
		if withTenant {
			c.Set(CtxTenantID, primitive.NewObjectID())
		}
		c.Next()
	})
	e.Use(NewSubscriptionMiddleware(p).Require())
	e.Handle(method, "/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(method, "/x", nil))
	return rec.Code
}

func TestSubscriptionGate(t *testing.T) {
	active := entitlement.Entitlement{
		Status:    entitlement.StatusActive,
		PeriodEnd: time.Now().Add(24 * time.Hour),
	}

	tests := []struct {
		name     string
		provider entitlement.Provider
		method   string
		want     int
	}{
		{
			// The rule that predates the move and had to survive it: a
			// tenant nobody has provisioned yet is not held to any
			// subscription state.
			name:     "no subscription record passes",
			provider: fakeProvider{ent: entitlement.Entitlement{Status: entitlement.StatusUnknown}},
			method:   http.MethodPost,
			want:     http.StatusNoContent,
		},
		{
			name:     "active passes",
			provider: fakeProvider{ent: active},
			method:   http.MethodPost,
			want:     http.StatusNoContent,
		},
		{
			name: "trialing passes",
			provider: fakeProvider{ent: entitlement.Entitlement{
				Status: entitlement.StatusTrialing, PeriodEnd: time.Now().Add(time.Hour),
			}},
			method: http.MethodPost,
			want:   http.StatusNoContent,
		},
		{
			// CHANGED BY THE MOVE, deliberately. The old local check was
			// `time.Now().Before(CurrentPeriodEnd)`, so a zero period end
			// blocked every write. entitlement.Active treats a zero period
			// as "no billing period is being tracked" — an internal or
			// comped account — and does not retroactively expire it.
			name: "active with no period end passes",
			provider: fakeProvider{ent: entitlement.Entitlement{
				Status: entitlement.StatusActive,
			}},
			method: http.MethodPost,
			want:   http.StatusNoContent,
		},
		{
			name: "expired is refused",
			provider: fakeProvider{ent: entitlement.Entitlement{
				Status: entitlement.StatusActive, PeriodEnd: time.Now().Add(-time.Hour),
			}},
			method: http.MethodPost,
			want:   http.StatusPaymentRequired,
		},
		{
			name:     "canceled is refused",
			provider: fakeProvider{ent: entitlement.Entitlement{Status: entitlement.StatusCanceled}},
			method:   http.MethodPost,
			want:     http.StatusPaymentRequired,
		},
		{
			name:     "past due is refused",
			provider: fakeProvider{ent: entitlement.Entitlement{Status: entitlement.StatusPastDue}},
			method:   http.MethodPost,
			want:     http.StatusPaymentRequired,
		},
		{
			// The most important one. A failed lookup means "we could not
			// find out" and must never be rendered as "you have not paid":
			// a 402 here would tell every paying tenant they were in arrears
			// the moment tenantcore blipped.
			name:     "lookup failure is never 402",
			provider: fakeProvider{err: errors.New("platform unreachable")},
			method:   http.MethodPost,
			want:     http.StatusInternalServerError,
		},
		{
			// Reads stay up for a lapsed tenant so their storefront keeps
			// serving customers while billing is sorted out.
			name:     "GET passes even when canceled",
			provider: fakeProvider{ent: entitlement.Entitlement{Status: entitlement.StatusCanceled}},
			method:   http.MethodGet,
			want:     http.StatusNoContent,
		},
		{
			name:     "DELETE is gated",
			provider: fakeProvider{ent: entitlement.Entitlement{Status: entitlement.StatusCanceled}},
			method:   http.MethodDelete,
			want:     http.StatusPaymentRequired,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := run(t, tc.provider, tc.method, true); got != tc.want {
				t.Fatalf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

// Mounting the gate without TenantMiddleware ahead of it is a wiring bug. It
// must surface as a 500 naming the cause, never as a silent pass — a gate
// that quietly lets everything through when misrouted is worse than one that
// fails.
func TestSubscriptionGateWithoutTenantMiddleware(t *testing.T) {
	p := fakeProvider{ent: entitlement.Entitlement{Status: entitlement.StatusCanceled}}
	if got := run(t, p, http.MethodPost, false); got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", got, http.StatusInternalServerError)
	}
}

// Unenforced is what an unconfigured deployment runs on. It must behave
// exactly as an unprovisioned tenant did before the move: writes pass, and
// module gates are not enforced either.
func TestUnenforcedProviderLetsWritesThrough(t *testing.T) {
	if got := run(t, entitlement.Unenforced{}, http.MethodPost, true); got != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", got, http.StatusNoContent)
	}

	ent, err := entitlement.Unenforced{}.For(context.Background(), primitive.NewObjectID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ent.HasModule("carwash") {
		t.Fatal("Unenforced must not enforce module gates")
	}
}
