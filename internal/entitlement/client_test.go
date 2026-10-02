package entitlement

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// The client's whole job is to be right about failure. These exercise each
// branch against an httptest server, so the rules that decide whether a
// paying tenant can write are checked on the dev toolchain rather than only
// against a live tenantcore.

func newTestClient(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	c := NewClient(ClientConfig{
		BaseURL:     srv.URL,
		ServiceKey:  "svc-key",
		TTL:         50 * time.Millisecond,
		GraceWindow: time.Hour,
		Timeout:     2 * time.Second,
	})
	if c == nil {
		t.Fatal("NewClient returned nil for a configured link")
	}
	t.Cleanup(func() {
		c.Close()
		srv.Close()
	})
	return c, srv
}

func TestNewClientNilWhenUnconfigured(t *testing.T) {
	for _, cfg := range []ClientConfig{
		{},
		{BaseURL: "http://x"},
		{ServiceKey: "k"},
	} {
		if c := NewClient(cfg); c != nil {
			t.Fatalf("expected nil client for %+v", cfg)
		}
	}
	// Available must be safe on the nil receiver — bootstrap relies on it to
	// decide whether to substitute Unenforced.
	var c *Client
	if c.Available() {
		t.Fatal("nil client reported available")
	}
}

func TestClientFetchesAndCaches(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if got := r.Header.Get("X-Service-Key"); got != "svc-key" {
			t.Errorf("X-Service-Key = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"status":"active","modules":["travel"],"limits":{"staff":10}}}`))
	}))

	id := primitive.NewObjectID()
	ent, err := c.For(context.Background(), id)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if !ent.Active() || !ent.HasModule("travel") {
		t.Fatalf("unexpected entitlement: %+v", ent)
	}
	if v, ok := ent.Limit("staff"); !ok || v != 10 {
		t.Fatalf("limit staff = %d, ok = %v", v, ok)
	}

	// Second call inside the TTL must not hit the platform: this lookup is on
	// the request path of every write.
	if _, err := c.For(context.Background(), id); err != nil {
		t.Fatalf("For (cached): %v", err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("platform called %d times, want 1 (cache miss)", n)
	}
}

// tenantcore sends modules/limits/features as null, not [] / {}, for a tenant
// with no subscription. Null and empty must mean the same thing — the console
// learned this the hard way by crashing on .length.
func TestClientTreatsNullCollectionsAsUnenforced(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"status":"active","modules":null,"limits":null,"features":null}}`))
	}))

	ent, err := c.For(context.Background(), primitive.NewObjectID())
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if !ent.HasModule("anything") {
		t.Fatal("null modules must mean the gate is unenforced, not that nothing is allowed")
	}
	if _, ok := ent.Limit("staff"); ok {
		t.Fatal("null limits must mean no ceiling")
	}
	if ent.Feature("beta") {
		t.Fatal("null features must mean off")
	}
}

// A tenant tenantcore has never heard of is NOT a denial. digitalservice still
// owns its own tenants collection and the migration has not run everywhere, so
// during the cutover a tenant can legitimately exist here and not there.
// Reported as StatusUnknown, which the subscription gate lets through.
func TestClientTreatsUnknownTenantAsUnprovisioned(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"error":{"domain":"TENANT","code":"NOT_FOUND"}}`))
	}))

	ent, err := c.For(context.Background(), primitive.NewObjectID())
	if err != nil {
		t.Fatalf("a tenant unknown to the platform must not be an error, got %v", err)
	}
	if ent.Status != StatusUnknown {
		t.Fatalf("status = %q, want StatusUnknown", ent.Status)
	}
}

// Our own service key being rejected is a misconfigured deployment, not a
// statement about the tenant. It must surface as an error so the gate answers
// 500, and it must never be cached as an entitlement.
func TestClientRejectedServiceKeyIsAnError(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"error":{"domain":"GENERAL","code":"UNAUTHORIZED"}}`))
	}))

	if _, err := c.For(context.Background(), primitive.NewObjectID()); err == nil {
		t.Fatal("expected an error when the platform rejects our service key")
	}
	if degraded, _ := c.Degraded(); !degraded {
		t.Fatal("a rejected service key must show up on /readyz")
	}
}

// The core availability rule: once the platform goes down, a tenant we already
// know about keeps working from cache, flagged Stale — never refused.
func TestClientServesStaleWhenPlatformFails(t *testing.T) {
	var fail atomic.Bool
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"status":"active","modules":["travel"]}}`))
	}))

	id := primitive.NewObjectID()
	if _, err := c.For(context.Background(), id); err != nil {
		t.Fatalf("warm-up fetch: %v", err)
	}

	fail.Store(true)
	time.Sleep(60 * time.Millisecond) // outlive the TTL so the cache is consulted, not returned fresh

	ent, err := c.For(context.Background(), id)
	if err != nil {
		t.Fatalf("a known tenant must keep working through an outage, got %v", err)
	}
	if !ent.Stale {
		t.Fatal("a cached answer served during an outage must be flagged Stale")
	}
	if !ent.Active() {
		t.Fatal("the cached entitlement lost its state")
	}
	if degraded, since := c.Degraded(); !degraded || since == nil {
		t.Fatal("serving stale state must be reported on /readyz")
	}
}

// A tenant we have never successfully fetched, with the platform down, has no
// honest answer. It must be an error — which the gate turns into a 500 — and
// never a 402, which would tell a paying customer they had not paid.
func TestClientErrorsWhenNothingCached(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))

	if _, err := c.For(context.Background(), primitive.NewObjectID()); err == nil {
		t.Fatal("expected an error with the platform down and nothing cached")
	}
}

// Recovery clears the degraded flag, so /readyz stops alerting once the
// platform answers again.
func TestClientClearsDegradedOnRecovery(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"status":"active"}}`))
	}))

	id := primitive.NewObjectID()
	_, _ = c.For(context.Background(), id)
	if degraded, _ := c.Degraded(); !degraded {
		t.Fatal("expected degraded after a failure")
	}

	fail.Store(false)
	if _, err := c.For(context.Background(), id); err != nil {
		t.Fatalf("For after recovery: %v", err)
	}
	if degraded, _ := c.Degraded(); degraded {
		t.Fatal("degraded must clear once the platform answers again")
	}
}
