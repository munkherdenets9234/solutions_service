package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/pkg/password"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// These pin the rules that make an UNAUTHENTICATED endpoint safe. Every one of
// them is invisible in a happy-path click-through, which is why they are worth
// asserting: the flow works fine with all of them broken.

const resetEmail = "enkhjin@example.com"

// ── fakes ────────────────────────────────────────────────────────────────

// resetFakeUsers holds users per tenant, as the real repository does: the same
// email can exist in two tenants.
type resetFakeUsers struct {
	mu      sync.Mutex
	users   map[string]*models.TenantUser // key: tenant hex + "/" + email
	updated map[primitive.ObjectID]string
}

func resetKey(tenant primitive.ObjectID, email string) string { return tenant.Hex() + "/" + email }

func (f *resetFakeUsers) add(tenant primitive.ObjectID, email, name string, status models.TenantUserStatus) *models.TenantUser {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.users == nil {
		f.users = map[string]*models.TenantUser{}
	}
	u := &models.TenantUser{ID: primitive.NewObjectID(), TenantID: tenant, Name: name, Email: email, Status: status}
	f.users[resetKey(tenant, email)] = u
	return u
}

func (f *resetFakeUsers) FindByTenantAndEmail(_ context.Context, tenant primitive.ObjectID, email string) (*models.TenantUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[resetKey(tenant, email)]; ok {
		c := *u
		return &c, nil
	}
	return nil, mongo.ErrNoDocuments
}

func (f *resetFakeUsers) UpdatePassword(_ context.Context, _ primitive.ObjectID, id primitive.ObjectID, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updated == nil {
		f.updated = map[primitive.ObjectID]string{}
	}
	f.updated[id] = hash
	return nil
}

func (f *resetFakeUsers) suspend(tenant primitive.ObjectID, email string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[resetKey(tenant, email)].Status = models.TenantUserSuspended
}

// resetFakeCodes stores codes keyed like the real repository: by tenant and
// email, newest wins.
type resetFakeCodes struct {
	mu          sync.Mutex
	codes       []*models.TenantPasswordReset
	invalidated int
	attempts    int
}

func (f *resetFakeCodes) Create(_ context.Context, p *models.TenantPasswordReset) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	for _, c := range f.codes {
		if c.TenantID == p.TenantID && c.UserID == p.UserID && c.UsedAt == nil {
			c.UsedAt = &now
		}
	}
	p.ID = primitive.NewObjectID()
	f.codes = append(f.codes, p)
	return nil
}

func (f *resetFakeCodes) FindActive(_ context.Context, tenant primitive.ObjectID, email string) (*models.TenantPasswordReset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.codes) - 1; i >= 0; i-- {
		c := f.codes[i]
		if c.TenantID == tenant && c.Email == email && c.UsedAt == nil && c.ExpiresAt.After(time.Now()) {
			cp := *c
			return &cp, nil
		}
	}
	return nil, mongo.ErrNoDocuments
}

func (f *resetFakeCodes) RecordAttempt(_ context.Context, id primitive.ObjectID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	for _, c := range f.codes {
		if c.ID == id {
			c.Attempts++
		}
	}
	return nil
}

func (f *resetFakeCodes) MarkUsed(_ context.Context, id primitive.ObjectID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.codes {
		if c.ID == id {
			if c.UsedAt != nil {
				return false, nil
			}
			now := time.Now()
			c.UsedAt = &now
			return true, nil
		}
	}
	return false, nil
}

func (f *resetFakeCodes) InvalidateForUser(_ context.Context, tenant, user primitive.ObjectID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invalidated++
	now := time.Now()
	for _, c := range f.codes {
		if c.TenantID == tenant && c.UserID == user && c.UsedAt == nil {
			c.UsedAt = &now
		}
	}
	return nil
}

func (f *resetFakeCodes) latest() *models.TenantPasswordReset {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.codes) == 0 {
		return nil
	}
	return f.codes[len(f.codes)-1]
}

func (f *resetFakeCodes) expireAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.codes {
		c.ExpiresAt = time.Now().Add(-time.Minute)
	}
}

