package entitlement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

// Client is the Provider implementation this service runs on: it asks
// tenantcore what a tenant has bought, and caches the answer.
//
// This is the half the seam was built for. What it replaced read the
// subscription and the plan out of this service's own database; the two
// copies drifted the moment the platform console wrote to tenantcore, which
// is the whole reason for the swap. Nothing that calls Provider changed.
//
// Everything interesting about this type is in how it FAILS. An entitlement
// lookup sits on the request path of every mutating call this service
// serves. If an unreachable tenantcore meant "not entitled", tenantcore would
// become a single point of failure for every product at once — strictly worse
// than the monolith the split replaced — and it would tell paying customers
// they had not paid. So on any transport or server failure the client answers
// from its last-known value with Stale set, and says so loudly (see Degraded,
// surfaced on /readyz).
//
// Two differences from the equivalent client in the car wash service, both
// deliberate:
//
//   - It looks tenants up BY ID, not by API key. digitalservice still owns
//     its own tenants collection and resolves X-API-Key itself in
//     TenantMiddleware, so the tenant is already known by the time anything
//     asks about entitlement. tenantcore documents /entitlements/{tenant_id}
//     as the route for exactly this case. The car wash keeps no tenants at
//     all and so must resolve key and entitlement in one call.
//   - A tenant tenantcore has never heard of is not a denial. See fetch.
type Client struct {
	baseURL    string
	serviceKey string
	ttl        time.Duration
	// graceWindow is how long a stale answer may be served once the TTL has
	// passed and tenantcore is unreachable. Past it the entry is dropped and
	// the next call fails honestly rather than serving an hour-old
	// entitlement forever.
	graceWindow time.Duration

	http *http.Client
	log  *zap.Logger

	mu     sync.RWMutex
	cache  map[string]*cacheEntry // keyed by tenant id hex
	stop   chan struct{}
	closer sync.Once

	// degradedSince records when fetches started failing and we began
	// answering from cache. Read by Degraded for /readyz.
	degradedSince *time.Time
}

type cacheEntry struct {
	ent       Entitlement
	fetchedAt time.Time
}

// ClientConfig configures the client.
type ClientConfig struct {
	BaseURL     string // e.g. http://localhost:8092
	ServiceKey  string // this service's own key, from POST /admin/service-clients on tenantcore
	TTL         time.Duration
	GraceWindow time.Duration
	Timeout     time.Duration
	Log         *zap.Logger
}

// NewClient builds the client, or returns nil when the platform link is not
// configured.
//
// nil is a supported state, not a failure: it follows the same rule as every
// other optional dependency here (see buildUpload and config.Features). The
// process starts, /readyz says the link is missing, and bootstrap substitutes
// Unenforced so the service keeps serving instead of refusing every write.
func NewClient(cfg ClientConfig) *Client {
	if cfg.BaseURL == "" || cfg.ServiceKey == "" {
		return nil
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 60 * time.Second
	}
	if cfg.GraceWindow <= 0 {
		cfg.GraceWindow = 15 * time.Minute
	}
	if cfg.Timeout <= 0 {
		// Short on purpose. This is on the request path; a slow platform
		// must degrade to cache quickly rather than making every tenant's
		// write wait for it.
		cfg.Timeout = 3 * time.Second
	}

	c := &Client{
		baseURL:     cfg.BaseURL,
		serviceKey:  cfg.ServiceKey,
		ttl:         cfg.TTL,
		graceWindow: cfg.GraceWindow,
		http:        &http.Client{Timeout: cfg.Timeout},
		log:         cfg.Log,
		cache:       make(map[string]*cacheEntry),
		stop:        make(chan struct{}),
	}
	go c.janitor()
	return c
}

// Compile-time proof that the swap is real: if Client stops satisfying
// Provider the build breaks here, not at a call site.
var _ Provider = (*Client)(nil)

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
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case now := <-t.C:
			c.mu.Lock()
			for k, e := range c.cache {
				if now.Sub(e.fetchedAt) > c.ttl+c.graceWindow {
					delete(c.cache, k)
				}
			}
			c.mu.Unlock()
		}
	}
}

