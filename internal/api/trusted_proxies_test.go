package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func clientIPEngine(t *testing.T, trusted []string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	applyTrustedProxies(e, &config.Config{TrustedProxies: trusted}, zap.NewNop())
	e.GET("/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })
	return e
}

func clientIP(e *gin.Engine, peer, xff string) string {
	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = peer
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w.Body.String()
}

// Unset: exactly gin's default, which trusts every peer. Pinned so local mode
// is provably unchanged.
func TestTrustedProxies_UnsetKeepsGinDefault(t *testing.T) {
	ref := gin.New() // untouched gin, the baseline
	ref.GET("/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })
	e := clientIPEngine(t, nil)
	for _, c := range [][2]string{{"192.0.2.7:1", "203.0.113.9"}, {"192.0.2.7:1", ""}, {"[2001:db8::1]:9", "198.51.100.4, 203.0.113.9"}} {
		if got, want := clientIP(e, c[0], c[1]), clientIP(ref, c[0], c[1]); got != want {
			t.Errorf("peer %s xff %q: got %q, untouched gin gives %q", c[0], c[1], got, want)
		}
	}
	if got := clientIP(e, "192.0.2.7:1", "203.0.113.9"); got != "203.0.113.9" {
		t.Errorf("default must still honour a spoofed XFF, got %q", got)
	}
}

func TestTrustedProxies_SetRestrictsTrust(t *testing.T) {
	e := clientIPEngine(t, []string{"10.0.0.0/8"})
	// Untrusted peer: the header is ignored.
	if got := clientIP(e, "192.0.2.7:1", "203.0.113.9"); got != "192.0.2.7" {
		t.Errorf("spoofed XFF from an untrusted peer: got %q, want the peer", got)
	}
	// Trusted proxy: the header is honoured.
	if got := clientIP(e, "10.1.2.3:1", "203.0.113.9"); got != "203.0.113.9" {
		t.Errorf("XFF from a trusted proxy: got %q", got)
	}
	// A single IP entry works too.
	e = clientIPEngine(t, []string{"192.0.2.7"})
	if got := clientIP(e, "192.0.2.7:1", "203.0.113.9"); got != "203.0.113.9" {
		t.Errorf("single-IP trust: got %q", got)
	}
}

// End to end: with TRUSTED_PROXIES set, a fresh random X-Forwarded-For per
// request no longer buys a fresh limiter bucket.
func TestTrustedProxies_SpoofedXFFDoesNotBypassResolveLimiter(t *testing.T) {
	run := func(trusted []string) int {
		res := &countingResolver{}
		e := floodEngine(t, res, config.Config{
			TenantResolver:             config.TenantResolverTenantcore,
			TenantResolveRatePerMinute: 1,
			TenantResolveBurst:         1,
			TrustedProxies:             trusted,
		})
		refused := 0
		for i := 0; i < 5; i++ {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/destinations", nil)
			req.Header.Set("X-API-Key", "bad-key")
			req.Header.Set("X-Forwarded-For", "203.0.113."+string(rune('1'+i)))
			req.RemoteAddr = "192.0.2.9:1"
			w := httptest.NewRecorder()
			e.ServeHTTP(w, req)
			if w.Code == http.StatusTooManyRequests {
				refused++
			}
		}
		return refused
	}
	if got := run(nil); got != 0 {
		t.Errorf("unset (today's behaviour): %d refused, want 0 (the bypass this setting exists to close)", got)
	}
	if got := run([]string{"10.0.0.0/8"}); got != 4 {
		t.Errorf("set: %d refused, want 4", got)
	}
}

func TestReadyz_ProxyDetailOnlyInTenantcoreModeWithoutTrustedProxies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mk := func(cfg config.Config) map[string]any {
		cfg.AppEnv = config.EnvTest
		return readyz(t, NewServer(Deps{Log: zap.NewNop(), Config: &cfg}).Handler())
	}

	body := mk(config.Config{TenantResolver: config.TenantResolverTenantcore})
	block, ok := body["tenant_resolver"].(map[string]any)
	if !ok {
		t.Fatalf("tenantcore mode without TRUSTED_PROXIES must carry a tenant_resolver block: %v", body)
	}
	detail, _ := block["detail"].(string)
	for _, want := range []string{"X-Forwarded-For", "TRUSTED_PROXIES"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail %q must mention %s", detail, want)
		}
	}
	// Other unset optional features already make a bare test config degraded;
	// the proxy note itself must add nothing to that.
	with := mk(config.Config{TenantResolver: config.TenantResolverTenantcore, TrustedProxies: []string{"10.0.0.0/8"}})
	if body["degraded"] != with["degraded"] {
		t.Errorf("the proxy note changed degraded: %v vs %v", body["degraded"], with["degraded"])
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v", body["status"])
	}

	if _, present := mk(config.Config{TenantResolver: config.TenantResolverTenantcore, TrustedProxies: []string{"10.0.0.0/8"}})["tenant_resolver"]; present {
		t.Error("with TRUSTED_PROXIES set there must be no tenant_resolver block")
	}
	if _, present := mk(config.Config{})["tenant_resolver"]; present {
		t.Error("local mode must not emit a tenant_resolver block")
	}
}
