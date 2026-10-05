package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/notify"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/password"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// resetCodeTTL is how long a code lives. Long enough to find the mail on a
// phone, short enough that a code glimpsed over a shoulder is worthless by the
// time anyone acts on it.
const resetCodeTTL = 10 * time.Minute

// The reads and writes this flow needs, as narrow interfaces rather than the
// concrete repositories, for the same reason as the platform-admin version in
// tenantcore: these rules (one answer for every failure, a capped attempt
// count, single use, expiry, nothing depending on the account) are the entire
// security of an unauthenticated endpoint, and rules that can only be tested
// against a running stack stop being tested.
type (
	resetUsers interface {
		FindByTenantAndEmail(ctx context.Context, tenantID primitive.ObjectID, email string) (*models.TenantUser, error)
		UpdatePassword(ctx context.Context, tenantID, id primitive.ObjectID, passwordHash string) error
	}
	resetCodes interface {
		Create(ctx context.Context, p *models.TenantPasswordReset) error
		FindActive(ctx context.Context, tenantID primitive.ObjectID, email string) (*models.TenantPasswordReset, error)
		RecordAttempt(ctx context.Context, id primitive.ObjectID) error
		MarkUsed(ctx context.Context, id primitive.ObjectID) (bool, error)
		InvalidateForUser(ctx context.Context, tenantID, userID primitive.ObjectID) error
	}
	resetTenants interface {
		FindByID(ctx context.Context, id primitive.ObjectID) (*models.Tenant, error)
	}
	resetMailer interface {
		Available() bool
		Send(ctx context.Context, to, template string, data map[string]string) error
	}
)

// TenantPasswordResetService runs the emailed-code reset for a tenant's admin
// users.
//
// digitalservice owns this because it owns the users; tenantcore is only the
// sender. The rules are the same as the platform-admin reset, and so is the
// reason for the shape of Request.
type TenantPasswordResetService struct {
	users   resetUsers
	codes   resetCodes
	tenants resetTenants
	mail    resetMailer
	log     *zap.Logger

	// background tracks the work Request starts after it has responded.
	background sync.WaitGroup
}

func NewTenantPasswordResetService(users resetUsers, codes resetCodes, tenants resetTenants, mail resetMailer, log *zap.Logger) *TenantPasswordResetService {
	return &TenantPasswordResetService{users: users, codes: codes, tenants: tenants, mail: mail, log: log}
}

// Drain waits for the background work Request started. Called at shutdown, so a
// reset requested a moment before a restart is still delivered, and by tests.
func (s *TenantPasswordResetService) Drain() { s.background.Wait() }

// Request issues a code and mails it, if the address belongs to an active user
// of this tenant.
//
// It returns before doing ANY work that depends on the account. The body of the
// response is identical for a real and an unknown address, but a response TIME
// that differs is just as much an oracle: on the platform-admin version, waiting
// for the SMTP send made a real account answer 1.7s later than a fake one, and
// with only the send moved out the lookup, the insert and the invalidation (the
// database round trips an unknown address skips) still left about 250ms, which
// is averaged out in a few dozen requests. Both were measured live. So the
// lookup, the insert and the mail all happen in the background, and both paths
// do the same amount of work here: none.
//
// The only error is mail being unconfigured. That depends on deployment, never
// on the account, and hiding it would leave the caller waiting for an email that
// can never arrive.
func (s *TenantPasswordResetService) Request(_ context.Context, tenantID primitive.ObjectID, email string) error {
	email = normalizeResetEmail(email)

	if !s.mail.Available() {
		return apierr.FeatureUnavailable("email")
	}

	s.background.Add(1)
	go func() {
		defer s.background.Done()
		// The request's context is cancelled the moment the handler returns, so
		// the background work gets its own, bounded one.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.issue(ctx, tenantID, email)
	}()
	return nil
}

