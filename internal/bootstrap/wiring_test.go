package bootstrap

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eandstravel/digitalservice/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Without a usable tenantcore link startup must stop. There is no fallback to
// this service's own tenants collection: its stale hashes would keep accepting
// re-issued or revoked keys.
func TestBuildTenantResolver_WithoutLinkFailsClosed(t *testing.T) {
	r, c, err := buildTenantResolver(&config.Config{}, zap.NewNop())
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

func TestBuildTenantResolver_BuildsAClient(t *testing.T) {
	cfg := &config.Config{TenantcoreURL: "http://127.0.0.1:1", TenantcoreServiceKey: "svc-test"}
	r, c, err := buildTenantResolver(cfg, zap.NewNop())
	if err != nil || r == nil || c == nil {
		t.Fatalf("r=%v c=%v err=%v", r, c, err)
	}
	c.Close()
}

func TestWarnUntrustedProxies_OnlyWithoutTheSetting(t *testing.T) {
	warned := func(cfg config.Config) bool {
		var buf bytes.Buffer
		core := zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
			zapcore.AddSync(&buf), zapcore.DebugLevel)
		warnUntrustedProxies(zap.New(core), &cfg)
		return strings.Contains(buf.String(), "TRUSTED_PROXIES") && strings.Contains(buf.String(), `"level":"warn"`)
	}
	if !warned(config.Config{}) {
		t.Error("without TRUSTED_PROXIES must warn")
	}
	if warned(config.Config{TrustedProxies: []string{"10.0.0.0/8"}}) {
		t.Error("must not warn when TRUSTED_PROXIES is set")
	}
}