type resetFakeTenants map[primitive.ObjectID]*models.Tenant

func (f resetFakeTenants) FindByID(_ context.Context, id primitive.ObjectID) (*models.Tenant, error) {
	if t, ok := f[id]; ok {
		return t, nil
	}
	return nil, mongo.ErrNoDocuments
}

type resetSent struct {
	to, template string
	data         map[string]string
}

// resetFakeMail records sends; gate, when set, blocks every Send until closed.
type resetFakeMail struct {
	mu        sync.Mutex
	available bool
	gate      chan struct{}
	sent      []resetSent
}

func (f *resetFakeMail) Available() bool { return f.available }
func (f *resetFakeMail) Send(_ context.Context, to, template string, data map[string]string) error {
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, resetSent{to, template, data})
	return nil
}
func (f *resetFakeMail) all() []resetSent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]resetSent(nil), f.sent...)
}

// slowResetUsers blocks the account lookup until released.
type slowResetUsers struct {
	*resetFakeUsers
	release chan struct{}
}

func (s slowResetUsers) FindByTenantAndEmail(ctx context.Context, tenant primitive.ObjectID, email string) (*models.TenantUser, error) {
	<-s.release
	return s.resetFakeUsers.FindByTenantAndEmail(ctx, tenant, email)
}

// ── fixture ──────────────────────────────────────────────────────────────

type resetFixture struct {
	svc     *TenantPasswordResetService
	users   *resetFakeUsers
	codes   *resetFakeCodes
	mail    *resetFakeMail
	tenantA primitive.ObjectID
	tenantB primitive.ObjectID
}

func newResetFixture() *resetFixture {
	f := &resetFixture{
		users:   &resetFakeUsers{},
		codes:   &resetFakeCodes{},
		mail:    &resetFakeMail{available: true},
		tenantA: primitive.NewObjectID(),
		tenantB: primitive.NewObjectID(),
	}
	tenants := resetFakeTenants{
		f.tenantA: {ID: f.tenantA, Name: "E and S Discovery Mongolia"},
		f.tenantB: {ID: f.tenantB, Name: "Other Tenant"},
	}
	f.users.add(f.tenantA, resetEmail, "Enkhjin", models.TenantUserActive)
	f.svc = NewTenantPasswordResetService(f.users, f.codes, tenants, f.mail, zap.NewNop())
	return f
}

// request runs Request then waits for the background work, because Request
// deliberately does not wait for it.
func (f *resetFixture) request(tenant primitive.ObjectID, email string) error {
	err := f.svc.Request(context.Background(), tenant, email)
	f.svc.Drain()
	return err
}

// codeFromMail is the code the user would read off the email.
func (f *resetFixture) codeFromMail(t *testing.T) string {
	t.Helper()
	sent := f.mail.all()
	if len(sent) == 0 {
		t.Fatal("no mail was sent")
	}
	return sent[len(sent)-1].data["code"]
}

// ── Request ──────────────────────────────────────────────────────────────

func TestResetRequestMailsACodeToAnActiveUser(t *testing.T) {
	f := newResetFixture()
	if err := f.request(f.tenantA, resetEmail); err != nil {
		t.Fatalf("Request: %v", err)
	}
	sent := f.mail.all()
	if len(sent) != 1 {
		t.Fatalf("mails = %d, want 1", len(sent))
	}
	m := sent[0]
	if m.to != resetEmail || m.template != "password_reset_code" {
		t.Fatalf("mail = %+v", m)
	}
	// The email names the TENANT, not the platform: this is E&S's admin.
	if m.data["app"] != "E and S Discovery Mongolia admin" || m.data["name"] != "Enkhjin" || m.data["expires_in"] != "10 minutes" {
		t.Fatalf("data = %+v", m.data)
	}
	code := m.data["code"]
	if len(code) != 6 {
		t.Fatalf("code = %q, want six digits", code)
	}

	stored := f.codes.latest()
	// A database leak must not hand over live codes.
	if stored.CodeHash == code || stored.CodeHash != hashResetCode(code) {
		t.Fatal("the code must be stored as its hash, never in plaintext")
	}
	if !stored.ExpiresAt.After(time.Now()) {
		t.Fatal("stored code is already expired")
	}
}

