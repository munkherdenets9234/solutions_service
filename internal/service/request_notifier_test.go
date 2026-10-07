package service

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/unsubscribe"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

type fakeRecipients struct {
	users []*models.TenantUser
	err   error
	calls int
}

func (f *fakeRecipients) FindEmailRecipients(_ context.Context, _ primitive.ObjectID) ([]*models.TenantUser, error) {
	f.calls++
	return f.users, f.err
}

type fakeEnqueuer struct {
	rows     []*models.MailOutbox
	calls    int
	err      error
	deadline time.Time
	hasDL    bool
}

func (f *fakeEnqueuer) Enqueue(ctx context.Context, rows []*models.MailOutbox) error {
	f.calls++
	f.deadline, f.hasDL = ctx.Deadline()
	f.rows = append(f.rows, rows...)
	return f.err
}

type fakeLinks struct {
	adminURL, siteBase, tenantName string
	err                            error
}

func (f *fakeLinks) Links(_ context.Context, _ primitive.ObjectID, _ NotifyKind, _ string) (string, string, string, error) {
	return f.adminURL, f.siteBase, f.tenantName, f.err
}

var notifierTestNow = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// testKey is assembled at run time so the gitleaks scan stays clean.
func testKey() []byte { return []byte(strings.Repeat("k", 32)) }

func twoUsers() []*models.TenantUser {
	return []*models.TenantUser{
		{ID: primitive.NewObjectID(), Email: "a@example.com"},
		{ID: primitive.NewObjectID(), Email: "b@example.com"},
	}
}

func newTestNotifier(users *fakeRecipients, store *fakeEnqueuer, links *fakeLinks) *RequestNotifier {
	return NewRequestNotifier(users, store, links, testKey(), "Digital Service",
		func() time.Time { return notifierTestNow }, zap.NewNop())
}

func okLinks() *fakeLinks {
	return &fakeLinks{adminURL: "https://admin.example.com/bookings/1", siteBase: "https://site.example.com", tenantName: "Acme Travel"}
}

func TestNotifyEnqueuesOneRowPerRecipient(t *testing.T) {
	users := &fakeRecipients{users: twoUsers()}
	store := &fakeEnqueuer{}
	n := newTestNotifier(users, store, okLinks())
	tenant := primitive.NewObjectID()
	rec := primitive.NewObjectID()

	n.Notify(context.Background(), tenant, NotifyBooking, rec.Hex(), "Trip to Gobi")

	if len(store.rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(store.rows))
	}
	for i, r := range store.rows {
		if r.TenantID != tenant || r.UserID != users.users[i].ID || r.To != users.users[i].Email {
			t.Fatalf("row %d scoped wrongly: %+v", i, r)
		}
		if r.Kind != "booking" || r.RecordID != rec {
			t.Fatalf("row %d kind/record wrong: %+v", i, r)
		}
		if r.Data["summary"] != "Trip to Gobi" || r.Data["tenant"] != "Acme Travel" || r.Data["request_type"] != "booking" {
			t.Fatalf("row %d data wrong: %+v", i, r.Data)
		}
	}
	if store.calls != 1 {
		t.Fatalf("want one batched Enqueue, got %d", store.calls)
	}
}

func TestNotifyNoRecipientsOrNilIsNoOp(t *testing.T) {
	var nilN *RequestNotifier
	nilN.Notify(context.Background(), primitive.NewObjectID(), NotifyBooking, primitive.NewObjectID().Hex(), "x")

	store := &fakeEnqueuer{}
	empty := newTestNotifier(&fakeRecipients{}, store, okLinks())
	empty.Notify(context.Background(), primitive.NewObjectID(), NotifyBooking, primitive.NewObjectID().Hex(), "x")
	if store.calls != 0 {
		t.Fatalf("no recipients must not call Enqueue")
	}

	failing := newTestNotifier(&fakeRecipients{err: errors.New("db down")}, store, okLinks())
	failing.Notify(context.Background(), primitive.NewObjectID(), NotifyBooking, primitive.NewObjectID().Hex(), "x")

	badLinks := newTestNotifier(&fakeRecipients{users: twoUsers()}, store, &fakeLinks{err: errors.New("no host")})
	badLinks.Notify(context.Background(), primitive.NewObjectID(), NotifyBooking, primitive.NewObjectID().Hex(), "x")

	badID := newTestNotifier(&fakeRecipients{users: twoUsers()}, store, okLinks())
	badID.Notify(context.Background(), primitive.NewObjectID(), NotifyBooking, "not-an-id", "x")

	enqErr := newTestNotifier(&fakeRecipients{users: twoUsers()}, &fakeEnqueuer{err: errors.New("boom")}, okLinks())
	enqErr.Notify(context.Background(), primitive.NewObjectID(), NotifyBooking, primitive.NewObjectID().Hex(), "x")

	nilParts := NewRequestNotifier(nil, nil, nil, testKey(), "App", nil, nil)
	nilParts.Notify(context.Background(), primitive.NewObjectID(), NotifyBooking, primitive.NewObjectID().Hex(), "x")

	if store.calls != 0 {
		t.Fatalf("failure paths must not enqueue, got %d calls", store.calls)
	}
}

