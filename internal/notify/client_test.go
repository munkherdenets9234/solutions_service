package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newClient(t *testing.T, h http.Handler, timeout time.Duration) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := NewClient(Config{BaseURL: srv.URL, ServiceKey: "svc-key", Timeout: timeout})
	if c == nil {
		t.Fatal("NewClient returned nil for a configured link")
	}
	return c
}

func TestSendPostsTheTemplateWithTheServiceKey(t *testing.T) {
	var gotPath, gotKey, gotMethod string
	var gotBody struct {
		To       string            `json:"to"`
		Template string            `json:"template"`
		Data     map[string]string `json:"data"`
	}
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotKey = r.Method, r.URL.Path, r.Header.Get("X-Service-Key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{"accepted":true}}`))
	}), time.Second)

	err := c.Send(context.Background(), "ops@example.com", "password_reset_code",
		map[string]string{"app": "E and S admin", "code": "048213"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/svc/notifications/email" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	// The service key proves which PRODUCT is asking; without it tenantcore
	// refuses, and with the wrong one anyone could send as us.
	if gotKey != "svc-key" {
		t.Fatalf("X-Service-Key = %q", gotKey)
	}
	if gotBody.To != "ops@example.com" || gotBody.Template != "password_reset_code" || gotBody.Data["code"] != "048213" {
		t.Fatalf("body = %+v", gotBody)
	}
}

func TestSendReportsNon2xx(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusUnauthorized, "401"},
		{http.StatusTooManyRequests, "429"},
		{http.StatusServiceUnavailable, "not configured"},
		{http.StatusInternalServerError, "500"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}), time.Second)
			err := c.Send(context.Background(), "a@b.c", "password_reset_code", nil)
			if err == nil {
				t.Fatalf("status %d produced no error", tc.status)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should mention %q", err, tc.want)
			}
		})
	}
}

func TestNilClientIsUnavailable(t *testing.T) {
	for _, cfg := range []Config{{}, {BaseURL: "http://x"}, {ServiceKey: "k"}} {
		if c := NewClient(cfg); c != nil {
			t.Fatalf("expected nil client for %+v", cfg)
		}
	}
	var c *Client
	if c.Available() {
		t.Fatal("nil client reported available")
	}
	if err := c.Send(context.Background(), "a@b.c", "x", nil); err == nil {
		t.Fatal("Send on an unconfigured client must error, not silently succeed")
	}
}

// Review Focus 3. Mail is sent after the response, so a hung connection is not
// felt by the caller; but each hung send still holds a goroutine, and an
// unbounded wait would let a slow tenantcore pile them up.
func TestSendGivesUpOnAServerThatNeverAnswers(t *testing.T) {
	block := make(chan struct{})
	c := newClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }), 100*time.Millisecond)
	// Registered AFTER newClient on purpose: cleanups run last-in-first-out, and
	// the handler must be released before the server's Close waits for it. In
	// the other order Close blocks on a handler that is waiting to be released.
	t.Cleanup(func() { close(block) })

	start := time.Now()
	err := c.Send(context.Background(), "a@b.c", "password_reset_code", nil)
	if err == nil {
		t.Fatal("expected an error from a server that never answers")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Send took %v; it must give up near its 100ms timeout", elapsed)
	}
}

func TestSendWrapsRateLimitSentinel(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}), time.Second)
	err := c.Send(context.Background(), "a@b.c", "password_reset_code", nil)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("429 should wrap ErrRateLimited, got %v", err)
	}
}
