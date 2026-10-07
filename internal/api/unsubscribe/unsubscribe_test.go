package unsubscribe

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/middleware"
	unsubtoken "github.com/eandstravel/digitalservice/internal/unsubscribe"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

type optCall struct {
	tenant, user primitive.ObjectID
	v            bool
}

type fakeUsers struct {
	calls []optCall
	known map[primitive.ObjectID]bool // user ids that exist
	err   error
}

func (f *fakeUsers) SetReceiveEmails(_ context.Context, tenantID, id primitive.ObjectID, v bool) error {
	f.calls = append(f.calls, optCall{tenantID, id, v})
	if f.err != nil {
		return f.err
	}
	if !f.known[id] {
		return mongo.ErrNoDocuments
	}
	return nil
}

type cancelCall struct{ tenant, user primitive.ObjectID }

type fakeOutbox struct {
	calls []cancelCall
	err   error
}

func (f *fakeOutbox) CancelPendingForUser(_ context.Context, tenantID, userID primitive.ObjectID) (int64, error) {
	f.calls = append(f.calls, cancelCall{tenantID, userID})
	return 1, f.err
}

func testKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

type rig struct {
	e      *gin.Engine
	key    []byte
	users  *fakeUsers
	outbox *fakeOutbox
	now    time.Time
	tenant primitive.ObjectID
	user   primitive.ObjectID
}

func newRig(t *testing.T, limit gin.HandlerFunc) *rig {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := &rig{
		key:    testKey(t),
		outbox: &fakeOutbox{},
		now:    time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC),
		tenant: primitive.NewObjectID(),
		user:   primitive.NewObjectID(),
	}
	r.users = &fakeUsers{known: map[primitive.ObjectID]bool{r.user: true}}
	e := gin.New()
	e.Use(middleware.ErrorHandler(zap.NewNop(), false))
	Register(e.Group("/api/v1/public"), Deps{
		Key: r.key, Users: r.users, Outbox: r.outbox,
		RateLimit: limit, Now: func() time.Time { return r.now }, Log: zap.NewNop(),
	})
	r.e = e
	return r
}

func (r *rig) token(tenant, user primitive.ObjectID, exp time.Time) string {
	return unsubtoken.Sign(r.key, tenant, user, exp)
}

func (r *rig) valid() string { return r.token(r.tenant, r.user, r.now.Add(time.Hour)) }

func (r *rig) postForm(tok string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/public/unsubscribe", strings.NewReader(url.Values{"token": {tok}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.e.ServeHTTP(w, req)
	return w
}

func (r *rig) postJSON(body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/public/unsubscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.e.ServeHTTP(w, req)
	return w
}

func (r *rig) get(tok string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/unsubscribe?token="+url.QueryEscape(tok), nil)
	w := httptest.NewRecorder()
	r.e.ServeHTTP(w, req)
	return w
}

func TestGetDoesNotChangeAnything(t *testing.T) {
	r := newRig(t, nil)
	w := r.get(r.valid())
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if len(r.users.calls) != 0 || len(r.outbox.calls) != 0 {
		t.Fatal("GET must not touch the user or the outbox")
	}
	if !strings.Contains(w.Body.String(), `<form method="post"`) {
		t.Error("GET page should carry a POST form")
	}
	for h, want := range map[string]string{
		"Cache-Control":           "no-store",
		"Referrer-Policy":         "no-referrer",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": wantCSP,
	} {
		if got := w.Header().Get(h); got != want {
			t.Errorf("%s = %q, want %q", h, got, want)
		}
	}
	if strings.Contains(w.Body.String(), "<style") || strings.Contains(w.Body.String(), "style=") {
		t.Error("the page uses inline style, so the CSP must allow it")
	}
}

// The page has no inline style, script or external resource, so style-src is omitted.
const wantCSP = "default-src 'none'; form-action 'self'; frame-ancestors 'none'"

func TestEveryResponseCarriesCSP(t *testing.T) {
	r := newRig(t, nil)
	for name, w := range map[string]*httptest.ResponseRecorder{
		"page":    r.get(r.valid()),
		"done":    r.postForm(r.valid()),
		"invalid": r.postForm("bad"),
	} {
		if got := w.Header().Get("Content-Security-Policy"); got != wantCSP {
			t.Errorf("%s: CSP = %q, want %q", name, got, wantCSP)
		}
	}
}

func TestPostUnsubscribesAndCancelsPending(t *testing.T) {
	r := newRig(t, nil)
	w := r.postForm(r.valid())
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if len(r.users.calls) != 1 || r.users.calls[0].v {
		t.Fatalf("SetReceiveEmails calls = %+v, want one with false", r.users.calls)
	}
	if len(r.outbox.calls) != 1 {
		t.Fatalf("CancelPendingForUser calls = %d, want 1", len(r.outbox.calls))
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("success page should be no-store")
	}
	// Idempotent.
	if w2 := r.postForm(r.valid()); w2.Code != http.StatusOK {
		t.Errorf("repeat status %d", w2.Code)
	}
}

// flipOneChar returns tok with one character in the middle replaced by one that
// is guaranteed to differ. A mid-token character carries all six bits, so the
// decoded bytes always change (unlike the last one, whose low bits may be padding).
func flipOneChar(tok string) string {
	b := []byte(tok)
	for i := len(b) / 2; i < len(b); i++ {
		if b[i] == '.' {
			continue
		}
		if b[i] == 'A' {
			b[i] = 'B'
		} else {
			b[i] = 'A'
		}
		return string(b)
	}
	panic("token has no flippable character")
}

