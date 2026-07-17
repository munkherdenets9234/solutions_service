package repository

import (
	"context"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type SubscriptionRepo struct {
	col *mongo.Collection
}

func NewSubscriptionRepo(db *mongo.Database) *SubscriptionRepo {
	return &SubscriptionRepo{col: db.Collection("subscriptions")}
}

func (r *SubscriptionRepo) Create(ctx context.Context, s *models.Subscription, userID *primitive.ObjectID) error {
	s.ID = primitive.NewObjectID()
	s.CreatedAt = time.Now()
	s.UpdatedAt = time.Now()
	s.UserID = userID
	_, err := r.col.InsertOne(ctx, s)
	return err
}

func (r *SubscriptionRepo) FindByTenantID(ctx context.Context, tenantID primitive.ObjectID) (*models.Subscription, error) {
	var s models.Subscription
	err := r.col.FindOne(ctx, bson.M{"tenant_id": tenantID}).Decode(&s)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SubscriptionRepo) UpdatePlan(ctx context.Context, tenantID primitive.ObjectID, plan models.SubscriptionPlan, periodStart, periodEnd time.Time, userID *primitive.ObjectID) error {
	set := bson.M{
		"plan":                 plan,
		"current_period_start": periodStart,
		"current_period_end":   periodEnd,
		"updated_at":           time.Now(),
	}
	if userID != nil {
		set["user_id"] = *userID
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"tenant_id": tenantID}, bson.M{"$set": set})
	return err
}

func (r *SubscriptionRepo) UpdateStatus(ctx context.Context, tenantID primitive.ObjectID, status models.SubscriptionStatus, userID *primitive.ObjectID) error {
	set := bson.M{
		"status":     status,
		"updated_at": time.Now(),
	}
	if status == models.SubscriptionCanceled {
		set["canceled_at"] = time.Now()
	}
	if userID != nil {
		set["user_id"] = *userID
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"tenant_id": tenantID}, bson.M{"$set": set})
	return err
}
