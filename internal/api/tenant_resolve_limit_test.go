package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.uber.org/zap"
)

type countingResolver struct{ calls atomic.Int64 }

func (c *countingResolver) Resolve(context.Context, string) (middleware.TenantRef, error) {
	c.calls.Add(1)
	return middleware.TenantRef{}, apierr.Unauthorized("")
}

// The resolve limiter must run before the tenant gate. If it ran after, a
// flood of bad keys would be refused one by one by the resolver (and, in
// tenantcore mode, by tenantcore) and never counted, because a request the
// gate rejects never reaches a limiter behind it.
func TestTenantResolveLimiterRunsBeforeTheResolver(t *testing.T) {
	res := &countingResolver{}
	rl := middleware.NewRateLimiter()
	t.Cleanup(rl.Close)

	e := NewServer(Deps{
		Log: zap.NewNop(),
		Config: &config.Config{
			AppEnv:                     config.EnvTest,
			RateLimitEnabled:           true,
			RateLimitBurst:             1,
			AuthRatePerMinute:          10,
			LeadRatePerMinute:          20,
			TenantResolveRatePerMinute: 1,
		},
		RateLimiter: rl,
		TenantMW:    middleware.NewTenantMiddleware(res),
	}).Handler()

	var codes []int
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/destinations", nil)
		req.Header.Set("X-API-Key", "bad-key")
		req.RemoteAddr = "192.0.2.7:1234"
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		codes = append(codes, w.Code)
	}

	if codes[0] != http.StatusUnauthorized {
		t.Errorf("first request: got %d, want 401 (limiter has one token)", codes[0])
	}
	for i, c := range codes[1:] {
		if c != http.StatusTooManyRequests {
			t.Errorf("request %d: got %d, want 429", i+2, c)
		}
	}
	if got := res.calls.Load(); got != 1 {
		t.Errorf("resolver called %d times, want 1: refused requests must not reach it", got)
	}
}
