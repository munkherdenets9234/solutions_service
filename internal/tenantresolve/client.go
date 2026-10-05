// Package tenantresolve asks tenantcore which tenant an API key belongs to.
//
// It exists so this service stops resolving X-API-Key against its own tenants
// collection: tenantcore owns tenants, and a second copy drifts the moment the
// platform console writes to the first.
package tenantresolve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

const (
	// DefaultTTL is how long a resolved identity is served without asking
	// tenantcore again.
	DefaultTTL = 60 * time.Second
	// DefaultGraceWindow is how long past the TTL a stale identity may be served
	// while tenantcore is unreachable. Long on purpose: an outage of the
	// platform must not take every storefront down with it. Past it the entry
	// is dropped and the caller gets ErrUnavailable.
	DefaultGraceWindow = 24 * time.Hour
	// DefaultNegativeTTL is how long a refusal ("unknown key") is remembered, so
	// a flood of bad keys cannot turn into a flood of requests to tenantcore.
	//
	// The cost: a newly issued or re-issued key can be refused for up to this
	// long after tenantcore starts accepting it, because the earlier refusal is
	// still cached. Keep it short.
	DefaultNegativeTTL = 30 * time.Second
	// DefaultTimeout is short on purpose. This is on the request path; a slow
	// platform must degrade to cache quickly rather than making every request
	// wait for it.
	DefaultTimeout = 3 * time.Second

	// FailureBackoff is how long, after a transport or server failure, the
	// client stops contacting tenantcore for ANY key and answers from cache (or
	// ErrUnavailable at once). Without it a blackholed tenantcore would make
	// every request pay the full Timeout for as long as the grace window lasts.
	FailureBackoff = 10 * time.Second
	// MaxEntries caps the cache. Unique bad keys each add a negative entry, so
	// without a cap a client sending random keys grows memory without bound.
	// Eviction drops negative entries first, then the oldest.
	MaxEntries = 10000

	// maxBody bounds how much of a tenantcore response is read.
	maxBody = 1 << 20
)

var (
	// ErrUnknownKey means tenantcore authoritatively said the tenant key is not
	// valid. It is never returned for a transport failure.
	ErrUnknownKey = errors.New("tenantresolve: unknown api key")
	// ErrUnavailable means tenantcore could not be asked (or answered
	// unusably) and nothing within the grace window is cached. There is no
	// honest answer, so the caller should turn it into a 503, never a 401.
	ErrUnavailable = errors.New("tenantresolve: tenantcore unreachable and nothing usable cached")
)

// Identity is who a tenant key belongs to. A suspended tenant resolves and says
// so; what suspension means is the caller's decision.
type Identity struct {
	TenantID  primitive.ObjectID
	Slug      string
	Name      string
	Domain    string
	Hosts     []string
	Suspended bool
	// Stale is set when this answer came from cache past its TTL because
	// tenantcore could not be reached.
	Stale bool
}

// Client resolves tenant API keys through tenantcore and caches the answers.
//
// Like the entitlement client it is built around how it FAILS: every request
// this service serves passes through here, so an unreachable tenantcore must
// not mean "unknown tenant". On a transport or server failure the client
// answers from its last-known identity with Stale set and says so via Degraded.
//
// The raw API key is a credential. It is sent to tenantcore in a header and is
// never stored: the cache is keyed by the hex SHA-256 of it, and it is never
// logged or put in an error.
type Client struct {
	baseURL     string
	serviceKey  string
	ttl         time.Duration
	graceWindow time.Duration
	negativeTTL time.Duration

	http *http.Client
	log  *zap.Logger
	// now is injectable so expiry tests do not sleep.
	now func() time.Time

	maxEntries int

	mu     sync.RWMutex
	cache  map[string]*cacheEntry // keyed by hex(sha256(raw key))
	stop   chan struct{}
	closer sync.Once

	// inflight collapses concurrent misses on one key into a single fetch.
	inflight map[string]*call
	// backoffUntil: until then, tenantcore is not contacted at all.
	backoffUntil time.Time

	// degradedSince records when fetches started failing. Read by Degraded for
	// /readyz.
	degradedSince *time.Time
}

