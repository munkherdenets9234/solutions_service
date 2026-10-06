package response

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/gin-gonic/gin"
)

func render(err error, devMode bool) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	Err(c, err, devMode)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (body %s)", err, w.Body.String())
	}
	return body
}

func TestErrRendersTheTaxonomy(t *testing.T) {
	w := render(apierr.NotFound("blog"), false)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}

	body := decode(t, w)
	if body["success"] != false {
		t.Error("success should be false")
	}
	errBody, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("error object missing: %v", body)
	}
	if errBody["code"] != apierr.CodeNotFound {
		t.Errorf("code = %v, want %s", errBody["code"], apierr.CodeNotFound)
	}
	if errBody["message"] != "blog not found" {
		t.Errorf("message = %v, want %q", errBody["message"], "blog not found")
	}
}

// The top-level "message" is what every existing client reads. Dropping it in
// favour of the structured object would be a breaking change for no gain, so
// both are written.
func TestErrKeepsTheLegacyMessageField(t *testing.T) {
	body := decode(t, render(apierr.BadRequest("invalid id"), false))

	if body["message"] != "invalid id" {
		t.Errorf("top-level message = %v, want %q", body["message"], "invalid id")
	}
}

// An error that never went through the taxonomy is a bug. The caller gets a
// generic 500 and the cause stays in the log, where it belongs — a bug's text
// is not a contract, and it is frequently the text of a database error.
func TestErrHidesTheCauseOfAnUntypedError(t *testing.T) {
	w := render(errors.New("pq: relation \"secret_table\" does not exist"), false)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if got := w.Body.String(); strings.Contains(got, "secret_table") {
		t.Errorf("the underlying cause leaked to the client: %s", got)
	}
}

func TestErrIncludesTheStackOnlyInDev(t *testing.T) {
	prod := decode(t, render(apierr.Internal(errors.New("boom")), false))
	if e, ok := prod["error"].(map[string]any); ok {
		if _, present := e["stack_trace"]; present {
			t.Error("stack_trace must not reach a production client")
		}
	}

	dev := decode(t, render(apierr.Internal(errors.New("boom")), true))
	e, ok := dev["error"].(map[string]any)
	if !ok {
		t.Fatal("error object missing")
	}
	if _, present := e["stack_trace"]; !present {
		t.Error("stack_trace should be present in development")
	}
}

func renderFrom(err error, devMode bool, remoteAddr string) map[string]any {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.RemoteAddr = remoteAddr
	Err(c, err, devMode)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body
}

func detailOf(body map[string]any) (string, bool) {
	e, _ := body["error"].(map[string]any)
	d, ok := e["detail"].(string)
	return d, ok
}

// The public message is the same whatever the cause; the cause is for a
// developer on this machine only.
func TestErrDetailOnlyInDevToALoopbackCaller(t *testing.T) {
	cause := func() error { return apierr.Unauthorized("").WithDetail("no tenant matches this key") }

	for _, c := range []struct {
		name    string
		dev     bool
		remote  string
		visible bool
	}{
		{"dev, loopback v4", true, "127.0.0.1:5555", true},
		{"dev, loopback v6", true, "[::1]:5555", true},
		{"dev, remote caller", true, "203.0.113.9:5555", false},
		{"prod, loopback", false, "127.0.0.1:5555", false},
		{"prod, remote caller", false, "203.0.113.9:5555", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, present := detailOf(renderFrom(cause(), c.dev, c.remote))
			if present != c.visible {
				t.Fatalf("detail present = %v, want %v", present, c.visible)
			}
			if c.visible && d != "no tenant matches this key" {
				t.Errorf("detail = %q", d)
			}
		})
	}
}

// With no explicit detail, a development caller on this machine sees the cause
// of an internal error instead of having to find it in the log.
func TestErrDetailFallsBackToTheCauseInDev(t *testing.T) {
	d, ok := detailOf(renderFrom(apierr.Internal(errors.New("mongo: no reachable servers")), true, "127.0.0.1:1"))
	if !ok || d != "mongo: no reachable servers" {
		t.Errorf("detail = %q (present %v)", d, ok)
	}
	if _, ok := detailOf(renderFrom(apierr.Internal(errors.New("mongo: no reachable servers")), true, "203.0.113.9:1")); ok {
		t.Error("cause leaked to a non-loopback caller")
	}
}

// A 429 without Retry-After leaves a well-behaved client no way to back off.
func TestErrSetsRetryAfterOnRateLimit(t *testing.T) {
	w := render(apierr.RateLimited(30), false)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, want %q", got, "30")
	}
}

func TestErrUnwrapsAWrappedAppError(t *testing.T) {
	// A service that wraps a taxonomy error with fmt.Errorf must still
	// produce the right status, not a blanket 500.
	wrapped := errors.Join(errors.New("while loading the page"), apierr.Forbidden("nope"))

	w := render(wrapped, false)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}
