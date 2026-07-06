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

type TenantRepo struct {
	col *mongo.Collection
}

func NewTenantRepo(db *mongo.Database) *TenantRepo {
	return &TenantRepo{col: db.Collection("tenants")}
}

func (r *TenantRepo) Create(ctx context.Context, t *models.Tenant) error {
	t.ID = primitive.NewObjectID()
	t.Status = models.TenantActive
	t.CreatedAt = time.Now()
	t.UpdatedAt = time.Now()
	_, err := r.col.InsertOne(ctx, t)
	return err
}

func (r *TenantRepo) FindAll(ctx context.Context, page, limit int) ([]*models.Tenant, int64, error) {
	total, err := r.col.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, err
	}

	skip := int64((page - 1) * limit)
	opts := options.Find().SetSkip(skip).SetLimit(int64(limit)).SetSort(bson.D{{Key: "created_at", Value: -1}})

	cur, err := r.col.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)

	var results []*models.Tenant
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *TenantRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*models.Tenant, error) {
	var t models.Tenant
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&t)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TenantRepo) FindByAPIKeyHash(ctx context.Context, hash string) (*models.Tenant, error) {
	var t models.Tenant
	err := r.col.FindOne(ctx, bson.M{"api_key_hash": hash}).Decode(&t)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TenantRepo) UpdateStatus(ctx context.Context, id primitive.ObjectID, status models.TenantStatus) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
		"status":     status,
		"updated_at": time.Now(),
	}})
	return err
}

func (r *TenantRepo) RotateAPIKey(ctx context.Context, id primitive.ObjectID, hash, last4 string) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
		"api_key_hash":  hash,
		"api_key_last4": last4,
		"updated_at":    time.Now(),
	}})
	return err
}

func (r *TenantRepo) UpdateDomain(ctx context.Context, id primitive.ObjectID, domain string) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
		"domain":     domain,
		"updated_at": time.Now(),
	}})
	return err
}
