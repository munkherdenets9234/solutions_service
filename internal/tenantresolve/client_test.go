package tenantresolve

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	testKey    = "test-key-1"
	testSvcKey = "svc-test"
)

var testTenantID = primitive.NewObjectID()

// fakeTenantcore is an httptest stand-in for the resolve route.
type fakeTenantcore struct {
	srv    *httptest.Server
	calls  atomic.Int64
	mu     sync.Mutex
	mode   string // ok | suspended | down | 500 | garbage | unknown | svcrejected
	gotSvc string
	gotKey string
}

func newFake(t *testing.T) *fakeTenantcore {
	t.Helper()
	f := &fakeTenantcore{mode: "ok"}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.mu.Lock()
		mode := f.mode
		f.gotSvc = r.Header.Get("X-Service-Key")
		f.gotKey = r.Header.Get("X-Tenant-Key")
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch mode {
		case "ok", "suspended":
			status := "active"
			if mode == "suspended" {
				status = "suspended"
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"tenant_id":"` + testTenantID.Hex() +
				`","slug":"acme","name":"Acme","status":"` + status + `","domain":"acme.example","hosts":["a.example","b.example"]}}`))
		case "unknown":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"error":{"code":"UNAUTHORIZED","message":"unauthorized","domain":"TENANT"}}`))
		case "svcrejected":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"error":{"code":"UNAUTHORIZED","message":"bad service key","domain":"SERVICE"}}`))
		case "500":
			w.WriteHeader(http.StatusInternalServerError)
		case "garbage":
			_, _ = w.Write([]byte(`<html>not json`))
		case "down":
			// Drop the connection without a response.
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTenantcore) setMode(m string) {
	f.mu.Lock()
	f.mode = m
	f.mu.Unlock()
}

// clock is a controllable time source.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestClient(t *testing.T, f *fakeTenantcore, log *zap.Logger) (*Client, *clock) {
	t.Helper()
	c := NewClient(ClientConfig{BaseURL: f.srv.URL, ServiceKey: testSvcKey, Log: log})
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	t.Cleanup(c.Close)
	clk := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	c.now = clk.now
	return c, clk
}

func TestNewClient_NilWhenUnconfigured(t *testing.T) {
	if c := NewClient(ClientConfig{BaseURL: "", ServiceKey: "x"}); c != nil || c.Available() {
		t.Fatal("expected nil client without BaseURL")
	}
	if c := NewClient(ClientConfig{BaseURL: "http://x", ServiceKey: ""}); c != nil {
		t.Fatal("expected nil client without ServiceKey")
	}
}