// For implements Provider.
func (c *Client) For(ctx context.Context, tenantID primitive.ObjectID) (Entitlement, error) {
	if !c.Available() {
		return Entitlement{}, errors.New("entitlement: platform link is not configured")
	}
	key := tenantID.Hex()

	if e, ok := c.fresh(key); ok {
		return e, nil
	}

	ent, err := c.fetch(ctx, tenantID)
	if err == nil {
		c.store(key, ent)
		c.clearDegraded()
		return ent, nil
	}

	// "We could not find out." Serve last-known state rather than deciding
	// against the tenant — see the type comment.
	if e, ok := c.stale(key); ok {
		c.markDegraded()
		c.logOnce("serving a cached entitlement: tenantcore is unreachable", err)
		e.Stale = true
		return e, nil
	}

	// Nothing cached and the platform is down. There is no honest answer, so
	// say so rather than inventing one in either direction — the caller turns
	// this into a 500/503, never a 402.
	c.markDegraded()
	return Entitlement{}, fmt.Errorf("entitlement: tenantcore unreachable and nothing cached: %w", err)
}

func (c *Client) fetch(ctx context.Context, tenantID primitive.ObjectID) (Entitlement, error) {
	url := c.baseURL + "/api/v1/svc/entitlements/" + tenantID.Hex()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Entitlement{}, err
	}
	// The service key proves which PRODUCT is asking. There is no tenant
	// credential here: the tenant is named in the path, because this service
	// resolved it from its own X-API-Key before reaching this point.
	req.Header.Set("X-Service-Key", c.serviceKey)

	res, err := c.http.Do(req)
	if err != nil {
		return Entitlement{}, err
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		// tenantcore has never heard of this tenant.
		//
		// This is NOT a denial, and treating it as one would take live
		// storefronts down. digitalservice still owns its own tenants
		// collection, and the migration that copies them into tenantcore has
		// not been run against a real database — so during the cutover a
		// tenant can legitimately exist here and not there. Reported as
		// StatusUnknown, which is the same thing this service has always
		// said about a tenant with no subscription record: not held to any
		// subscription state.
		//
		// A deliberate migration affordance, and exactly the kind of default
		// that quietly becomes permanent. Once the migration has run and
		// every tenant resolves, this should become an error so a genuinely
		// missing tenant is loud instead of silently unenforced. The WARN
		// below is what tells you it is still happening.
		c.logOnce("tenantcore does not know this tenant — treating as unprovisioned; "+
			"run the migration if this is not a brand-new tenant",
			fmt.Errorf("tenant %s not found on platform", tenantID.Hex()))
		return Entitlement{TenantID: tenantID, Status: StatusUnknown}, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		// Our own service key was rejected. That is a misconfigured
		// deployment, not a statement about the tenant, and it must not be
		// cached or degraded into an answer.
		return Entitlement{}, fmt.Errorf(
			"entitlement: tenantcore rejected OUR service key (%d) — check TENANTCORE_SERVICE_KEY", res.StatusCode)
	default:
		return Entitlement{}, fmt.Errorf("entitlement: tenantcore returned %d", res.StatusCode)
	}

	var body struct {
		Success bool        `json:"success"`
		Data    Entitlement `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return Entitlement{}, fmt.Errorf("entitlement: decode: %w", err)
	}
	if !body.Success {
		return Entitlement{}, errors.New("entitlement: tenantcore reported failure")
	}
	// tenantcore sends modules/limits/features as null rather than empty
	// when a tenant has no subscription. Nothing below needs normalising:
	// HasModule treats a nil list exactly as it treats an empty one (both
	// mean "not enforced"), Limit reports nil as no ceiling, and Feature
	// reports nil as off. Stated here because the console had to learn it
	// the hard way.
	return body.Data, nil
}

func (c *Client) fresh(key string) (Entitlement, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.cache[key]
	if !ok || time.Since(e.fetchedAt) > c.ttl {
		return Entitlement{}, false
	}
	return e.ent, true
}

func (c *Client) stale(key string) (Entitlement, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.cache[key]
	if !ok || time.Since(e.fetchedAt) > c.ttl+c.graceWindow {
		return Entitlement{}, false
	}
	return e.ent, true
}

func (c *Client) store(key string, ent Entitlement) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = &cacheEntry{ent: ent, fetchedAt: time.Now()}
}

func (c *Client) markDegraded() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.degradedSince == nil {
		now := time.Now()
		c.degradedSince = &now
	}
}

func (c *Client) clearDegraded() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.degradedSince = nil
}

// Degraded reports whether the platform link is currently failing, and since
// when.
//
// This is the loud half of serving stale state. Falling back to cache is the
// right behaviour and a liability on its own: without this, a platform that
// has been down for a day looks exactly like one that is fine, right up until
// a cache entry ages out and a tenant is refused for no visible reason.
// Surfaced on /readyz so a monitor can alert on it.
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
	if degraded && since != nil && time.Since(*since) > time.Second {
		return
	}
	c.log.Warn(msg, zap.Error(err), zap.String("platform", c.baseURL))
}
