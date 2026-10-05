package tenantresolve

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitForCalls(t *testing.T, f *fakeTenantcore, n int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for f.calls.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d calls (have %d)", n, f.calls.Load())
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSweep_EvictsNegativeAfterNegativeTTL(t *testing.T) {
	f := newFake(t)
	f.setMode("unknown")
	c, clk := newTestClient(t, f, nil)

	_, _ = c.Resolve(context.Background(), "test-key-bad")
	c.sweep()
	if len(c.cache) != 1 {
		t.Fatal("negative entry should survive inside NegativeTTL")
	}
	clk.advance(DefaultNegativeTTL + time.Second)
	c.sweep()
	if len(c.cache) != 0 {
		t.Fatal("negative entry should be reaped after NegativeTTL")
	}
}

func TestSweep_EvictsPositiveOnlyPastGrace(t *testing.T) {
	f := newFake(t)
	c, clk := newTestClient(t, f, nil)
	if _, err := c.Resolve(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	clk.advance(23 * time.Hour)
	c.sweep()
	if len(c.cache) != 1 {
		t.Fatal("positive entry inside the grace window must survive")
	}
	clk.advance(2 * time.Hour)
	c.sweep()
	if len(c.cache) != 0 {
		t.Fatal("positive entry past TTL+grace should be reaped")
	}
}

func TestStore_CapEvictsNegativeFirstThenOldest(t *testing.T) {
	f := newFake(t)
	c, clk := newTestClient(t, f, nil)
	c.maxEntries = 3

	// One real (positive) tenant, stored first so it is the OLDEST entry.
	if _, err := c.Resolve(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	posKey := cacheKey(testKey)
	f.setMode("unknown")
	for _, k := range []string{"test-key-n1", "test-key-n2", "test-key-n3", "test-key-n4", "test-key-n5"} {
		clk.advance(time.Millisecond)
		if _, err := c.Resolve(context.Background(), k); !errors.Is(err, ErrUnknownKey) {
			t.Fatalf("want ErrUnknownKey, got %v", err)
		}
		if len(c.cache) > 3 {
			t.Fatalf("cache exceeded cap: %d", len(c.cache))
		}
	}
	if _, ok := c.cache[posKey]; !ok {
		t.Fatal("the positive tenant entry was evicted before negative ones")
	}

	// With only positive entries, the oldest goes.
	c2, clk2 := newTestClient(t, newFake(t), nil)
	c2.maxEntries = 2
	for i, k := range []string{"a", "b", "c"} {
		clk2.advance(time.Second)
		c2.store(k, &cacheEntry{fetchedAt: clk2.now()})
		_ = i
	}
	if _, ok := c2.cache["a"]; ok || len(c2.cache) != 2 {
		t.Fatal("oldest positive entry should be evicted at the cap")
	}
}

func TestResolve_ConcurrentMissesCollapse(t *testing.T) {
	f := newFake(t)
	f.gate = make(chan struct{})
	c, _ := newTestClient(t, f, nil)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := c.Resolve(context.Background(), testKey)
			if err != nil || id.TenantID != testTenantID {
				t.Errorf("bad result: %+v %v", id, err)
			}
		}()
	}
	waitForCalls(t, f, 1)
	time.Sleep(50 * time.Millisecond)
	close(f.gate)
	wg.Wait()
	if n := f.calls.Load(); n != 1 {
		t.Fatalf("want 1 call for concurrent misses, got %d", n)
	}
}

func TestResolve_CancelledWaiterDoesNotCancelSharedFetch(t *testing.T) {
	f := newFake(t)
	f.gate = make(chan struct{})
	c, _ := newTestClient(t, f, nil)

	ctx1, cancel1 := context.WithCancel(context.Background())
	err1 := make(chan error, 1)
	go func() { _, err := c.Resolve(ctx1, testKey); err1 <- err }()
	waitForCalls(t, f, 1)

	type res struct {
		id  Identity
		err error
	}
	r2 := make(chan res, 1)
	go func() { id, err := c.Resolve(context.Background(), testKey); r2 <- res{id, err} }()
	time.Sleep(50 * time.Millisecond)

	cancel1()
	if err := <-err1; !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	close(f.gate)
	r := <-r2
	if r.err != nil || r.id.TenantID != testTenantID {
		t.Fatalf("other waiter should still succeed: %+v %v", r.id, r.err)
	}
	if n := f.calls.Load(); n != 1 {
		t.Fatalf("want 1 call, got %d", n)
	}
	if deg, _ := c.Degraded(); deg {
		t.Fatal("a cancelled caller must not mark degraded")
	}
}

func TestResolve_BackoffSkipsFetchAndServesStale(t *testing.T) {
	f := newFake(t)
	c, clk := newTestClient(t, f, nil)
	if _, err := c.Resolve(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	f.setMode("down")
	clk.advance(DefaultTTL + time.Second)

	if id, err := c.Resolve(context.Background(), testKey); err != nil || !id.Stale {
		t.Fatalf("want stale, got %+v %v", id, err)
	}
	before := f.calls.Load()

	// Inside the back-off: no calls for this key or any other.
	clk.advance(FailureBackoff / 2)
	for i := 0; i < 5; i++ {
		if id, err := c.Resolve(context.Background(), testKey); err != nil || !id.Stale {
			t.Fatalf("want stale during back-off, got %+v %v", id, err)
		}
		if _, err := c.Resolve(context.Background(), "test-key-other"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("want immediate ErrUnavailable for an uncached key, got %v", err)
		}
	}
	if f.calls.Load() != before {
		t.Fatal("tenantcore was contacted during back-off")
	}

	// After the back-off a call is made again.
	clk.advance(FailureBackoff)
	_, _ = c.Resolve(context.Background(), testKey)
	if f.calls.Load() != before+1 {
		t.Fatalf("want one call after back-off, got %d", f.calls.Load()-before)
	}

	// A success clears the back-off.
	f.setMode("ok")
	clk.advance(FailureBackoff + time.Second)
	if id, err := c.Resolve(context.Background(), testKey); err != nil || id.Stale {
		t.Fatalf("want fresh, got %+v %v", id, err)
	}
	if _, err := c.Resolve(context.Background(), "test-key-other"); err != nil {
		t.Fatalf("back-off should be cleared: %v", err)
	}
}

func TestResolve_UnknownKeyDoesNotStartBackoff(t *testing.T) {
	f := newFake(t)
	f.setMode("unknown")
	c, _ := newTestClient(t, f, nil)

	for _, k := range []string{"test-key-x1", "test-key-x2", "test-key-x3"} {
		if _, err := c.Resolve(context.Background(), k); !errors.Is(err, ErrUnknownKey) {
			t.Fatalf("want ErrUnknownKey, got %v", err)
		}
	}
	if n := f.calls.Load(); n != 3 {
		t.Fatalf("each distinct unknown key should be asked once; got %d calls", n)
	}
}

func TestResolve_CancelledContextIsNotAFailure(t *testing.T) {
	f := newFake(t)
	c, clk := newTestClient(t, f, nil)
	if _, err := c.Resolve(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	clk.advance(DefaultTTL + time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := f.calls.Load()
	id, err := c.Resolve(ctx, testKey)
	if !errors.Is(err, context.Canceled) || id.Stale {
		t.Fatalf("want context.Canceled and no stale answer, got %+v %v", id, err)
	}
	if deg, _ := c.Degraded(); deg || f.calls.Load() != before {
		t.Fatal("cancelled caller must not degrade or call tenantcore")
	}
}

func TestResolve_EmptyKeyIsUnknownWithoutCall(t *testing.T) {
	f := newFake(t)
	c, _ := newTestClient(t, f, nil)
	if _, err := c.Resolve(context.Background(), ""); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("want ErrUnknownKey, got %v", err)
	}
	if f.calls.Load() != 0 {
		t.Fatal("empty key must not reach tenantcore")
	}
}

func TestResolve_SuccessWithZeroTenantIDIsUndecodable(t *testing.T) {
	f := newFake(t)
	f.zeroID = true
	c, _ := newTestClient(t, f, nil)
	_, err := c.Resolve(context.Background(), testKey)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if len(c.cache) != 0 {
		t.Fatal("a zero identity must not be cached")
	}
}

func TestFetch_DoesNotFollowRedirects(t *testing.T) {
	var hits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer target.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redir.Close()

	c := NewClient(ClientConfig{BaseURL: redir.URL, ServiceKey: testSvcKey})
	defer c.Close()
	_, err := c.Resolve(context.Background(), testKey)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if hits.Load() != 0 {
		t.Fatal("redirect was followed; credentials would have been forwarded")
	}
}

func TestResolve_ServiceKeyRejectedWithStaleEntryServesStaleDegraded(t *testing.T) {
	f := newFake(t)
	c, clk := newTestClient(t, f, nil)
	if _, err := c.Resolve(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	f.setMode("svcrejected")
	clk.advance(DefaultTTL + time.Second)

	id, err := c.Resolve(context.Background(), testKey)
	if err != nil || !id.Stale || id.TenantID != testTenantID {
		t.Fatalf("deliberate: serve stale on service-key rejection, got %+v %v", id, err)
	}
	if deg, _ := c.Degraded(); !deg {
		t.Fatal("must be flagged degraded")
	}
}

func TestResolve_SuspendedServedStaleKeepsBothFlags(t *testing.T) {
	f := newFake(t)
	f.setMode("suspended")
	c, clk := newTestClient(t, f, nil)
	if _, err := c.Resolve(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	f.setMode("down")
	clk.advance(DefaultTTL + time.Second)
	id, err := c.Resolve(context.Background(), testKey)
	if err != nil || !id.Suspended || !id.Stale {
		t.Fatalf("want Suspended and Stale, got %+v %v", id, err)
	}
}

func TestResolve_EntryInsideGraceWindowIsServed(t *testing.T) {
	f := newFake(t)
	c, clk := newTestClient(t, f, nil)
	if _, err := c.Resolve(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	f.setMode("down")
	clk.advance(23 * time.Hour)
	id, err := c.Resolve(context.Background(), testKey)
	if err != nil || !id.Stale {
		t.Fatalf("want stale at ~23h, got %+v %v", id, err)
	}
	if !strings.HasPrefix(id.Slug, "acme") {
		t.Fatal("unexpected identity")
	}
}
