package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/eandstravel/digitalservice/internal/entitlement"
	"github.com/eandstravel/digitalservice/internal/middleware"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

// The public application form must stay in the `lead` group: rate limited
// (it costs uploads and a row) and outside the subscription gate (a lapsed
// tenant still has applicants). The group is the only place both properties
// come from, so the test shows the route shares the lead limiter bucket with
// /contact and is not refused by a canceled subscription.
func TestGuideSubmitStaysInTheLeadGroup(t *testing.T) {
	canceled := entitlement.ProviderFunc(func(_ context.Context, id primitive.ObjectID) (entitlement.Entitlement, error) {
		return entitlement.Entitlement{
			TenantID: id, Status: entitlement.StatusCanceled, PeriodEnd: time.Now().Add(-time.Hour),
		}, nil
	})
	rl := middleware.NewRateLimiter()
	t.Cleanup(rl.Close)
	e := NewServer(Deps{
		Log: zap.NewNop(),
		Config: &config.Config{
			AppEnv: config.EnvTest, RateLimitEnabled: true, RateLimitBurst: 1,
			AuthRatePerMinute: 10, LeadRatePerMinute: 1,
		},
		RateLimiter:    rl,
		TenantMW:       middleware.NewTenantMiddleware(fakeResolver{}),
		SubscriptionMW: middleware.NewSubscriptionMiddleware(canceled),
	}).Handler()

	found := false
	for _, r := range e.Routes() {
		if r.Method == http.MethodPost && r.Path == "/api/v1/guide-applications" {
			found = true
		}
	}
	if !found {
		t.Fatal("POST /api/v1/guide-applications is not registered")
	}

	send := func(path string) int {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Header.Set("X-API-Key", "test-key")
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.9:1234"
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w.Code
	}

	// First request spends the lead bucket's only token. Under a canceled
	// subscription it must not be refused as 402 (no service is wired, so the
	// handler itself answers 503).
	if got := send("/api/v1/guide-applications"); got == http.StatusPaymentRequired || got == http.StatusTooManyRequests {
		t.Fatalf("first guide submit: got %d, want it past the limiter and the subscription gate", got)
	}
	// The sibling lead route shares that bucket, so it is now limited.
	if got := send("/api/v1/contact"); got != http.StatusTooManyRequests {
		t.Errorf("contact after the bucket is spent: got %d, want 429 (same lead group)", got)
	}
	// And the guide route itself is limited.
	if got := send("/api/v1/guide-applications"); got != http.StatusTooManyRequests {
		t.Errorf("second guide submit: got %d, want 429", got)
	}
}