func TestResetRequestRevealsNothingAboutTheAccount(t *testing.T) {
	t.Run("unknown address", func(t *testing.T) {
		f := newResetFixture()
		if err := f.request(f.tenantA, "nobody@example.com"); err != nil {
			t.Fatalf("an unknown address must not error, got %v", err)
		}
		if len(f.mail.all()) != 0 {
			t.Fatal("mail was sent to an address with no account")
		}
	})
	t.Run("suspended user", func(t *testing.T) {
		f := newResetFixture()
		f.users.suspend(f.tenantA, resetEmail)
		if err := f.request(f.tenantA, resetEmail); err != nil {
			t.Fatalf("a suspended user must not error, got %v", err)
		}
		if len(f.mail.all()) != 0 {
			t.Fatal("a suspended user was mailed a way back in")
		}
	})
}

// The one answer allowed to differ, because it depends on deployment and never
// on the account: nothing the caller does can produce a code.
func TestResetRequestReportsWhenMailIsOff(t *testing.T) {
	f := newResetFixture()
	f.mail.available = false
	if err := f.svc.Request(context.Background(), f.tenantA, resetEmail); err == nil {
		t.Fatal("expected an error when the mail link is not configured")
	}
}

// The response time of an unauthenticated endpoint must not depend on whether
// the address has an account. Sending takes seconds and an unknown address
// sends nothing; the lookup and insert are database round trips an unknown
// address also skips. On the platform-admin version both measured live.
func TestResetRequestDoesNotWaitForTheMail(t *testing.T) {
	f := newResetFixture()
	f.mail.gate = make(chan struct{})

	done := make(chan error, 1)
	go func() { done <- f.svc.Request(context.Background(), f.tenantA, resetEmail) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Request: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Request waited for the mail: its response time reveals whether the account exists")
	}

	close(f.mail.gate)
	f.svc.Drain()
	if len(f.mail.all()) != 1 {
		t.Fatalf("mails = %d, want 1 after the send completed", len(f.mail.all()))
	}
}

func TestResetRequestDoesNotWaitForTheAccountLookup(t *testing.T) {
	f := newResetFixture()
	slow := slowResetUsers{resetFakeUsers: f.users, release: make(chan struct{})}
	svc := NewTenantPasswordResetService(slow, f.codes, resetFakeTenants{f.tenantA: {ID: f.tenantA, Name: "T"}}, f.mail, zap.NewNop())

	done := make(chan error, 1)
	go func() { done <- svc.Request(context.Background(), f.tenantA, resetEmail) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Request: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Request waited for the account lookup: its response time reveals whether the account exists")
	}

	close(slow.release)
	svc.Drain()
	if len(f.mail.all()) != 1 {
		t.Fatalf("mails = %d, want 1 once the lookup finished", len(f.mail.all()))
	}
}

// ── Confirm ──────────────────────────────────────────────────────────────

