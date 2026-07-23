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

type NewsletterRepo struct {
	col *mongo.Collection
}

func NewNewsletterRepo(db *mongo.Database) *NewsletterRepo {
	return &NewsletterRepo{col: db.Collection("newsletter_subscribers")}
}

// Create upserts on (tenant_id, email) so a repeat signup from the same
// visitor is a no-op instead of a duplicate-key error off the unique index.
func (r *NewsletterRepo) Create(ctx context.Context, tenantID primitive.ObjectID, m *models.NewsletterSubscriber) error {
	m.TenantID = tenantID
	filter := bson.M{"tenant_id": tenantID, "email": m.Email}
	update := bson.M{"$setOnInsert": bson.M{
		"_id":        primitive.NewObjectID(),
		"tenant_id":  tenantID,
		"email":      m.Email,
		"created_at": time.Now(),
	}}
	opts := options.Update().SetUpsert(true)
	if _, err := r.col.UpdateOne(ctx, filter, update, opts); err != nil {
		return err
	}
	return r.col.FindOne(ctx, filter).Decode(m)
}

func (r *NewsletterRepo) FindAll(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.NewsletterSubscriber, int64, error) {
	filter := bson.M{"tenant_id": tenantID}
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

	var results []*models.NewsletterSubscriber
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *NewsletterRepo) Delete(ctx context.Context, tenantID, id primitive.ObjectID) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"_id": id, "tenant_id": tenantID})
	return err
}