// call is one shared in-flight resolution; waiters read the result after done
// is closed.
type call struct {
	done  chan struct{}
	ident Identity
	err   error
}

type cacheEntry struct {
	ident     Identity
	fetchedAt time.Time
	// unknown marks a negative entry: tenantcore refused this key.
	unknown bool
}

// ClientConfig configures the client.
type ClientConfig struct {
	BaseURL     string // tenantcore base URL
	ServiceKey  string // this service's own key on tenantcore
	TTL         time.Duration
	GraceWindow time.Duration
	// NegativeTTL is how long an unknown-key refusal is cached. A newly
	// re-issued key can be refused for up to this long.
	NegativeTTL time.Duration
	Timeout     time.Duration
	Log         *zap.Logger
}

// NewClient builds the client, or returns nil when the platform link is not
// configured. nil is a supported state; Available is safe on it.
func NewClient(cfg ClientConfig) *Client {
	if cfg.BaseURL == "" || cfg.ServiceKey == "" {
		return nil
	}
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultTTL
	}
	if cfg.GraceWindow <= 0 {
		cfg.GraceWindow = DefaultGraceWindow
	}
	if cfg.NegativeTTL <= 0 {
		cfg.NegativeTTL = DefaultNegativeTTL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}

	c := &Client{
		baseURL:     cfg.BaseURL,
		serviceKey:  cfg.ServiceKey,
		ttl:         cfg.TTL,
		graceWindow: cfg.GraceWindow,
		negativeTTL: cfg.NegativeTTL,
		http: &http.Client{
			Timeout: cfg.Timeout,
			// Never follow a redirect: it would forward our service key and the
			// tenant key to wherever it points.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		log:        cfg.Log,
		now:        time.Now,
		maxEntries: MaxEntries,
		cache:      make(map[string]*cacheEntry),
		inflight:   make(map[string]*call),
		stop:       make(chan struct{}),
	}
	go c.janitor()
	return c
}

// Available reports whether the platform link is configured. Safe on a nil
// receiver so callers need no nil check of their own.
func (c *Client) Available() bool { return c != nil }

// Close stops the janitor.
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.closer.Do(func() { close(c.stop) })
}

func (c *Client) janitor() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.sweep()
		}
	}
}

// sweep drops negative entries older than NegativeTTL and positive entries
// older than TTL+GraceWindow.
func (c *Client) sweep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweepLocked()
}

func (c *Client) sweepLocked() {
	now := c.now()
	for k, e := range c.cache {
		if c.expired(e, now) {
			delete(c.cache, k)
		}
	}
}

func (c *Client) expired(e *cacheEntry, now time.Time) bool {
	if e.unknown {
		return now.Sub(e.fetchedAt) > c.negativeTTL
	}
	return now.Sub(e.fetchedAt) > c.ttl+c.graceWindow
}

func cacheKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

// Resolve maps a raw tenant API key to its identity.
func (c *Client) Resolve(ctx context.Context, rawKey string) (Identity, error) {
	if !c.Available() {
		return Identity{}, errors.New("tenantresolve: platform link is not configured")
	}
	if rawKey == "" {
		return Identity{}, ErrUnknownKey
	}
	// A caller that has already given up gets its own error: not a tenantcore
	// failure, so no degraded flag, no stale answer, no log line.
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}
	key := cacheKey(rawKey)

	if e, ok := c.fresh(key); ok {
		if e.unknown {
			return Identity{}, ErrUnknownKey
		}
		return e.ident, nil
	}

	// Backing off after a recent failure: do not wait on a tenantcore that is
	// probably still down.
	if c.backingOff() {
		if e, ok := c.stale(key); ok {
			id := e.ident
			id.Stale = true
			return id, nil
		}
		return Identity{}, fmt.Errorf("%w: backing off after a recent failure", ErrUnavailable)
	}

	cl := c.join(key, rawKey)
	select {
	case <-cl.done:
		return cl.ident, cl.err
	case <-ctx.Done():
		// This waiter leaves; the shared fetch carries on for the others.
		return Identity{}, ctx.Err()
	}
}

