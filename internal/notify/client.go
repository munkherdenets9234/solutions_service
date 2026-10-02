// Package notify asks tenantcore to send mail on this service's behalf.
//
// digitalservice never holds mail credentials. tenantcore is the platform's one
// sender: products name a template and supply data, and it delivers. That keeps
// one app password in one place and means a leaked service key can only trigger
// a templated message, never send arbitrary mail from the platform's address.
//
// It reuses the TENANTCORE_URL and TENANTCORE_SERVICE_KEY the entitlement link
// already needs, so mail adds no new configuration.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrMailNotConfigured is tenantcore answering 503: it is reachable but has no
// mail relay of its own. Callers that can report it to a person should.
var ErrMailNotConfigured = errors.New("notify: mail is not configured on tenantcore (503)")

type Config struct {
	BaseURL    string // e.g. http://localhost:8092
	ServiceKey string // this service's key, from POST /admin/service-clients on tenantcore
	Timeout    time.Duration
}

// Client sends templated mail through tenantcore. A nil *Client is a valid
// "mail is not configured" value: every method is nil-safe.
type Client struct {
	baseURL    string
	serviceKey string
	http       *http.Client
}

// NewClient returns nil when the link is not configured. That is a supported
// state, not a failure: the service starts, says so on /readyz, and the routes
// that need mail answer FEATURE_UNAVAILABLE.
func NewClient(cfg Config) *Client {
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.ServiceKey) == "" {
		return nil
	}
	if cfg.Timeout <= 0 {
		// Short on purpose. Mail is sent after the response, so a slow
		// tenantcore is not felt by the caller, but every hung send still
		// holds a goroutine, and an unbounded wait lets them pile up.
		cfg.Timeout = 5 * time.Second
	}
	return &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		serviceKey: cfg.ServiceKey,
		http:       &http.Client{Timeout: cfg.Timeout},
	}
}

// Available reports whether mail is configured. Safe on a nil receiver.
func (c *Client) Available() bool { return c != nil }

// Send asks tenantcore to deliver one templated message.
//
// A 2xx means tenantcore's relay ACCEPTED it, not that it was delivered; SMTP
// cannot promise the latter.
func (c *Client) Send(ctx context.Context, to, template string, data map[string]string) error {
	if !c.Available() {
		return errors.New("notify: tenantcore link is not configured")
	}

	body, err := json.Marshal(map[string]any{"to": to, "template": template, "data": data})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/svc/notifications/email", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// The service key proves which PRODUCT is asking.
	req.Header.Set("X-Service-Key", c.serviceKey)

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	defer res.Body.Close()

	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return nil
	case res.StatusCode == http.StatusServiceUnavailable:
		return ErrMailNotConfigured
	case res.StatusCode == http.StatusTooManyRequests:
		return errors.New("notify: tenantcore rate limited the send (429)")
	default:
		return fmt.Errorf("notify: tenantcore answered %d", res.StatusCode)
	}
}
