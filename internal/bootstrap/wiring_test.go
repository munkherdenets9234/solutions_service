package bootstrap

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/eandstravel/digitalservice/internal/notify"
	"github.com/eandstravel/digitalservice/internal/tenantresolve"
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

// With TENANTCORE_* unset there is no mail client, so the notifier and worker
// are not built: nil, not a typed-nil that a service would mistake for "on".
func TestNotifierIsNilSafeWhenTenantcoreUnset(t *testing.T) {
	n, w := buildRequestMail(&config.Config{}, repos{}, notify.NewClient(notify.Config{}), nil, zap.NewNop())
	if n != nil || w != nil {
		t.Fatalf("notifier=%v worker=%v, want both nil", n, w)
	}
	// Linked but none of the three mail variables set: still off (opt-in).
	cfg := &config.Config{TenantcoreURL: "http://127.0.0.1:1", TenantcoreServiceKey: "svc-test"}
	cl := notify.NewClient(notify.Config{BaseURL: cfg.TenantcoreURL, ServiceKey: cfg.TenantcoreServiceKey})
	n, w = buildRequestMail(cfg, repos{}, cl, nil, zap.NewNop())
	if n != nil || w != nil {
		t.Fatalf("without the mail variables notifier=%v worker=%v, want both nil", n, w)
	}
	// A nil notifier is safe to call, and services left unwired stay off.
	n.Notify(context.Background(), [12]byte{}, "booking", "x", "y")

	svcs := services{}
	wireNotifier(&svcs, nil)
	// Wiring nil must not panic and must not set anything.
}

func TestBuildRequestMail_BuildsBothWhenConfigured(t *testing.T) {
	cfg := &config.Config{
		TenantcoreURL: "http://127.0.0.1:1", TenantcoreServiceKey: "svc-test",
		MailUnsubscribeKey: strings.Repeat("k", 32),
		PublicBaseURL:      "https://api.example.com",
		AdminBaseURL:       "https://admin.example.com",
	}
	cl := notify.NewClient(notify.Config{BaseURL: cfg.TenantcoreURL, ServiceKey: cfg.TenantcoreServiceKey})
	rc := tenantresolve.NewClient(tenantresolve.ClientConfig{BaseURL: cfg.TenantcoreURL, ServiceKey: cfg.TenantcoreServiceKey})
	defer rc.Close()
	n, w := buildRequestMail(cfg, repos{}, cl, rc, zap.NewNop())
	if n == nil || w == nil {
		t.Fatalf("notifier=%v worker=%v, want both built", n, w)
	}
}

// The config's predicate decides: fully configured mail but no mail client
// (tenantcore unreachable by config) still builds nothing.
func TestBuildRequestMail_NoClientMeansOff(t *testing.T) {
	cfg := &config.Config{
		TenantcoreURL: "http://127.0.0.1:1", TenantcoreServiceKey: "svc-test",
		MailUnsubscribeKey: strings.Repeat("k", 32),
		PublicBaseURL:      "https://api.example.com",
		AdminBaseURL:       "https://admin.example.com",
	}
	if n, w := buildRequestMail(cfg, repos{}, notify.NewClient(notify.Config{}), nil, zap.NewNop()); n != nil || w != nil {
		t.Fatalf("no client: notifier=%v worker=%v, want both nil", n, w)
	}
}

type fakeRunner struct {
	started chan struct{}
	stopped chan struct{}
	hang    bool
}

func (f *fakeRunner) Run(ctx context.Context) {
	close(f.started)
	if f.hang {
		<-f.stopped // never closed: ignores ctx, like a stuck send
		return
	}
	<-ctx.Done()
	close(f.stopped)
}

func TestWorkerStartedAndStoppedWithApp(t *testing.T) {
	r := &fakeRunner{started: make(chan struct{}), stopped: make(chan struct{})}
	app := &App{Log: zap.NewNop()}
	app.startMailWorker(r)
	select {
	case <-r.started:
	case <-time.After(2 * time.Second):
		t.Fatal("worker was not started")
	}
	select {
	case <-r.stopped:
		t.Fatal("worker stopped before Close")
	default:
	}
	done := make(chan struct{})
	go func() { app.Close(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return")
	}
	select {
	case <-r.stopped:
	default:
		t.Fatal("Close returned before the worker's Run returned")
	}
}

func TestCloseGivesUpOnAStuckWorker(t *testing.T) {
	old := mailStopTimeout
	mailStopTimeout = 50 * time.Millisecond
	defer func() { mailStopTimeout = old }()
	r := &fakeRunner{started: make(chan struct{}), stopped: make(chan struct{}), hang: true}
	app := &App{Log: zap.NewNop()}
	app.startMailWorker(r)
	<-r.started
	done := make(chan struct{})
	go func() { app.Close(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close must stop waiting after the bounded timeout")
	}
	close(r.stopped) // let the goroutine finish
}

func TestCloseWithoutWorkerIsFine(t *testing.T) {
	(&App{Log: zap.NewNop()}).Close(context.Background())
}
