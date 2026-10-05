package bootstrap

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eandstravel/digitalservice/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Tenantcore mode without a usable link must stop startup, never install the
// local resolver (stale hashes would keep accepting re-issued or revoked keys).
func TestBuildTenantResolver_TenantcoreWithoutLinkFailsClosed(t *testing.T) {
	cfg := &config.Config{TenantResolver: config.TenantResolverTenantcore}
	r, c, err := buildTenantResolver(cfg, nil, zap.NewNop())
	if err == nil {
		t.Fatal("want an error, got a resolver")
	}
	if r != nil || c != nil {
		t.Errorf("no resolver or client may be returned on failure, got %v / %v", r, c)
	}
	for _, want := range []string{"TENANTCORE_URL", "TENANTCORE_SERVICE_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %s: %v", want, err)
		}
	}
}

func TestBuildTenantResolver_LocalIsTheDefault(t *testing.T) {
	for _, mode := range []string{"", config.TenantResolverLocal} {
		r, c, err := buildTenantResolver(&config.Config{TenantResolver: mode}, nil, zap.NewNop())
		if err != nil || r == nil || c != nil {
			t.Fatalf("mode %q: r=%v c=%v err=%v", mode, r, c, err)
		}
	}
}

func TestBuildTenantResolver_TenantcoreBuildsAClient(t *testing.T) {
	cfg := &config.Config{TenantResolver: config.TenantResolverTenantcore, TenantcoreURL: "http://127.0.0.1:1", TenantcoreServiceKey: "svc-test"}
	r, c, err := buildTenantResolver(cfg, nil, zap.NewNop())
	if err != nil || r == nil || c == nil {
		t.Fatalf("r=%v c=%v err=%v", r, c, err)
	}
	c.Close()
}

func TestWarnUntrustedProxies_OnlyInTenantcoreModeWithoutTheSetting(t *testing.T) {
	warned := func(cfg config.Config) bool {
		var buf bytes.Buffer
		core := zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
			zapcore.AddSync(&buf), zapcore.DebugLevel)
		warnUntrustedProxies(zap.New(core), &cfg)
		return strings.Contains(buf.String(), "TRUSTED_PROXIES") && strings.Contains(buf.String(), `"level":"warn"`)
	}
	if !warned(config.Config{TenantResolver: config.TenantResolverTenantcore}) {
		t.Error("tenantcore mode without TRUSTED_PROXIES must warn")
	}
	if warned(config.Config{TenantResolver: config.TenantResolverTenantcore, TrustedProxies: []string{"10.0.0.0/8"}}) {
		t.Error("must not warn when TRUSTED_PROXIES is set")
	}
	if warned(config.Config{}) || warned(config.Config{TenantResolver: config.TenantResolverLocal}) {
		t.Error("local mode must not warn")
	}
}
