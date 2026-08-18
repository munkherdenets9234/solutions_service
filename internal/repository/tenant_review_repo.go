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

type TenantReviewRepo struct {
	col *mongo.Collection
}

func NewTenantReviewRepo(db *mongo.Database) *TenantReviewRepo {
	return &TenantReviewRepo{col: db.Collection("tenant_reviews")}
}

func (r *TenantReviewRepo) Create(ctx context.Context, rev *models.TenantReview, userID *primitive.ObjectID) error {
	rev.ID = primitive.NewObjectID()
	rev.IsActive = true
	rev.CreatedAt = time.Now()
	rev.UpdatedAt = time.Now()
	rev.UserID = userID
	_, err := r.col.InsertOne(ctx, rev)
	return err
}

func (r *TenantReviewRepo) FindAll(ctx context.Context, filter bson.M, page, limit int) ([]*models.TenantReview, int64, error) {
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

	var results []*models.TenantReview
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *TenantReviewRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*models.TenantReview, error) {
	var rev models.TenantReview
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&rev)
	if err != nil {
		return nil, err
	}
	return &rev, nil
}

func (r *TenantReviewRepo) Update(ctx context.Context, id primitive.ObjectID, update bson.M, userID *primitive.ObjectID) error {
	stripProtectedFields(update)
	update["updated_at"] = time.Now()
	if userID != nil {
		update["user_id"] = *userID
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": update})
	return err
}

func (r *TenantReviewRepo) Delete(ctx context.Context, id primitive.ObjectID, userID *primitive.ObjectID) error {
	set := bson.M{"is_active": false, "updated_at": time.Now()}
	if userID != nil {
		set["user_id"] = *userID
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	return err
}