func TestSummaryIsSingleLineAndCapped(t *testing.T) {
	store := &fakeEnqueuer{}
	links := okLinks()
	links.tenantName = strings.Repeat("T", 400)
	n := NewRequestNotifier(&fakeRecipients{users: twoUsers()[:1]}, store, links, testKey(),
		strings.Repeat("A", 400), func() time.Time { return notifierTestNow }, zap.NewNop())

	n.Notify(context.Background(), primitive.NewObjectID(), NotifyGuide, primitive.NewObjectID().Hex(),
		"line one\r\n  line\ttwo\n"+strings.Repeat("é", 400))

	if len(store.rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(store.rows))
	}
	d := store.rows[0].Data
	if strings.ContainsAny(d["summary"], "\r\n\t") || !strings.HasPrefix(d["summary"], "line one line two ") {
		t.Fatalf("summary not collapsed to one line: %q", d["summary"])
	}
	for _, k := range []string{"app", "tenant", "request_type", "summary", "admin_url"} {
		if got := utf8.RuneCountInString(d[k]); got > 256 {
			t.Fatalf("%s is %d runes, cap is 256", k, got)
		}
	}
	if got := utf8.RuneCountInString(d["unsubscribe_url"]); got > 512 {
		t.Fatalf("unsubscribe_url is %d runes, cap is 512", got)
	}
	if !utf8.ValidString(d["summary"]) {
		t.Fatalf("summary cut inside a rune")
	}
}

func TestDataKeysAreExactlyTemplateKeys(t *testing.T) {
	store := &fakeEnqueuer{}
	newTestNotifier(&fakeRecipients{users: twoUsers()[:1]}, store, okLinks()).
		Notify(context.Background(), primitive.NewObjectID(), NotifyRental, primitive.NewObjectID().Hex(), "s")
	d := store.rows[0].Data
	want := []string{"app", "tenant", "request_type", "summary", "admin_url", "unsubscribe_url"}
	if len(d) != len(want) {
		t.Fatalf("want exactly %v, got %v", want, d)
	}
	for _, k := range want {
		if d[k] == "" {
			t.Fatalf("key %s missing or empty in %v", k, d)
		}
	}
	if d["app"] != "Digital Service" || d["request_type"] != "car rental" || d["admin_url"] != "https://admin.example.com/bookings/1" {
		t.Fatalf("unexpected values: %v", d)
	}
}

func TestUnsubscribeURLCarriesVerifiableToken(t *testing.T) {
	users := twoUsers()[:1]
	store := &fakeEnqueuer{}
	tenant := primitive.NewObjectID()
	newTestNotifier(&fakeRecipients{users: users}, store, okLinks()).
		Notify(context.Background(), tenant, NotifyTransfer, primitive.NewObjectID().Hex(), "s")

	raw := store.rows[0].Data["unsubscribe_url"]
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "site.example.com" || u.Path != "/api/v1/public/unsubscribe" {
		t.Fatalf("unexpected unsubscribe url shape: %s", u.Scheme+"://"+u.Host+u.Path)
	}
	token := u.Query().Get("token")
	gotT, gotU, err := unsubscribe.Verify(testKey(), token, notifierTestNow)
	if err != nil || gotT != tenant || gotU != users[0].ID {
		t.Fatalf("token does not verify to tenant/user: %v", err)
	}
	// TTL is 90 days: valid at 89, expired at 91.
	if _, _, err := unsubscribe.Verify(testKey(), token, notifierTestNow.Add(89*24*time.Hour)); err != nil {
		t.Fatalf("token should still be valid after 89 days: %v", err)
	}
	if _, _, err := unsubscribe.Verify(testKey(), token, notifierTestNow.Add(91*24*time.Hour)); err == nil {
		t.Fatalf("token should be expired after 91 days")
	}
}

func TestOverlongURLSkipsRecipientInsteadOfTruncating(t *testing.T) {
	// unsubscribe_url over 512 runes: no row, never a broken link.
	store := &fakeEnqueuer{}
	long := okLinks()
	long.siteBase = "https://" + strings.Repeat("s", 600) + ".example.com"
	newTestNotifier(&fakeRecipients{users: twoUsers()}, store, long).
		Notify(context.Background(), primitive.NewObjectID(), NotifyBooking, primitive.NewObjectID().Hex(), "s")
	if store.calls != 0 || len(store.rows) != 0 {
		t.Fatalf("overlong unsubscribe_url must skip the recipients, got %d rows", len(store.rows))
	}

	// admin_url over 256 runes: same.
	store2 := &fakeEnqueuer{}
	longAdmin := okLinks()
	longAdmin.adminURL = "https://admin.example.com/" + strings.Repeat("a", 300)
	newTestNotifier(&fakeRecipients{users: twoUsers()}, store2, longAdmin).
		Notify(context.Background(), primitive.NewObjectID(), NotifyBooking, primitive.NewObjectID().Hex(), "s")
	if len(store2.rows) != 0 {
		t.Fatalf("overlong admin_url must skip the recipients, got %d rows", len(store2.rows))
	}
}

// The notify call runs on the visitor's request path, so its budget is short.
func TestNotifyEnqueueBudgetIsTwoSeconds(t *testing.T) {
	store := &fakeEnqueuer{}
	start := time.Now()
	newTestNotifier(&fakeRecipients{users: twoUsers()[:1]}, store, okLinks()).
		Notify(context.Background(), primitive.NewObjectID(), NotifyBooking, primitive.NewObjectID().Hex(), "x")
	if !store.hasDL {
		t.Fatal("enqueue context has no deadline")
	}
	if left := store.deadline.Sub(start); left > 2*time.Second+100*time.Millisecond {
		t.Fatalf("enqueue budget = %v, want at most 2s", left)
	}
}