// join returns the in-flight resolution for key, starting one if there is none.
// The fetch runs on its own context so one caller's cancellation cannot fail
// the others; the HTTP client's Timeout still bounds it.
func (c *Client) join(key, rawKey string) *call {
	c.mu.Lock()
	if cl, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		return cl
	}
	cl := &call{done: make(chan struct{})}
	c.inflight[key] = cl
	c.mu.Unlock()

	go func() {
		cl.ident, cl.err = c.refresh(key, rawKey)
		c.mu.Lock()
		delete(c.inflight, key)
		c.mu.Unlock()
		close(cl.done)
	}()
	return cl
}

func (c *Client) backingOff() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now().Before(c.backoffUntil)
}

func (c *Client) startBackoff() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.backoffUntil = c.now().Add(FailureBackoff)
}

// refresh asks tenantcore and applies the outcome to the cache, the degraded
// flag and the back-off. It runs once per key however many callers wait.
func (c *Client) refresh(key, rawKey string) (Identity, error) {
	ident, err := c.fetch(context.Background(), rawKey)
	if err == nil {
		c.store(key, &cacheEntry{ident: ident, fetchedAt: c.now()})
		c.clearDegraded()
		c.clearBackoff()
		return ident, nil
	}
	if errors.Is(err, ErrUnknownKey) {
		// An authoritative answer, so the link is healthy and no back-off starts.
		c.store(key, &cacheEntry{fetchedAt: c.now(), unknown: true})
		c.clearDegraded()
		c.clearBackoff()
		return Identity{}, ErrUnknownKey
	}
	c.startBackoff()
	return c.fallback(key, err)
}

func (c *Client) clearBackoff() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.backoffUntil = time.Time{}
}

