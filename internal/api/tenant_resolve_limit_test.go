package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/internal/tenantresolve"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/gin-gonic/gin"
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
			TenantResolveBurst:         1,
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

func floodEngine(t *testing.T, res *countingResolver, cfg config.Config) *gin.Engine {
	t.Helper()
	rl := middleware.NewRateLimiter()
	t.Cleanup(rl.Close)
	cfg.AppEnv = config.EnvTest
	cfg.RateLimitEnabled = true
	cfg.RateLimitBurst = 5
	cfg.AuthRatePerMinute = 10
	cfg.LeadRatePerMinute = 20
	return NewServer(Deps{
		Log:         zap.NewNop(),
		Config:      &cfg,
		RateLimiter: rl,
		TenantMW:    middleware.NewTenantMiddleware(res),
	}).Handler()
}

func flood(e *gin.Engine, n int) map[int]int {
	codes := map[int]int{}
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/destinations", nil)
		req.Header.Set("X-API-Key", "bad-key")
		req.RemoteAddr = "192.0.2.9:1"
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		codes[w.Code]++
	}
	return codes
}

// With the shipped defaults (600/min, burst 120) a storefront host making 100
// rapid calls from one IP is never throttled.
func TestTenantResolveLimiter_DefaultsAdmitARealisticBurst(t *testing.T) {
	res := &countingResolver{}
	e := floodEngine(t, res, config.Config{
		TenantResolveRatePerMinute: 600,
		TenantResolveBurst:         120,
	})
	codes := flood(e, 100)
	if codes[http.StatusTooManyRequests] != 0 || res.calls.Load() != 100 {
		t.Fatalf("codes=%v resolver calls=%d, want 100 calls and no 429", codes, res.calls.Load())
	}
}

// Past the burst the limiter refuses BEFORE the resolver. Rate 1/min so no
// token refills during the test.
func TestTenantResolveLimiter_RefusesPastBurstBeforeResolver(t *testing.T) {
	res := &countingResolver{}
	e := floodEngine(t, res, config.Config{
		TenantResolveRatePerMinute: 1,
		TenantResolveBurst:         120,
	})
	codes := flood(e, 125)
	if res.calls.Load() != 120 || codes[http.StatusTooManyRequests] != 5 {
		t.Fatalf("codes=%v resolver calls=%d, want 120 calls and 5 refusals", codes, res.calls.Load())
	}
}

func readyz(t *testing.T, e *gin.Engine) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

// A tenantcore resolver client that cannot reach tenantcore is reported on
// /readyz by name.
func TestReadyzReportsDegradedTenantResolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := tenantresolve.NewClient(tenantresolve.ClientConfig{BaseURL: srv.URL, ServiceKey: "svc-test"})
	t.Cleanup(c.Close)
	if _, err := c.Resolve(context.Background(), "test-key-1"); err == nil {
		t.Fatal("expected the fake outage to fail the resolve")
	}

	gin.SetMode(gin.TestMode)
	deg := NewServer(Deps{
		Log:                 zap.NewNop(),
		Config:              &config.Config{AppEnv: config.EnvTest},
		TenantResolveClient: c,
	}).Handler()
	body := readyz(t, deg)
	block, ok := body["tenant_resolver"].(map[string]any)
	if body["degraded"] != true || !ok || block["stale"] != true || !strings.Contains(block["detail"].(string), "tenantcore") {
		t.Fatalf("degraded resolver not reported: %v", body)
	}
}
