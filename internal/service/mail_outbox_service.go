package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// mailLogStore is the slice of the outbox repository the admin mail log uses.
type mailLogStore interface {
	List(ctx context.Context, tenantID primitive.ObjectID, status models.MailStatus, page, limit int) ([]*models.MailOutbox, int64, error)
	ResetFailed(ctx context.Context, tenantID, id primitive.ObjectID) error
}

// MailLogItem is what the admin sees of one outbox row. It is a separate type
// on purpose: the row also holds the template data (links, a signed
// unsubscribe URL) and the full recipient address, and none of that may reach
// the API by a model gaining a json tag.
type MailLogItem struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	RecordID  string     `json:"record_id"`
	To        string     `json:"to"` // masked, see maskRecipient
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	LastError string     `json:"last_error"`
	CreatedAt time.Time  `json:"created_at"`
	SentAt    *time.Time `json:"sent_at"`
}

// MailOutboxService backs the tenant admin's mail log and manual retry.
type MailOutboxService struct {
	store mailLogStore
}

func NewMailOutboxService(store mailLogStore) *MailOutboxService {
	return &MailOutboxService{store: store}
}

// maskRecipient shows the first character and the domain: a***@example.com.
// An address with no @ shows nothing of itself.
func maskRecipient(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return "***"
	}
	local, domain := []rune(addr[:at]), addr[at:]
	if len(local) == 0 {
		return "***" + domain
	}
	return string(local[0]) + "***" + domain
}

func validMailStatus(s string) (models.MailStatus, bool) {
	switch st := models.MailStatus(s); st {
	case "", models.MailPending, models.MailSent, models.MailFailed:
		return st, true
	}
	return "", false
}

// List returns one page of the tenant's mail log, newest first. status is
// optional (pending, sent or failed); anything else is a 400.
func (s *MailOutboxService) List(ctx context.Context, tenantID primitive.ObjectID, status string, page, limit int) ([]*MailLogItem, int64, error) {
	st, ok := validMailStatus(status)
	if !ok {
		return nil, 0, apierr.BadRequest("invalid status")
	}
	page, limit = ClampPage(page, limit)
	rows, total, err := s.store.List(ctx, tenantID, st, page, limit)
	if err != nil {
		return nil, 0, apierr.Internal(err)
	}
	items := make([]*MailLogItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, &MailLogItem{
			ID:        r.ID.Hex(),
			Kind:      r.Kind,
			RecordID:  r.RecordID.Hex(),
			To:        maskRecipient(r.To),
			Status:    string(r.Status),
			Attempts:  r.Attempts,
			LastError: r.LastError,
			CreatedAt: r.CreatedAt,
			SentAt:    r.SentAt,
		})
	}
	return items, total, nil
}

// Retry puts a failed row back in the queue. A row that is not failed, does
// not exist or belongs to another tenant all answer the same 404.
func (s *MailOutboxService) Retry(ctx context.Context, tenantID primitive.ObjectID, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	if err := s.store.ResetFailed(ctx, tenantID, oid); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return apierr.NotFound("mail")
		}
		return apierr.Internal(err)
	}
	return nil
}
