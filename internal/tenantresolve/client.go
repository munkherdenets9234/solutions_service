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

	mu     sync.RWMutex
	cache  map[string]*cacheEntry // keyed by hex(sha256(raw key))
	stop   chan struct{}
	closer sync.Once

	// degradedSince records when fetches started failing. Read by Degraded for
	// /readyz.
	degradedSince *time.Time
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
		http:        &http.Client{Timeout: cfg.Timeout},
		log:         cfg.Log,
		now:         time.Now,
		cache:       make(map[string]*cacheEntry),
		stop:        make(chan struct{}),
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
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.mu.Lock()
			now := c.now()
			for k, e := range c.cache {
				if now.Sub(e.fetchedAt) > c.ttl+c.graceWindow {
					delete(c.cache, k)
				}
			}
			c.mu.Unlock()
		}
	}
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
	key := cacheKey(rawKey)

	if e, ok := c.fresh(key); ok {
		if e.unknown {
			return Identity{}, ErrUnknownKey
		}
		return e.ident, nil
	}

	ident, err := c.fetch(ctx, rawKey)
	if err == nil {
		c.store(key, &cacheEntry{ident: ident, fetchedAt: c.now()})
		c.clearDegraded()
		return ident, nil
	}
	if errors.Is(err, ErrUnknownKey) {
		// An authoritative answer, so the link is healthy.
		c.store(key, &cacheEntry{fetchedAt: c.now(), unknown: true})
		c.clearDegraded()
		return Identity{}, ErrUnknownKey
	}

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
		_ = json.NewDecoder(res.Body).Decode(&eb)
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
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return Identity{}, fmt.Errorf("tenantresolve: decode: %w", err)
	}
	if !body.Success {
		return Identity{}, errors.New("tenantresolve: tenantcore reported failure")
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

func (c *Client) store(key string, e *cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = e
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
