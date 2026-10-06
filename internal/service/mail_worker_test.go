package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/notify"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

var workerNow = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

type failedMark struct {
	id       primitive.ObjectID
	code     string
	attempts int
	nextAt   time.Time
	final    bool
}

type fakeWorkStore struct {
	queue    []*models.MailOutbox
	claims   int
	lease    time.Duration
	sent     []primitive.ObjectID
	failed   []failedMark
	claimErr error
}

func (f *fakeWorkStore) ClaimDue(_ context.Context, _ time.Time, lease time.Duration) (*models.MailOutbox, error) {
	f.claims++
	f.lease = lease
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	if len(f.queue) == 0 {
		return nil, nil
	}
	r := f.queue[0]
	f.queue = f.queue[1:]
	return r, nil
}

func (f *fakeWorkStore) MarkSent(_ context.Context, id primitive.ObjectID, _ time.Time) error {
	f.sent = append(f.sent, id)
	return nil
}

func (f *fakeWorkStore) MarkAttemptFailed(_ context.Context, id primitive.ObjectID, code string, attempts int, nextAt time.Time, final bool) error {
	f.failed = append(f.failed, failedMark{id, code, attempts, nextAt, final})
	return nil
}

type fakeMail struct {
	err    error
	calls  []string
	tmpl   string
	onSend func()
}

func (f *fakeMail) Send(_ context.Context, to, template string, _ map[string]string) error {
	f.calls = append(f.calls, to)
	f.tmpl = template
	if f.onSend != nil {
		f.onSend()
	}
	return f.err
}

type fakeChecker struct {
	user *models.TenantUser
	err  error
}

func (f *fakeChecker) FindByID(_ context.Context, _, _ primitive.ObjectID) (*models.TenantUser, error) {
	return f.user, f.err
}

func activeUser() *fakeChecker {
	return &fakeChecker{user: &models.TenantUser{Status: models.TenantUserActive, ReceiveEmails: true}}
}

func dueRow(attempts int) *models.MailOutbox {
	return &models.MailOutbox{
		ID: primitive.NewObjectID(), TenantID: primitive.NewObjectID(), UserID: primitive.NewObjectID(),
		To: "a@example.com", Kind: "booking", Attempts: attempts,
		Data: map[string]string{"summary": "s"},
	}
}

func newWorker(store *fakeWorkStore, mail *fakeMail, users *fakeChecker) *MailWorker {
	return NewMailWorker(store, mail, users, func() time.Time { return workerNow }, zap.NewNop())
}

func TestTickSendsDueRowAndMarksSent(t *testing.T) {
	row := dueRow(0)
	store := &fakeWorkStore{queue: []*models.MailOutbox{row}}
	mail := &fakeMail{}
	newWorker(store, mail, activeUser()).Tick(context.Background())

	if len(mail.calls) != 1 || mail.calls[0] != "a@example.com" || mail.tmpl != "request_notification" {
		t.Fatalf("send wrong: %v %q", mail.calls, mail.tmpl)
	}
	if len(store.sent) != 1 || store.sent[0] != row.ID || len(store.failed) != 0 {
		t.Fatalf("row not marked sent: sent=%v failed=%v", store.sent, store.failed)
	}
	if store.lease != 5*time.Minute {
		t.Fatalf("lease must be 5m (above the 30s send timeout), got %v", store.lease)
	}
}

func TestNextAttemptDelay(t *testing.T) {
	want := map[int]time.Duration{1: time.Minute, 2: 5 * time.Minute, 3: 30 * time.Minute, 4: 2 * time.Hour}
	for n, d := range want {
		if got := nextAttemptDelay(n); got != d {
			t.Fatalf("delay(%d)=%v want %v", n, got, d)
		}
	}
}

func TestTickRetriesWithBackoff(t *testing.T) {
	delays := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}
	for prior := 0; prior < 4; prior++ {
		row := dueRow(prior)
		store := &fakeWorkStore{queue: []*models.MailOutbox{row}}
		newWorker(store, &fakeMail{err: errors.New("boom")}, activeUser()).Tick(context.Background())
		if len(store.failed) != 1 || len(store.sent) != 0 {
			t.Fatalf("prior=%d: want one failure mark, got %+v", prior, store.failed)
		}
		f := store.failed[0]
		if f.attempts != prior+1 || f.final || !f.nextAt.Equal(workerNow.Add(delays[prior])) {
			t.Fatalf("prior=%d: got %+v want attempts=%d next=%v", prior, f, prior+1, workerNow.Add(delays[prior]))
		}
	}
}

func TestTickFailsAfterFifthAttempt(t *testing.T) {
	store := &fakeWorkStore{queue: []*models.MailOutbox{dueRow(4)}}
	newWorker(store, &fakeMail{err: errors.New("boom")}, activeUser()).Tick(context.Background())
	if len(store.failed) != 1 || store.failed[0].attempts != 5 || !store.failed[0].final {
		t.Fatalf("fifth failure must be final: %+v", store.failed)
	}
}

