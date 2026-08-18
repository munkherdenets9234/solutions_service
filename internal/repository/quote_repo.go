package repository

import (
	"context"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type QuoteRepo struct {
	col *mongo.Collection
}

func NewQuoteRepo(db *mongo.Database) *QuoteRepo {
	return &QuoteRepo{col: db.Collection("quotes")}
}

// Create inserts q as-is — q.TenantID may be nil (a lead with no existing
// tenant relationship) or set (the caller already resolved it, e.g. from a
// tenant's X-API-Key).
func (r *QuoteRepo) Create(ctx context.Context, q *models.Quote) error {
	q.ID = primitive.NewObjectID()
	q.Status = models.QuoteNew
	q.CreatedAt = time.Now()
	_, err := r.col.InsertOne(ctx, q)
	return err
}

// FindAll lists quotes matching filter — pass {"tenant_id": id} for a single
// tenant's own leads, or {} for every quote across the platform (including
// tenant-less ones).
func (r *QuoteRepo) FindAll(ctx context.Context, filter bson.M, page, limit int) ([]*models.Quote, int64, error) {
	total, err := r.col.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	skip := int64((page - 1) * limit)
	opts := options.Find().SetSkip(skip).SetLimit(int64(limit)).SetSort(bson.D{{Key: "created_at", Value: -1}})

	cur, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)

	var results []*models.Quote
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *QuoteRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*models.Quote, error) {
	var q models.Quote
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&q)
	if err != nil {
		return nil, err
	}
	return &q, nil
}

// UpdateStatus updates any quote regardless of tenant — used by the
// platform-wide status route, which must also be able to act on
// tenant-less leads.
func (r *QuoteRepo) UpdateStatus(ctx context.Context, id primitive.ObjectID, status models.QuoteStatus, userID *primitive.ObjectID) error {
	set := bson.M{
		"status":     status,
		"updated_at": time.Now(),
	}
	if userID != nil {
		set["user_id"] = *userID
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	return err
}

// UpdateStatusForTenant is UpdateStatus scoped to a single tenant's own
// leads — used by the tenant admin panel, so a tenant can only ever update
// quotes that actually belong to it.
func (r *QuoteRepo) UpdateStatusForTenant(ctx context.Context, tenantID, id primitive.ObjectID, status models.QuoteStatus, userID *primitive.ObjectID) error {
	set := bson.M{
		"status":     status,
		"updated_at": time.Now(),
	}
	if userID != nil {
		set["user_id"] = *userID
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": set})
	return err
}