// issue looks the user up, stores a code and mails it. It runs after the
// response has gone, so it logs rather than returns.
func (s *TenantPasswordResetService) issue(ctx context.Context, tenantID primitive.ObjectID, email string) {
	user, err := s.users.FindByTenantAndEmail(ctx, tenantID, email)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			s.log.Info("tenant password reset requested for an unknown address",
				zap.String("tenant", tenantID.Hex()), zap.String("email", email))
			return
		}
		s.log.Error("tenant password reset: account lookup failed", zap.String("email", email), zap.Error(err))
		return
	}
	if user.Status != models.TenantUserActive {
		// A suspended user getting back in by email is precisely what
		// suspension exists to prevent.
		s.log.Warn("tenant password reset requested for a non-active user",
			zap.String("email", email), zap.String("status", string(user.Status)))
		return
	}

	if err := s.issueCode(ctx, tenantID, user); err != nil {
		s.log.Error("tenant password reset: "+err.Error(), zap.String("email", email))
	}
}

// RequestNow is the operator path: the same steps as issue (store a code, mail
// it), run synchronously on the caller's context and RETURNING what goes wrong.
//
// Request answers before doing anything because its caller is anonymous and a
// slow or failing answer would tell them which addresses exist. A superadmin
// resetting a named user has no such adversary, and needs the opposite: a mail
// that cannot go out must not read as "a reset code was emailed". The user is
// the one the caller already loaded, so the address never comes from the request.
func (s *TenantPasswordResetService) RequestNow(ctx context.Context, tenantID primitive.ObjectID, user *models.TenantUser) error {
	if !s.mail.Available() {
		return apierr.FeatureUnavailable("email")
	}
	if user.Status != models.TenantUserActive {
		return apierr.Conflict("this admin account is suspended")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := s.issueCode(ctx, tenantID, user); err != nil {
		if errors.Is(err, notify.ErrMailNotConfigured) {
			return apierr.FeatureUnavailable("email")
		}
		s.log.Error("operator password reset failed", zap.String("email", user.Email), zap.Error(err))
		return apierr.Upstream(apierr.DomainGeneral, err)
	}
	return nil
}

// issueCode stores a fresh code for user and mails it. Shared by the background
// path (which logs the error) and RequestNow (which returns it).
func (s *TenantPasswordResetService) issueCode(ctx context.Context, tenantID primitive.ObjectID, user *models.TenantUser) error {
	email := normalizeResetEmail(user.Email)
	code, err := generateResetCode()
	if err != nil {
		return fmt.Errorf("could not generate a code: %w", err)
	}
	reset := &models.TenantPasswordReset{
		TenantID:  tenantID,
		UserID:    user.ID,
		Email:     email,
		CodeHash:  hashResetCode(code),
		ExpiresAt: time.Now().Add(resetCodeTTL),
	}
	if err := s.codes.Create(ctx, reset); err != nil {
		return fmt.Errorf("could not store the code: %w", err)
	}
	if err := s.mail.Send(ctx, email, "password_reset_code", map[string]string{
		"app":        s.appName(ctx, tenantID),
		"name":       greetingName(user),
		"code":       code,
		"expires_in": "10 minutes",
	}); err != nil {
		return fmt.Errorf("code could not be mailed: %w", err)
	}
	return nil
}

// appName is what the email calls the product: the tenant's own name, so a
// user at E&S is told "E and S Discovery Mongolia admin" rather than something
// about the platform they have never heard of.
func (s *TenantPasswordResetService) appName(ctx context.Context, tenantID primitive.ObjectID) string {
	t, err := s.tenants.FindByID(ctx, tenantID)
	if err != nil || strings.TrimSpace(t.Name) == "" {
		return "Admin"
	}
	return t.Name + " admin"
}

// Confirm verifies a code and sets the new password.
//
// Every failure below is the same error on the wire, so a caller cannot learn
// whether the address exists, whether a code was ever issued, or whether the one
// they guessed was merely expired. The distinctions are real and belong in the
// log, not the response.
func (s *TenantPasswordResetService) Confirm(ctx context.Context, tenantID primitive.ObjectID, email, code, newPassword string) error {
	email = normalizeResetEmail(email)
	code = strings.TrimSpace(code)

	if len(newPassword) < 8 {
		// Safe to be specific about: it says nothing about the account, only
		// about what the caller typed.
		return apierr.BadRequest("new password must be at least 8 characters")
	}

	invalid := apierr.BadRequest("that code is not valid — request a new one").In(apierr.DomainAuth)

	reset, err := s.codes.FindActive(ctx, tenantID, email)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// Spend what a real check costs, so an address with no outstanding
			// code does not answer measurably faster than one that has to
			// verify a hash.
			password.DummyCompare()
			s.log.Info("tenant password reset confirm with no outstanding code", zap.String("email", email))
			return invalid
		}
		return apierr.Internal(err)
	}

	if reset.Spent(time.Now()) {
		s.log.Info("tenant password reset confirm against a spent code",
			zap.String("email", email), zap.Int("attempts", reset.Attempts))
		return invalid
	}

	// Constant-time, so a comparison that returns early cannot reveal how much
	// of the code was right.
	if subtle.ConstantTimeCompare([]byte(hashResetCode(code)), []byte(reset.CodeHash)) != 1 {
		if err := s.codes.RecordAttempt(ctx, reset.ID); err != nil {
			s.log.Error("could not record a failed reset attempt", zap.Error(err))
		}
		return invalid
	}

	// Re-read the user rather than trusting the one that existed when the code
	// was issued: suspension exists to keep someone out, and a code issued before
	// it must not get them back in.
	user, err := s.users.FindByTenantAndEmail(ctx, tenantID, email)
	if err != nil || user.Status != models.TenantUserActive || user.ID != reset.UserID {
		s.log.Warn("tenant password reset confirm for a user that is gone or not active", zap.String("email", email))
		return invalid
	}

	// Consume before writing the password, conditional on it still being
	// unused, so two confirms racing with the same code cannot both proceed.
	ok, err := s.codes.MarkUsed(ctx, reset.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	if !ok {
		return invalid
	}

	hash, err := password.Hash(newPassword)
	if err != nil {
		return apierr.Internal(err)
	}
	if err := s.users.UpdatePassword(ctx, tenantID, user.ID, hash); err != nil {
		return apierr.Internal(err)
	}

	// Any other outstanding code is now worthless: a second reset mail, possibly
	// sent by an attacker, must not still work after the owner has taken the
	// account back.
	if err := s.codes.InvalidateForUser(ctx, tenantID, user.ID); err != nil {
		s.log.Error("could not invalidate remaining reset codes", zap.Error(err))
	}

	// The owner must hear about it. In the background, like the request: the
	// caller has what they came for and should not wait on SMTP.
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		nctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.mail.Send(nctx, email, "password_changed", map[string]string{
			"app":  s.appName(nctx, tenantID),
			"name": greetingName(user),
		}); err != nil {
			s.log.Error("password changed but the notice could not be mailed", zap.Error(err))
		}
	}()

	// Worth stating plainly: a reset does NOT end sessions already signed in.
	// Tokens are stateless and last until they expire.
	s.log.Info("tenant user password reset via emailed code",
		zap.String("tenant", tenantID.Hex()), zap.String("email", email))
	return nil
}

// generateResetCode returns six digits from crypto/rand. math/rand could be
// guessed from an earlier code, which defeats the entire flow.
func generateResetCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	// Zero-padded: 000123 is a valid code, and trimming it would make a sixth of
	// all codes shorter and easier to guess.
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// hashResetCode hashes a code for storage and comparison.
//
// SHA-256 rather than bcrypt, deliberately: bcrypt's cost exists to make
// guessing a low-entropy human-chosen secret slow. This one is machine-chosen,
// single-use, ten minutes old and capped at five attempts, so the guessing
// budget is already closed. What matters is that a database leak does not hand
// over live codes, which a fast hash does just as well.
func hashResetCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func normalizeResetEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// greetingName is what the email says after "Hello". tenantcore rejects a
// template with a blank value, and a user can have no name, so an empty one
// must not reach it or the code is stored but never mailed.
func greetingName(u *models.TenantUser) string {
	if n := strings.TrimSpace(u.Name); n != "" {
		return n
	}
	return "there"
}