func TestTickSkipsUnsubscribedOrSuspendedUser(t *testing.T) {
	cases := map[string]*fakeChecker{
		"opted out": {user: &models.TenantUser{Status: models.TenantUserActive, ReceiveEmails: false}},
		"suspended": {user: &models.TenantUser{Status: "suspended", ReceiveEmails: true}},
		"missing":   {err: mongo.ErrNoDocuments},
		"nil user":  {},
	}
	for name, users := range cases {
		store := &fakeWorkStore{queue: []*models.MailOutbox{dueRow(1)}}
		mail := &fakeMail{}
		newWorker(store, mail, users).Tick(context.Background())
		if len(mail.calls) != 0 {
			t.Fatalf("%s: must not send", name)
		}
		if len(store.failed) != 1 || store.failed[0].code != "recipient_opted_out" || !store.failed[0].final || store.failed[0].attempts != 2 {
			t.Fatalf("%s: want final recipient_opted_out, got %+v", name, store.failed)
		}
	}
}

func TestTickLeavesRowOnTransientUserLookupError(t *testing.T) {
	store := &fakeWorkStore{queue: []*models.MailOutbox{dueRow(0)}}
	mail := &fakeMail{}
	newWorker(store, mail, &fakeChecker{err: errors.New("db down")}).Tick(context.Background())
	if len(mail.calls) != 0 || len(store.failed) != 0 || len(store.sent) != 0 {
		t.Fatalf("transient lookup error must neither send nor record; the lease lets it retry")
	}
}

func TestMailNotConfiguredRetries(t *testing.T) {
	store := &fakeWorkStore{queue: []*models.MailOutbox{dueRow(0)}}
	err := fmt.Errorf("wrapped: %w", notify.ErrMailNotConfigured)
	newWorker(store, &fakeMail{err: err}, activeUser()).Tick(context.Background())
	if len(store.failed) != 1 || store.failed[0].code != "mail_not_configured" || store.failed[0].final {
		t.Fatalf("want retryable mail_not_configured, got %+v", store.failed)
	}
}

func TestErrorCode(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{notify.ErrMailNotConfigured, "mail_not_configured"},
		{fmt.Errorf("x: %w", notify.ErrRateLimited), "rate_limited"},
		{context.DeadlineExceeded, "unreachable"},
		{fmt.Errorf("notify: %w", &net.DNSError{Err: "no such host"}), "unreachable"},
		{errors.New("notify: tenantcore answered 500"), "upstream"},
	}
	for _, c := range cases {
		if got := errorCode(c.err); got != c.want {
			t.Fatalf("errorCode(%v)=%q want %q", c.err, got, c.want)
		}
	}
}

func TestLastErrorNeverContainsProviderText(t *testing.T) {
	secret := "smtp password hunter2 leaked to bob@example.com"
	store := &fakeWorkStore{queue: []*models.MailOutbox{dueRow(0)}}
	newWorker(store, &fakeMail{err: errors.New(secret)}, activeUser()).Tick(context.Background())
	if len(store.failed) != 1 {
		t.Fatalf("want a failure mark")
	}
	code := store.failed[0].code
	if code != "upstream" || strings.Contains(code, "hunter2") || strings.Contains(code, "@") {
		t.Fatalf("stored code leaks provider text: %q", code)
	}
}

func TestNilMailSenderLeavesRowsPending(t *testing.T) {
	store := &fakeWorkStore{queue: []*models.MailOutbox{dueRow(0)}}
	w := NewMailWorker(store, nil, activeUser(), func() time.Time { return workerNow }, zap.NewNop())
	w.Tick(context.Background())
	if store.claims != 0 || len(store.queue) != 1 || len(store.failed) != 0 || len(store.sent) != 0 {
		t.Fatalf("nil mail sender must not claim or touch rows")
	}
	// An unconfigured *notify.Client behind the interface is the same state.
	var c *notify.Client
	w2 := NewMailWorker(store, c, activeUser(), func() time.Time { return workerNow }, zap.NewNop())
	w2.Tick(context.Background())
	if store.claims != 0 {
		t.Fatalf("unconfigured notify client must not claim rows")
	}
	var nilW *MailWorker
	nilW.Tick(context.Background())
	NewMailWorker(nil, &fakeMail{}, activeUser(), nil, nil).Tick(context.Background())
}

func TestTickCapsRowsPerTick(t *testing.T) {
	store := &fakeWorkStore{}
	for i := 0; i < 60; i++ {
		store.queue = append(store.queue, dueRow(0))
	}
	newWorker(store, &fakeMail{}, activeUser()).Tick(context.Background())
	if len(store.sent) != 50 {
		t.Fatalf("want 50 sent per tick, got %d", len(store.sent))
	}
}

func TestRunTicksImmediatelyAndStopsWhenContextDone(t *testing.T) {
	store := &fakeWorkStore{queue: []*models.MailOutbox{dueRow(0)}}
	sent := make(chan struct{}, 1)
	mail := &fakeMail{onSend: func() { sent <- struct{}{} }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w := newWorker(store, mail, activeUser())
	go func() { w.Run(ctx); close(done) }()
	select {
	case <-sent:
	case <-time.After(2 * time.Second):
		t.Fatal("first tick did not run immediately")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
