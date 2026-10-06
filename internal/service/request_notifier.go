package service

import (
	"context"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/unsubscribe"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

// NotifyKind names the storefront request that triggered a notification. The
// value is also what the template shows as request_type.
type NotifyKind string

const (
	NotifyBooking  NotifyKind = "booking"
	NotifyRental   NotifyKind = "car rental"
	NotifyTransfer NotifyKind = "airport transfer"
	NotifyGuide    NotifyKind = "guide application"
)

const (
	// notifyTemplate is the tenantcore template. It takes exactly the six keys below.
	notifyTemplate = "request_notification"
	// maxValueRunes caps every template value; maxUnsubscribeRunes caps unsubscribe_url.
	// tenantcore expects the caller to enforce both.
	maxValueRunes       = 256
	maxUnsubscribeRunes = 512
	unsubscribeTTL      = 90 * 24 * time.Hour
	unsubscribePath     = "/api/v1/public/unsubscribe"
	enqueueTimeout      = 10 * time.Second
)

type (
	recipientLister interface {
		FindEmailRecipients(ctx context.Context, tenantID primitive.ObjectID) ([]*models.TenantUser, error)
	}
	outboxEnqueuer interface {
		Enqueue(ctx context.Context, rows []*models.MailOutbox) error
	}
	// linkBuilder resolves the tenant's admin link for a record, its public site
	// base (https, no trailing slash) and display name.
	linkBuilder interface {
		Links(ctx context.Context, tenantID primitive.ObjectID, kind NotifyKind, recordID string) (adminURL, siteBase, tenantName string, err error)
	}
)

// RequestNotifier queues one email per opted-in tenant user when a storefront
// request is created. It makes no network call: it only inserts outbox rows,
// which MailWorker sends. It never returns an error, because a mail problem
// must not touch the visitor's request.
type RequestNotifier struct {
	users   recipientLister
	store   outboxEnqueuer
	links   linkBuilder
	key     []byte
	appName string
	now     func() time.Time
	log     *zap.Logger
}

// NewRequestNotifier builds a notifier. key must come from a validated
// MAIL_UNSUBSCRIBE_KEY. A nil now or log gets a safe default.
func NewRequestNotifier(users recipientLister, store outboxEnqueuer, links linkBuilder, key []byte, appName string, now func() time.Time, log *zap.Logger) *RequestNotifier {
	if now == nil {
		now = time.Now
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &RequestNotifier{users: users, store: store, links: links, key: key, appName: appName, now: now, log: log}
}

// oneLine collapses all whitespace runs (including newlines) to single spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// capRunes truncates to max runes without cutting inside a rune.
func capRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// Notify queues a notification for every opted-in recipient of the tenant.
// Nil-safe, and every failure is logged by id and outcome code only.
func (n *RequestNotifier) Notify(ctx context.Context, tenantID primitive.ObjectID, kind NotifyKind, recordID, summary string) {
	if n == nil || n.users == nil || n.store == nil || n.links == nil {
		return
	}
	logf := func(msg string, fields ...zap.Field) {
		n.log.Error(msg, append([]zap.Field{
			zap.String("tenant_id", tenantID.Hex()), zap.String("kind", string(kind)), zap.String("record_id", recordID),
		}, fields...)...)
	}
	recID, err := primitive.ObjectIDFromHex(recordID)
	if err != nil {
		logf("request notify skipped", zap.String("outcome", "bad_record_id"))
		return
	}
	// The request has already succeeded. Do not let its context ending cancel the enqueue.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), enqueueTimeout)
	defer cancel()

	recipients, err := n.users.FindEmailRecipients(ctx, tenantID)
	if err != nil {
		logf("request notify skipped", zap.String("outcome", "recipient_lookup_failed"))
		return
	}
	if len(recipients) == 0 {
		return
	}
	adminURL, siteBase, tenantName, err := n.links.Links(ctx, tenantID, kind, recordID)
	if err != nil {
		logf("request notify skipped", zap.String("outcome", "links_failed"))
		return
	}
	// A URL cannot be truncated into a working link: skip rather than send a broken one.
	adminURL = strings.TrimSpace(adminURL)
	siteBase = strings.TrimRight(strings.TrimSpace(siteBase), "/")
	if !strings.HasPrefix(adminURL, "https://") || !strings.HasPrefix(siteBase, "https://") ||
		utf8.RuneCountInString(adminURL) > maxValueRunes {
		logf("request notify skipped", zap.String("outcome", "bad_admin_or_site_url"))
		return
	}

	now := n.now()
	expires := now.Add(unsubscribeTTL)
	base := map[string]string{
		"app":          capRunes(oneLine(n.appName), maxValueRunes),
		"tenant":       capRunes(oneLine(tenantName), maxValueRunes),
		"request_type": capRunes(oneLine(string(kind)), maxValueRunes),
		"summary":      capRunes(oneLine(summary), maxValueRunes),
		"admin_url":    adminURL,
	}
	rows := make([]*models.MailOutbox, 0, len(recipients))
	for _, u := range recipients {
		unsubURL := siteBase + unsubscribePath + "?token=" + url.QueryEscape(unsubscribe.Sign(n.key, tenantID, u.ID, expires))
		if utf8.RuneCountInString(unsubURL) > maxUnsubscribeRunes {
			logf("request notify recipient skipped", zap.String("user_id", u.ID.Hex()), zap.String("outcome", "unsubscribe_url_too_long"))
			continue
		}
		data := make(map[string]string, len(base)+1)
		for k, v := range base {
			data[k] = v
		}
		data["unsubscribe_url"] = unsubURL
		rows = append(rows, &models.MailOutbox{
			TenantID: tenantID, UserID: u.ID, To: u.Email, Kind: string(kind), RecordID: recID, Data: data,
		})
	}
	if len(rows) == 0 {
		return
	}
	if err := n.store.Enqueue(ctx, rows); err != nil {
		logf("request notify enqueue failed", zap.String("outcome", "enqueue_failed"))
	}
}
