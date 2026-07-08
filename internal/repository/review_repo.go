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

type ReviewRepo struct {
	col *mongo.Collection
}

func NewReviewRepo(db *mongo.Database) *ReviewRepo {
	return &ReviewRepo{col: db.Collection("reviews")}
}

func (r *ReviewRepo) Create(ctx context.Context, tenantID primitive.ObjectID, rev *models.Review) error {
	rev.ID = primitive.NewObjectID()
	rev.TenantID = tenantID
	rev.CreatedAt = time.Now()
	rev.UpdatedAt = time.Now()
	_, err := r.col.InsertOne(ctx, rev)
	return err
}

func (r *ReviewRepo) FindAll(ctx context.Context, tenantID primitive.ObjectID, filter bson.M, page, limit int) ([]*models.Review, int64, error) {
	filter["tenant_id"] = tenantID

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

	var results []*models.Review
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *ReviewRepo) FindByID(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (*models.Review, error) {
	var rev models.Review
	err := r.col.FindOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}).Decode(&rev)
	if err != nil {
		return nil, err
	}
	return &rev, nil
}

func (r *ReviewRepo) Update(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, update bson.M) error {
	stripProtectedFields(update)
	update["updated_at"] = time.Now()
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": update})
	return err
}

func (r *ReviewRepo) Delete(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (int64, error) {
	res, err := r.col.DeleteOne(ctx, bson.M{"_id": id, "tenant_id": tenantID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