// fallback answers a failed fetch from cache or says there is no answer.
//
// A rejected SERVICE key takes this path too, deliberately, as in the
// entitlement client: a cached identity is still served (flagged Stale, and
// degraded). The cost is up to GraceWindow (24h) of serving identity after our
// service key was revoked, visible only through the degraded flag.
func (c *Client) fallback(key string, err error) (Identity, error) {
	// "We could not find out." Serve last-known state rather than deciding
	// against the tenant.
	if e, ok := c.stale(key); ok {
		c.markDegraded()
		c.logOnce("serving a cached tenant identity: tenantcore is unreachable", err)
		id := e.ident
		id.Stale = true
		return id, nil
	}

	c.markDegraded()
	c.logOnce("tenantcore is unreachable and no tenant identity is cached", err)
	return Identity{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
}

func (c *Client) fetch(ctx context.Context, rawKey string) (Identity, error) {
	url := c.baseURL + "/api/v1/svc/tenants/resolve"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("X-Service-Key", c.serviceKey)
	req.Header.Set("X-Tenant-Key", rawKey)

	res, err := c.http.Do(req)
	if err != nil {
		return Identity{}, err
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		// Two different 401s. The tenant key being refused carries the TENANT
		// error domain; anything else is OUR service key being rejected, which
		// is a misconfigured deployment, not a statement about the tenant, and
		// must never be cached or reported as an unknown key.
		var eb struct {
			Error struct {
				Domain string `json:"domain"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, maxBody)).Decode(&eb)
		if res.StatusCode == http.StatusUnauthorized && eb.Error.Domain == "TENANT" {
			return Identity{}, ErrUnknownKey
		}
		return Identity{}, fmt.Errorf(
			"tenantresolve: tenantcore rejected OUR service key (%d) — check TENANTCORE_SERVICE_KEY", res.StatusCode)
	default:
		return Identity{}, fmt.Errorf("tenantresolve: tenantcore returned %d", res.StatusCode)
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			TenantID primitive.ObjectID `json:"tenant_id"`
			Slug     string             `json:"slug"`
			Name     string             `json:"name"`
			Status   string             `json:"status"`
			Domain   string             `json:"domain"`
			Hosts    []string           `json:"hosts"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxBody)).Decode(&body); err != nil {
		return Identity{}, fmt.Errorf("tenantresolve: decode: %w", err)
	}
	if !body.Success {
		return Identity{}, errors.New("tenantresolve: tenantcore reported failure")
	}
	if body.Data.TenantID.IsZero() {
		// A success with no tenant is not an identity; never cache it.
		return Identity{}, errors.New("tenantresolve: decode: response has no tenant_id")
	}
	return Identity{
		TenantID:  body.Data.TenantID,
		Slug:      body.Data.Slug,
		Name:      body.Data.Name,
		Domain:    body.Data.Domain,
		Hosts:     body.Data.Hosts,
		Suspended: body.Data.Status == "suspended",
	}, nil
}

// fresh returns a cached entry still inside its TTL (or, for a negative entry,
// inside NegativeTTL).
func (c *Client) fresh(key string) (cacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.cache[key]
	if !ok {
		return cacheEntry{}, false
	}
	limit := c.ttl
	if e.unknown {
		limit = c.negativeTTL
	}
	if c.now().Sub(e.fetchedAt) > limit {
		return cacheEntry{}, false
	}
	return *e, true
}

// stale returns a positive entry within TTL+GraceWindow. Refusals are never
// served stale.
func (c *Client) stale(key string) (cacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.cache[key]
	if !ok || e.unknown || c.now().Sub(e.fetchedAt) > c.ttl+c.graceWindow {
		return cacheEntry{}, false
	}
	return *e, true
}

// store inserts an entry, keeping the cache within maxEntries. When full it
// first drops expired entries, then the oldest negative entry, then the oldest
// entry: a real tenant's identity outlives unique-bad-key noise.
func (c *Client) store(key string, e *cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.cache[key]; !exists && len(c.cache) >= c.maxEntries {
		c.sweepLocked()
		if len(c.cache) >= c.maxEntries {
			c.evictOneLocked()
		}
	}
	c.cache[key] = e
}

func (c *Client) evictOneLocked() {
	var oldestNeg, oldestAny string
	var negAt, anyAt time.Time
	for k, e := range c.cache {
		if oldestAny == "" || e.fetchedAt.Before(anyAt) {
			oldestAny, anyAt = k, e.fetchedAt
		}
		if e.unknown && (oldestNeg == "" || e.fetchedAt.Before(negAt)) {
			oldestNeg, negAt = k, e.fetchedAt
		}
	}
	if oldestNeg != "" {
		delete(c.cache, oldestNeg)
	} else if oldestAny != "" {
		delete(c.cache, oldestAny)
	}
}

func (c *Client) markDegraded() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.degradedSince == nil {
		now := c.now()
		c.degradedSince = &now
	}
}

func (c *Client) clearDegraded() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.degradedSince = nil
}

// Degraded reports whether the platform link is currently failing, and since
// when. Surfaced on /readyz so a monitor can alert on it.
func (c *Client) Degraded() (bool, *time.Time) {
	if c == nil {
		return false, nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.degradedSince != nil, c.degradedSince
}

// logOnce keeps an outage from writing a line per request.
func (c *Client) logOnce(msg string, err error) {
	if c.log == nil {
		return
	}
	degraded, since := c.Degraded()
	if degraded && since != nil && c.now().Sub(*since) > time.Second {
		return
	}
	c.log.Warn(msg, zap.Error(err), zap.String("platform", c.baseURL))
}
