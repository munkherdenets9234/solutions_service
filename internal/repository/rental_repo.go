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

type RentalRepo struct {
	col *mongo.Collection
}

func NewRentalRepo(db *mongo.Database) *RentalRepo {
	return &RentalRepo{col: db.Collection("rentals")}
}

func (r *RentalRepo) Create(ctx context.Context, tenantID primitive.ObjectID, rt *models.Rental) error {
	rt.ID = primitive.NewObjectID()
	rt.TenantID = tenantID
	rt.Status = models.RentalPending
	rt.CreatedAt = time.Now()
	rt.UpdatedAt = time.Now()
	_, err := r.col.InsertOne(ctx, rt)
	return err
}

func (r *RentalRepo) FindAll(ctx context.Context, tenantID primitive.ObjectID, filter bson.M, page, limit int) ([]*models.Rental, int64, error) {
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

	var results []*models.Rental
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *RentalRepo) FindByID(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (*models.Rental, error) {
	var rt models.Rental
	err := r.col.FindOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}).Decode(&rt)
	if err != nil {
		return nil, err
	}
	return &rt, nil
}

func (r *RentalRepo) UpdateStatus(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, status models.RentalStatus) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": bson.M{
		"status":     status,
		"updated_at": time.Now(),
	}})
	return err
}