func TestResetConfirmSetsThePasswordAndConsumesTheCode(t *testing.T) {
	f := newResetFixture()
	_ = f.request(f.tenantA, resetEmail)
	code := f.codeFromMail(t)

	if err := f.svc.Confirm(context.Background(), f.tenantA, resetEmail, code, "a-new-strong-password"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	f.svc.Drain()

	user, _ := f.users.FindByTenantAndEmail(context.Background(), f.tenantA, resetEmail)
	hash, ok := f.users.updated[user.ID]
	if !ok || !password.Verify(hash, "a-new-strong-password") {
		t.Fatal("the stored hash does not verify against the new password")
	}
	if f.codes.latest().UsedAt == nil {
		t.Fatal("the code was not consumed")
	}
	// The owner must hear about it: a change they did not make is the one
	// thing they need to know immediately.
	sent := f.mail.all()
	if len(sent) != 2 || sent[1].template != "password_changed" {
		t.Fatalf("mails = %+v, want a password_changed notice second", sent)
	}
}

func TestResetConfirmRejectsAReusedCode(t *testing.T) {
	f := newResetFixture()
	_ = f.request(f.tenantA, resetEmail)
	code := f.codeFromMail(t)
	if err := f.svc.Confirm(context.Background(), f.tenantA, resetEmail, code, "a-new-strong-password"); err != nil {
		t.Fatalf("first Confirm: %v", err)
	}
	if err := f.svc.Confirm(context.Background(), f.tenantA, resetEmail, code, "another-password-x"); err == nil {
		t.Fatal("a consumed code was accepted a second time")
	}
}

func TestResetConfirmRejectsAnExpiredCode(t *testing.T) {
	f := newResetFixture()
	_ = f.request(f.tenantA, resetEmail)
	code := f.codeFromMail(t)
	f.codes.expireAll()
	if err := f.svc.Confirm(context.Background(), f.tenantA, resetEmail, code, "a-new-strong-password"); err == nil {
		t.Fatal("an expired code was accepted")
	}
}

// Six digits is only safe because guessing is capped.
func TestResetConfirmBurnsTheCodeAfterTooManyWrongGuesses(t *testing.T) {
	f := newResetFixture()
	_ = f.request(f.tenantA, resetEmail)
	code := f.codeFromMail(t)

	for i := 0; i < models.MaxResetAttempts; i++ {
		if err := f.svc.Confirm(context.Background(), f.tenantA, resetEmail, "000000", "a-new-strong-password"); err == nil {
			t.Fatal("a wrong code was accepted")
		}
	}
	if err := f.svc.Confirm(context.Background(), f.tenantA, resetEmail, code, "a-new-strong-password"); err == nil {
		t.Fatal("the correct code still worked after the attempt cap was reached")
	}
}

// Every failure must look the same from outside, or the differences become a
// map of which addresses have accounts and which codes were close.
func TestResetConfirmFailuresAreIndistinguishable(t *testing.T) {
	f := newResetFixture()
	_ = f.request(f.tenantA, resetEmail)

	wrongCode := f.svc.Confirm(context.Background(), f.tenantA, resetEmail, "000000", "a-new-strong-password")
	noAccount := f.svc.Confirm(context.Background(), f.tenantA, "nobody@example.com", "000000", "a-new-strong-password")
	if wrongCode == nil || noAccount == nil {
		t.Fatal("both cases should fail")
	}
	if wrongCode.Error() != noAccount.Error() {
		t.Fatalf("failures are distinguishable:\n  wrong code: %v\n  no account: %v", wrongCode, noAccount)
	}
}

func TestResetConfirmRejectsAShortPassword(t *testing.T) {
	f := newResetFixture()
	_ = f.request(f.tenantA, resetEmail)
	code := f.codeFromMail(t)
	if err := f.svc.Confirm(context.Background(), f.tenantA, resetEmail, code, "short"); err == nil {
		t.Fatal("a short password was accepted")
	}
}

func TestResetEmailIsCaseInsensitive(t *testing.T) {
	f := newResetFixture()
	if err := f.request(f.tenantA, "  Enkhjin@Example.COM  "); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if len(f.mail.all()) != 1 {
		t.Fatal("a differently-cased address did not resolve to the account")
	}
	if err := f.svc.Confirm(context.Background(), f.tenantA, resetEmail, f.codeFromMail(t), "a-new-strong-password"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
}

// Review Focus 1. Users are unique per tenant, not globally, so two tenants can
// each have a user with this email. A code issued for one must be useless
// against the other.
func TestResetConfirmRejectsACodeFromAnotherTenant(t *testing.T) {
	f := newResetFixture()
	f.users.add(f.tenantB, resetEmail, "Someone Else", models.TenantUserActive)

	_ = f.request(f.tenantA, resetEmail)
	code := f.codeFromMail(t)

	if err := f.svc.Confirm(context.Background(), f.tenantB, resetEmail, code, "a-new-strong-password"); err == nil {
		t.Fatal("a code issued for tenant A reset a password in tenant B")
	}
	userB, _ := f.users.FindByTenantAndEmail(context.Background(), f.tenantB, resetEmail)
	if _, changed := f.users.updated[userB.ID]; changed {
		t.Fatal("tenant B's user password was changed")
	}
}

// Review Focus 2. Suspension exists to keep someone out; a code issued before it
// must not get them back in.
func TestResetConfirmRefusesAUserSuspendedAfterTheCodeWasIssued(t *testing.T) {
	f := newResetFixture()
	_ = f.request(f.tenantA, resetEmail)
	code := f.codeFromMail(t)

	f.users.suspend(f.tenantA, resetEmail)

	if err := f.svc.Confirm(context.Background(), f.tenantA, resetEmail, code, "a-new-strong-password"); err == nil {
		t.Fatal("a suspended user reset their password with a code issued before the suspension")
	}
	user, _ := f.users.FindByTenantAndEmail(context.Background(), f.tenantA, resetEmail)
	if _, changed := f.users.updated[user.ID]; changed {
		t.Fatal("the suspended user's password was changed")
	}
}

// A lookup failure inside the background work is logged, not returned: it must
// not panic and must not send anything.
func TestResetRequestSurvivesAStoreFailure(t *testing.T) {
	f := newResetFixture()
	svc := NewTenantPasswordResetService(failingResetUsers{}, f.codes, resetFakeTenants{}, f.mail, zap.NewNop())
	if err := svc.Request(context.Background(), f.tenantA, resetEmail); err != nil {
		t.Fatalf("Request: %v", err)
	}
	svc.Drain()
	if len(f.mail.all()) != 0 {
		t.Fatal("mail was sent after the account lookup failed")
	}
}

type failingResetUsers struct{}

func (failingResetUsers) FindByTenantAndEmail(context.Context, primitive.ObjectID, string) (*models.TenantUser, error) {
	return nil, errors.New("mongo: connection refused")
}
func (failingResetUsers) UpdatePassword(context.Context, primitive.ObjectID, primitive.ObjectID, string) error {
	return errors.New("mongo: connection refused")
}

// tenantBlindCodes is a code store that ignores the tenant, like a repository
// whose filter lost its tenant_id. The repository filter has its own test, but a
// service that trusts its store completely is one bug away from resetting
// another tenant's user. The service re-reads the user in the tenant it was
// asked about and requires it to be the user the code was issued for, so even a
// tenant-blind store cannot turn tenant A's code into a reset for tenant B.
type tenantBlindCodes struct{ *resetFakeCodes }

func (t tenantBlindCodes) FindActive(_ context.Context, _ primitive.ObjectID, email string) (*models.TenantPasswordReset, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := len(t.codes) - 1; i >= 0; i-- {
		c := t.codes[i]
		if c.Email == email && c.UsedAt == nil && c.ExpiresAt.After(time.Now()) {
			cp := *c
			return &cp, nil
		}
	}
	return nil, mongo.ErrNoDocuments
}

func TestResetConfirmRejectsACodeFromAnotherTenantEvenIfTheStoreIsTenantBlind(t *testing.T) {
	f := newResetFixture()
	userB := f.users.add(f.tenantB, resetEmail, "Someone Else", models.TenantUserActive)

	_ = f.request(f.tenantA, resetEmail)
	code := f.codeFromMail(t)

	tenants := resetFakeTenants{f.tenantA: {ID: f.tenantA, Name: "A"}, f.tenantB: {ID: f.tenantB, Name: "B"}}
	svc := NewTenantPasswordResetService(f.users, tenantBlindCodes{f.codes}, tenants, f.mail, zap.NewNop())

	if err := svc.Confirm(context.Background(), f.tenantB, resetEmail, code, "a-new-strong-password"); err == nil {
		t.Fatal("tenant A's code reset a password in tenant B through a tenant-blind store")
	}
	if _, changed := f.users.updated[userB.ID]; changed {
		t.Fatal("tenant B's user password was changed")
	}
}
