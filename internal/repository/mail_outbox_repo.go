package repository

import (
	"context"
	"errors"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// maxErrCodeLen bounds last_error. It holds a short code, never provider text.
const maxErrCodeLen = 64

// mailOutboxTTLSeconds deletes rows 30 days after creation.
const mailOutboxTTLSeconds = 30 * 24 * 60 * 60

type MailOutboxRepo struct {
	col *mongo.Collection
}

func NewMailOutboxRepo(db *mongo.Database) *MailOutboxRepo {
	return &MailOutboxRepo{col: db.Collection("mail_outbox")}
}

// claimFilter finds rows a worker may take: pending and due. Deliberately not
// tenant-scoped, the worker serves every tenant.
func claimFilter(now time.Time) bson.M {
	return bson.M{
		"status":          models.MailPending,
		"next_attempt_at": bson.M{"$lte": now},
	}
}

// claimUpdate is the lease: the row stops being due until the lease passes, so
// a second worker skips it and a crashed worker's row comes back by itself.
func claimUpdate(now time.Time, lease time.Duration) bson.M {
	return bson.M{"$set": bson.M{
		"next_attempt_at": now.Add(lease),
		"updated_at":      now,
	}}
}

func claimSort() bson.D {
	return bson.D{{Key: "next_attempt_at", Value: 1}}
}

// markFilter matches a row only while it is still pending, so a worker that
// overran its lease cannot overwrite a row another worker already finished
// (a late failure flipping a sent row back to pending would send it twice).
func markFilter(id primitive.ObjectID) bson.M {
	return bson.M{"_id": id, "status": models.MailPending}
}

func sentUpdate(now time.Time) bson.M {
	return bson.M{"$set": bson.M{
		"status":     models.MailSent,
		"sent_at":    now,
		"last_error": "",
		"updated_at": now,
	}}
}

// truncateErrCode bounds a stored error code to maxErrCodeLen runes.
func truncateErrCode(code string) string {
	r := []rune(code)
	if len(r) > maxErrCodeLen {
		return string(r[:maxErrCodeLen])
	}
	return code
}

// attemptFailedUpdate records a failed send: back to pending for a retry, or
// failed for good when final.
func attemptFailedUpdate(now time.Time, errCode string, attempts int, nextAt time.Time, final bool) bson.M {
	status := models.MailPending
	if final {
		status = models.MailFailed
	}
	return bson.M{"$set": bson.M{
		"status":          status,
		"attempts":        attempts,
		"next_attempt_at": nextAt,
		"last_error":      truncateErrCode(errCode),
		"updated_at":      now,
	}}
}

func resetFailedFilter(tenantID, id primitive.ObjectID) bson.M {
	return bson.M{"_id": id, "tenant_id": tenantID, "status": models.MailFailed}
}

func resetFailedUpdate(now time.Time) bson.M {
	return bson.M{"$set": bson.M{
		"status":          models.MailPending,
		"attempts":        0,
		"next_attempt_at": now,
		"updated_at":      now,
	}}
}

func cancelPendingFilter(tenantID, userID primitive.ObjectID) bson.M {
	return bson.M{"tenant_id": tenantID, "user_id": userID, "status": models.MailPending}
}

func listFilter(tenantID primitive.ObjectID, status models.MailStatus) bson.M {
	f := bson.M{"tenant_id": tenantID}
	if status != "" {
		f["status"] = status
	}
	return f
}

// mailOutboxIndexes: the claim scan, the admin list, and the 30 day TTL.
func mailOutboxIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "next_attempt_at", Value: 1}}},
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "status", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "created_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(mailOutboxTTLSeconds)},
	}
}

// Enqueue inserts rows as pending and due now. An empty slice is not an error.
func (r *MailOutboxRepo) Enqueue(ctx context.Context, rows []*models.MailOutbox) error {
	if len(rows) == 0 {
		return nil
	}
	now := time.Now()
	docs := make([]interface{}, len(rows))
	for i, m := range rows {
		m.ID = primitive.NewObjectID()
		m.Status = models.MailPending
		m.Attempts = 0
		m.NextAttemptAt = now
		m.LastError = ""
		m.CreatedAt = now
		m.UpdatedAt = now
		m.SentAt = nil
		docs[i] = m
	}
	_, err := r.col.InsertMany(ctx, docs)
	return err
}

// ClaimDue atomically takes the oldest due pending row and leases it, or
// returns (nil, nil) when there is none. One FindOneAndUpdate, so two workers
// cannot both get the same row. The lease must exceed the worker's send
// timeout, otherwise the row becomes due again while the first send is still
// running and is claimed twice.
func (r *MailOutboxRepo) ClaimDue(ctx context.Context, now time.Time, lease time.Duration) (*models.MailOutbox, error) {
	var m models.MailOutbox
	err := r.col.FindOneAndUpdate(ctx, claimFilter(now), claimUpdate(now, lease),
		options.FindOneAndUpdate().SetSort(claimSort()).SetReturnDocument(options.After),
	).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// MarkSent records a delivered row. Only a still-pending row is updated, so the
// lease passed to ClaimDue must exceed the worker's send timeout.
func (r *MailOutboxRepo) MarkSent(ctx context.Context, id primitive.ObjectID, now time.Time) error {
	// Zero matches is fine: the row was deleted by unsubscribe or finished by another worker.
	_, err := r.col.UpdateOne(ctx, markFilter(id), sentUpdate(now))
	return err
}

// MarkAttemptFailed stores the outcome of a failed send. errCode is a short
// code and is truncated to 64 characters.
func (r *MailOutboxRepo) MarkAttemptFailed(ctx context.Context, id primitive.ObjectID, errCode string, attempts int, nextAt time.Time, final bool) error {
	// Zero matches is fine: the row was deleted by unsubscribe or finished by another worker.
	_, err := r.col.UpdateOne(ctx, markFilter(id), attemptFailedUpdate(time.Now(), errCode, attempts, nextAt, final))
	return err
}

// CancelPendingForUser deletes a user's queued mail, e.g. after unsubscribe.
// Sent and failed rows are kept as history.
func (r *MailOutboxRepo) CancelPendingForUser(ctx context.Context, tenantID, userID primitive.ObjectID) (int64, error) {
	res, err := r.col.DeleteMany(ctx, cancelPendingFilter(tenantID, userID))
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// List pages a tenant's rows, newest first; an empty status means all.
func (r *MailOutboxRepo) List(ctx context.Context, tenantID primitive.ObjectID, status models.MailStatus, page, limit int) ([]*models.MailOutbox, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	filter := listFilter(tenantID, status)
	total, err := r.col.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	opts := options.Find().SetSkip(int64((page - 1) * limit)).SetLimit(int64(limit)).
		SetSort(bson.D{{Key: "created_at", Value: -1}})
	cur, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)
	var rows []*models.MailOutbox
	if err := cur.All(ctx, &rows); err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ResetFailed puts a failed row back in the queue. mongo.ErrNoDocuments when
// the row is not failed or belongs to another tenant.
func (r *MailOutboxRepo) ResetFailed(ctx context.Context, tenantID, id primitive.ObjectID) error {
	res, err := r.col.UpdateOne(ctx, resetFailedFilter(tenantID, id), resetFailedUpdate(time.Now()))
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}
