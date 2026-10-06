package service

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/notify"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

const (
	workerPollInterval = 15 * time.Second
	workerSendTimeout  = 30 * time.Second
	// workerLease must exceed workerSendTimeout, or a row becomes due again
	// while its first send is still running.
	workerLease      = 5 * time.Minute
	workerMaxPerTick = 50
	maxSendAttempts  = 5
)

// Short codes stored in mail_outbox.last_error. Never provider text.
const (
	errCodeNotConfigured = "mail_not_configured"
	errCodeRateLimited   = "rate_limited"
	errCodeUpstream      = "upstream"
	errCodeUnreachable   = "unreachable"
	errCodeOptedOut      = "recipient_opted_out"
)

type (
	mailSender interface {
		Send(ctx context.Context, to, template string, data map[string]string) error
	}
	outboxWorkStore interface {
		ClaimDue(ctx context.Context, now time.Time, lease time.Duration) (*models.MailOutbox, error)
		MarkSent(ctx context.Context, id primitive.ObjectID, now time.Time) error
		MarkAttemptFailed(ctx context.Context, id primitive.ObjectID, errCode string, attempts int, nextAt time.Time, final bool) error
	}
	userChecker interface {
		FindByID(ctx context.Context, tenantID, id primitive.ObjectID) (*models.TenantUser, error)
	}
)

// MailWorker sends due outbox rows through tenantcore and retries failures.
type MailWorker struct {
	store outboxWorkStore
	mail  mailSender
	users userChecker
	now   func() time.Time
	log   *zap.Logger
}

func NewMailWorker(store outboxWorkStore, mail mailSender, users userChecker, now func() time.Time, log *zap.Logger) *MailWorker {
	if now == nil {
		now = time.Now
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &MailWorker{store: store, mail: mail, users: users, now: now, log: log}
}

// nextAttemptDelay is the wait after the given number of failures (1..4).
func nextAttemptDelay(failures int) time.Duration {
	switch failures {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	case 3:
		return 30 * time.Minute
	default:
		return 2 * time.Hour
	}
}

// errorCode maps a send error to a stored short code. It never returns err.Error().
func errorCode(err error) string {
	var netErr net.Error
	switch {
	case errors.Is(err, notify.ErrMailNotConfigured):
		return errCodeNotConfigured
	case errors.Is(err, notify.ErrRateLimited):
		return errCodeRateLimited
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr):
		return errCodeUnreachable
	default:
		return errCodeUpstream
	}
}

// mailReady is false for a nil sender, including an unconfigured *notify.Client
// held in the interface, so rows stay pending instead of burning attempts.
func (w *MailWorker) mailReady() bool {
	if w.mail == nil {
		return false
	}
	if a, ok := w.mail.(interface{ Available() bool }); ok && !a.Available() {
		return false
	}
	return true
}

// Run polls until ctx is done. The first Tick runs immediately.
func (w *MailWorker) Run(ctx context.Context) {
	if w == nil {
		return
	}
	t := time.NewTicker(workerPollInterval)
	defer t.Stop()
	for {
		w.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick processes due rows, at most workerMaxPerTick.
func (w *MailWorker) Tick(ctx context.Context) {
	if w == nil || w.store == nil || w.users == nil || !w.mailReady() {
		return
	}
	for i := 0; i < workerMaxPerTick; i++ {
		if ctx.Err() != nil {
			return
		}
		row, err := w.store.ClaimDue(ctx, w.now(), workerLease)
		if err != nil {
			w.log.Error("mail claim failed", zap.String("outcome", "claim_failed"))
			return
		}
		if row == nil {
			return
		}
		w.process(ctx, row)
	}
}

func (w *MailWorker) process(ctx context.Context, row *models.MailOutbox) {
	rowLog := w.log.With(zap.String("outbox_id", row.ID.Hex()), zap.String("tenant_id", row.TenantID.Hex()))
	attempts := row.Attempts + 1

	// An unsubscribe or suspension wins over a queued row.
	user, err := w.users.FindByID(ctx, row.TenantID, row.UserID)
	missing := user == nil && (err == nil || errors.Is(err, mongo.ErrNoDocuments))
	if err != nil && !missing {
		// Transient: leave the row; its lease expires and it comes back, with no attempt burned.
		rowLog.Error("mail recipient check failed", zap.String("outcome", "recipient_check_failed"))
		return
	}
	if missing || user.Status != models.TenantUserActive || !user.ReceiveEmails {
		w.recordFailure(ctx, rowLog, row.ID, errCodeOptedOut, attempts, w.now(), true)
		return
	}

	sendCtx, cancel := context.WithTimeout(ctx, workerSendTimeout)
	err = w.mail.Send(sendCtx, row.To, notifyTemplate, row.Data)
	cancel()
	if err == nil {
		if mErr := w.store.MarkSent(ctx, row.ID, w.now()); mErr != nil {
			rowLog.Error("mail mark sent failed", zap.String("outcome", "mark_sent_failed"))
		}
		return
	}
	code := errorCode(err)
	final := attempts >= maxSendAttempts
	next := w.now()
	if !final {
		next = next.Add(nextAttemptDelay(attempts))
	}
	rowLog.Warn("mail send failed", zap.String("outcome", code), zap.Int("attempts", attempts), zap.Bool("final", final))
	w.recordFailure(ctx, rowLog, row.ID, code, attempts, next, final)
}

func (w *MailWorker) recordFailure(ctx context.Context, log *zap.Logger, id primitive.ObjectID, code string, attempts int, next time.Time, final bool) {
	if err := w.store.MarkAttemptFailed(ctx, id, code, attempts, next, final); err != nil {
		log.Error("mail mark failed failed", zap.String("outcome", "mark_failed_error"))
	}
}