func TestFlipOneCharAlwaysDiffers(t *testing.T) {
	r := newRig(t, nil)
	for i := 0; i < 200; i++ {
		tok := unsubtoken.Sign(r.key, primitive.NewObjectID(), primitive.NewObjectID(), r.now.Add(time.Hour))
		bad := flipOneChar(tok)
		if bad == tok {
			t.Fatalf("flip left the token unchanged: %q", tok)
		}
		if _, _, err := unsubtoken.Verify(r.key, bad, r.now); err == nil {
			t.Fatalf("flipped token still verifies: %q", tok)
		}
	}
}

func TestInvalidTokensAreIdentical(t *testing.T) {
	r := newRig(t, nil)
	good := r.valid()
	otherKey := unsubtoken.Sign(testKey(t), r.tenant, r.user, r.now.Add(time.Hour))
	expired := r.token(r.tenant, r.user, r.now.Add(-time.Hour))
	cases := map[string]string{
		"malformed":    "not-a-token",
		"empty":        "",
		"expired":      expired,
		"tampered":     flipOneChar(good),
		"wrong key":    otherKey,
		"unknown user": r.token(r.tenant, primitive.NewObjectID(), r.now.Add(time.Hour)),
	}
	var refCode int
	var refBody string
	first := true
	for name, tok := range cases {
		w := r.postForm(tok)
		if first {
			refCode, refBody, first = w.Code, w.Body.String(), false
			if refCode != http.StatusBadRequest {
				t.Fatalf("%s: status %d, want 400", name, refCode)
			}
			continue
		}
		if w.Code != refCode || w.Body.String() != refBody {
			t.Errorf("%s differs: %d %q vs %d %q", name, w.Code, w.Body.String(), refCode, refBody)
		}
	}
	if len(r.outbox.calls) != 0 {
		t.Error("an invalid token must not cancel anything")
	}
	// Same for the JSON shape, including a missing token field and bad JSON.
	var jc int
	var jb string
	for i, body := range []string{`{"token":"nope"}`, `{}`, `not json`, `{"token":"` + expired + `"}`} {
		w := r.postJSON(body)
		if i == 0 {
			jc, jb = w.Code, w.Body.String()
			if jc != http.StatusBadRequest {
				t.Fatalf("json status %d, want 400", jc)
			}
			continue
		}
		if w.Code != jc || w.Body.String() != jb {
			t.Errorf("json case %d differs: %d %q vs %d %q", i, w.Code, w.Body.String(), jc, jb)
		}
	}
}

func TestPostAcceptsJSONBody(t *testing.T) {
	r := newRig(t, nil)
	w := r.postJSON(`{"token":"` + r.valid() + `"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Errorf("content type = %q, want json", w.Header().Get("Content-Type"))
	}
	if len(r.users.calls) != 1 || len(r.outbox.calls) != 1 {
		t.Fatal("JSON post should unsubscribe and cancel")
	}
}

func TestUnsubscribeIsRateLimited(t *testing.T) {
	rl := middleware.NewRateLimiter()
	r := newRig(t, rl.Limit("unsubscribe", 1, 2))
	var got []int
	for i := 0; i < 4; i++ {
		got = append(got, r.get("x").Code)
	}
	if got[0] != http.StatusOK || got[len(got)-1] != http.StatusTooManyRequests {
		t.Fatalf("GET codes = %v, want 200 first and 429 last", got)
	}
	// POST shares the limiter, so it is refused too.
	if w := r.postForm(r.valid()); w.Code != http.StatusTooManyRequests {
		t.Errorf("POST after exhausting allowance: %d, want 429", w.Code)
	}
	if len(r.users.calls) != 0 {
		t.Error("a rate limited POST must not change anything")
	}
}

func TestUnsubscribeOnlyAffectsTokenTenantUser(t *testing.T) {
	r := newRig(t, nil)
	otherTenant, otherUser := primitive.NewObjectID(), primitive.NewObjectID()
	r.users.known[otherUser] = true
	r.postForm(r.token(otherTenant, otherUser, r.now.Add(time.Hour)))
	if len(r.users.calls) != 1 || r.users.calls[0].tenant != otherTenant || r.users.calls[0].user != otherUser {
		t.Fatalf("SetReceiveEmails = %+v", r.users.calls)
	}
	if len(r.outbox.calls) != 1 || r.outbox.calls[0].tenant != otherTenant || r.outbox.calls[0].user != otherUser {
		t.Fatalf("CancelPendingForUser = %+v", r.outbox.calls)
	}
}

func TestDatabaseErrorIsGenericAndKeepsOrder(t *testing.T) {
	r := newRig(t, nil)
	r.users.err = errors.New("mongo detail host=db1")
	w := r.postForm(r.valid())
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "mongo") || strings.Contains(w.Body.String(), "db1") {
		t.Error("500 body leaks detail")
	}
	if len(r.outbox.calls) != 0 {
		t.Error("outbox must not be touched when the opt-out failed")
	}
}

func TestRouteAbsentWhenMailNotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	Register(e.Group("/api/v1/public"), Deps{}) // no key
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(m, "/api/v1/public/unsubscribe", nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", m, w.Code)
		}
	}
}

func TestHTMLEscapesToken(t *testing.T) {
	r := newRig(t, nil)
	evil := `"><script>alert(1)</script>`
	body := r.get(evil).Body.String()
	if strings.Contains(body, "<script>") || strings.Contains(body, `"><script`) {
		t.Fatalf("token not escaped: %s", body)
	}
	if !strings.Contains(body, "&gt;&lt;script&gt;") {
		t.Errorf("expected escaped token in the hidden field: %s", body)
	}
}