func TestResolve_FreshHitMakesOneCall(t *testing.T) {
	f := newFake(t)
	c, _ := newTestClient(t, f, nil)

	for i := 0; i < 3; i++ {
		id, err := c.Resolve(context.Background(), testKey)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if id.TenantID != testTenantID || id.Slug != "acme" || id.Name != "Acme" || id.Domain != "acme.example" ||
			len(id.Hosts) != 2 || id.Suspended || id.Stale {
			t.Fatalf("unexpected identity: %+v", id)
		}
	}
	if n := f.calls.Load(); n != 1 {
		t.Fatalf("want 1 call, got %d", n)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gotSvc != testSvcKey || f.gotKey != testKey {
		t.Fatal("headers not forwarded")
	}
}

func TestResolve_RefetchesAfterTTL(t *testing.T) {
	f := newFake(t)
	c, clk := newTestClient(t, f, nil)

	_, _ = c.Resolve(context.Background(), testKey)
	clk.advance(DefaultTTL + time.Second)
	_, _ = c.Resolve(context.Background(), testKey)
	if n := f.calls.Load(); n != 2 {
		t.Fatalf("want 2 calls, got %d", n)
	}
}

func TestResolve_ServesStaleWhenTenantcoreDown_FlagsStale(t *testing.T) {
	f := newFake(t)
	c, clk := newTestClient(t, f, nil)

	if _, err := c.Resolve(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	f.setMode("down")
	clk.advance(DefaultTTL + time.Minute)

	id, err := c.Resolve(context.Background(), testKey)
	if err != nil {
		t.Fatalf("want stale answer, got %v", err)
	}
	if !id.Stale || id.TenantID != testTenantID {
		t.Fatalf("want stale identity, got %+v", id)
	}
	if deg, since := c.Degraded(); !deg || since == nil {
		t.Fatal("want degraded")
	}

	// Recovery clears degraded and the stale flag.
	f.setMode("ok")
	clk.advance(DefaultTTL + time.Minute)
	id, err = c.Resolve(context.Background(), testKey)
	if err != nil || id.Stale {
		t.Fatalf("want fresh answer, got %+v %v", id, err)
	}
	if deg, _ := c.Degraded(); deg {
		t.Fatal("degraded should clear after a success")
	}
}

func TestResolve_StaleExpiresAfterGraceWindow_ReturnsErrUnavailable(t *testing.T) {
	f := newFake(t)
	c, clk := newTestClient(t, f, nil)

	if _, err := c.Resolve(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	f.setMode("down")
	clk.advance(DefaultTTL + DefaultGraceWindow + time.Minute)

	_, err := c.Resolve(context.Background(), testKey)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestResolve_NoCacheAndDownIsErrUnavailable(t *testing.T) {
	f := newFake(t)
	f.setMode("down")
	c, _ := newTestClient(t, f, nil)

	_, err := c.Resolve(context.Background(), testKey)
	if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrUnknownKey) {
		t.Fatalf("want ErrUnavailable only, got %v", err)
	}
	if deg, _ := c.Degraded(); !deg {
		t.Fatal("want degraded")
	}
}

func TestResolve_UnknownKeyIsErrUnknownKeyAndNegativeCached(t *testing.T) {
	f := newFake(t)
	f.setMode("unknown")
	c, clk := newTestClient(t, f, nil)

	for i := 0; i < 3; i++ {
		_, err := c.Resolve(context.Background(), "test-key-bad")
		if !errors.Is(err, ErrUnknownKey) {
			t.Fatalf("want ErrUnknownKey, got %v", err)
		}
	}
	if n := f.calls.Load(); n != 1 {
		t.Fatalf("negative cache should absorb repeats; got %d calls", n)
	}
	if deg, _ := c.Degraded(); deg {
		t.Fatal("an authoritative refusal is not degradation")
	}

	// After NegativeTTL the refusal is re-checked, so a re-issued key works.
	f.setMode("ok")
	clk.advance(DefaultNegativeTTL + time.Second)
	if _, err := c.Resolve(context.Background(), "test-key-bad"); err != nil {
		t.Fatalf("want success after negative TTL, got %v", err)
	}
}

func TestResolve_ServiceKeyRejectedIsNotUnknownKey(t *testing.T) {
	f := newFake(t)
	f.setMode("svcrejected")
	c, _ := newTestClient(t, f, nil)

	for i := 0; i < 2; i++ {
		_, err := c.Resolve(context.Background(), testKey)
		if err == nil || errors.Is(err, ErrUnknownKey) {
			t.Fatalf("service-key rejection must not be ErrUnknownKey, got %v", err)
		}
		if !strings.Contains(err.Error(), "TENANTCORE_SERVICE_KEY") {
			t.Fatalf("error should name TENANTCORE_SERVICE_KEY, got %v", err)
		}
	}
	if n := f.calls.Load(); n != 2 {
		t.Fatalf("misconfiguration must not be cached; got %d calls", n)
	}
}

func TestResolve_SuspendedIsReportedNotErrored(t *testing.T) {
	f := newFake(t)
	f.setMode("suspended")
	c, _ := newTestClient(t, f, nil)

	id, err := c.Resolve(context.Background(), testKey)
	if err != nil {
		t.Fatalf("suspended must not error: %v", err)
	}
	if !id.Suspended {
		t.Fatal("want Suspended")
	}
}

func TestResolve_ServerErrorFallsBackToStale(t *testing.T) {
	for _, mode := range []string{"500", "garbage"} {
		t.Run(mode, func(t *testing.T) {
			f := newFake(t)
			c, clk := newTestClient(t, f, nil)
			if _, err := c.Resolve(context.Background(), testKey); err != nil {
				t.Fatal(err)
			}
			f.setMode(mode)
			clk.advance(DefaultTTL + time.Second)
			id, err := c.Resolve(context.Background(), testKey)
			if err != nil || !id.Stale {
				t.Fatalf("want stale fallback, got %+v %v", id, err)
			}
		})
	}
}

func TestResolve_RawKeyNeverAppearsInCacheKeysOrLogs(t *testing.T) {
	const rawKey = "test-key-secretish"
	var buf bytes.Buffer
	core := zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&buf), zapcore.DebugLevel)

	f := newFake(t)
	c, clk := newTestClient(t, f, zap.New(core))

	if _, err := c.Resolve(context.Background(), rawKey); err != nil {
		t.Fatal(err)
	}
	// Force failure paths that log and that build errors.
	f.setMode("down")
	clk.advance(DefaultTTL + time.Second)
	_, _ = c.Resolve(context.Background(), rawKey)
	clk.advance(DefaultGraceWindow + time.Hour)
	_, err := c.Resolve(context.Background(), rawKey)
	if err == nil {
		t.Fatal("expected error")
	}
	f.setMode("svcrejected")
	_, err2 := c.Resolve(context.Background(), rawKey)

	c.mu.RLock()
	for k := range c.cache {
		if strings.Contains(k, rawKey) || k == rawKey {
			t.Fatal("raw key used as cache key")
		}
		if len(k) != 64 {
			t.Fatalf("cache key should be a hex sha256, got len %d", len(k))
		}
	}
	c.mu.RUnlock()

	if buf.Len() == 0 {
		t.Fatal("expected the outage to be logged")
	}
	if strings.Contains(buf.String(), rawKey) {
		t.Fatal("raw key appeared in logs")
	}
	if strings.Contains(err.Error(), rawKey) || (err2 != nil && strings.Contains(err2.Error(), rawKey)) {
		t.Fatal("raw key appeared in an error")
	}
}

func TestResolve_ConcurrentCallsAreRaceFree(t *testing.T) {
	f := newFake(t)
	c, _ := newTestClient(t, f, nil)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := c.Resolve(context.Background(), testKey)
			if err != nil || id.TenantID != testTenantID {
				t.Errorf("inconsistent result: %+v %v", id, err)
			}
			c.Degraded()
		}()
	}
	wg.Wait()
}
