package tenantresolve

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestIdentityFor_HitAfterResolve(t *testing.T) {
	f := newFake(t)
	c, _ := newTestClient(t, f, nil)
	if _, err := c.Resolve(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	got, ok := c.IdentityFor(testTenantID)
	if !ok {
		t.Fatal("want a hit after Resolve")
	}
	if got.TenantID != testTenantID || got.Name != "Acme" || len(got.Hosts) != 2 || got.Hosts[0] != "a.example" {
		t.Fatalf("unexpected identity: %+v", got)
	}
	// The returned slice is a copy: mutating it must not touch the cache.
	got.Hosts[0] = "evil.example"
	again, _ := c.IdentityFor(testTenantID)
	if again.Hosts[0] != "a.example" {
		t.Fatal("IdentityFor leaked the cached Hosts slice")
	}
	if n := f.calls.Load(); n != 1 {
		t.Fatalf("IdentityFor must not call tenantcore, calls=%d", n)
	}
}

func TestIdentityFor_MissForUnknownTenant(t *testing.T) {
	f := newFake(t)
	c, _ := newTestClient(t, f, nil)
	if _, err := c.Resolve(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.IdentityFor(primitive.NewObjectID()); ok {
		t.Fatal("want a miss for a tenant never resolved")
	}
}

func TestIdentityFor_NilReceiver(t *testing.T) {
	var c *Client
	if _, ok := c.IdentityFor(testTenantID); ok {
		t.Fatal("nil client must miss")
	}
}

func TestIdentityFor_RemovedWhenCacheEntrySwept(t *testing.T) {
	f := newFake(t)
	c, clk := newTestClient(t, f, nil)
	if _, err := c.Resolve(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	clk.advance(c.ttl + c.graceWindow + time.Second)
	c.sweep()
	if _, ok := c.IdentityFor(testTenantID); ok {
		t.Fatal("identity must go when its cache entry is swept")
	}
	if len(c.byTenant) != 0 {
		t.Fatalf("secondary index leaked %d entries", len(c.byTenant))
	}
}

func TestIdentityFor_RemovedWhenCacheEntryEvicted(t *testing.T) {
	f := newFake(t)
	c, clk := newTestClient(t, f, nil)
	c.maxEntries = 1
	if _, err := c.Resolve(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	f.setMode("unknown")
	clk.advance(time.Second)
	if _, err := c.Resolve(context.Background(), "test-key-other"); err == nil {
		t.Fatal("want unknown key")
	}
	if _, ok := c.IdentityFor(testTenantID); ok {
		t.Fatal("identity must go when its cache entry is evicted")
	}
	if len(c.byTenant) != 0 {
		t.Fatalf("secondary index leaked %d entries", len(c.byTenant))
	}
}

func TestIdentityFor_MissPastGraceEvenBeforeSweep(t *testing.T) {
	f := newFake(t)
	c, clk := newTestClient(t, f, nil)
	if _, err := c.Resolve(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	clk.advance(c.ttl + c.graceWindow + time.Second)
	if _, ok := c.IdentityFor(testTenantID); ok {
		t.Fatal("an entry past TTL+grace must not be served")
	}
}
