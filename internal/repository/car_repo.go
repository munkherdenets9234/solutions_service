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

type CarRepo struct {
	col *mongo.Collection
}

func NewCarRepo(db *mongo.Database) *CarRepo {
	return &CarRepo{col: db.Collection("cars")}
}

func (r *CarRepo) Create(ctx context.Context, tenantID primitive.ObjectID, c *models.Car) error {
	c.ID = primitive.NewObjectID()
	c.TenantID = tenantID
	c.CreatedAt = time.Now()
	c.UpdatedAt = time.Now()
	_, err := r.col.InsertOne(ctx, c)
	return err
}

func (r *CarRepo) FindAll(ctx context.Context, tenantID primitive.ObjectID, filter bson.M, page, limit int) ([]*models.Car, int64, error) {
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

	var results []*models.Car
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *CarRepo) FindBySlug(ctx context.Context, tenantID primitive.ObjectID, slug string) (*models.Car, error) {
	var c models.Car
	err := r.col.FindOne(ctx, bson.M{"tenant_id": tenantID, "slug": slug, "is_active": true}).Decode(&c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CarRepo) FindByID(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (*models.Car, error) {
	var c models.Car
	err := r.col.FindOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}).Decode(&c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CarRepo) Update(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, update bson.M) error {
	stripProtectedFields(update)
	update["updated_at"] = time.Now()
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": update})
	return err
}

func (r *CarRepo) Delete(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": bson.M{"is_active": false, "updated_at": time.Now()}})
	return err
}
