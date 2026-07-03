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

type CustomerRepo struct {
	col *mongo.Collection
}

func NewCustomerRepo(db *mongo.Database) *CustomerRepo {
	return &CustomerRepo{col: db.Collection("customers")}
}

func (r *CustomerRepo) Upsert(ctx context.Context, tenantID primitive.ObjectID, c *models.Customer) (*models.Customer, error) {
	now := time.Now()
	filter := bson.M{"tenant_id": tenantID, "email": c.Email}
	update := bson.M{
		"$setOnInsert": bson.M{"_id": primitive.NewObjectID(), "tenant_id": tenantID, "created_at": now},
		"$set": bson.M{
			"name":        c.Name,
			"phone":       c.Phone,
			"nationality": c.Nationality,
			"updated_at":  now,
		},
	}

	upsert := true
	res, err := r.col.UpdateOne(ctx, filter, update, &options.UpdateOptions{Upsert: &upsert})
	if err != nil {
		return nil, err
	}

	if res.UpsertedID != nil {
		c.ID = res.UpsertedID.(primitive.ObjectID)
		c.TenantID = tenantID
		c.CreatedAt = now
		c.UpdatedAt = now
		return c, nil
	}

	var existing models.Customer
	if err := r.col.FindOne(ctx, filter).Decode(&existing); err != nil {
		return nil, err
	}
	return &existing, nil
}

func (r *CustomerRepo) FindByID(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (*models.Customer, error) {
	var c models.Customer
	err := r.col.FindOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}).Decode(&c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CustomerRepo) FindAll(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.Customer, int64, error) {
	filter := bson.M{"tenant_id": tenantID}
	total, _ := r.col.CountDocuments(ctx, filter)
	skip := int64((page - 1) * limit)
	opts := options.Find().SetSkip(skip).SetLimit(int64(limit)).SetSort(bson.D{{Key: "created_at", Value: -1}})

	cur, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)

	var results []*models.Customer
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}
