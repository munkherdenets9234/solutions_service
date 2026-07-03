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

type AirportTransferRepo struct {
	col *mongo.Collection
}

func NewAirportTransferRepo(db *mongo.Database) *AirportTransferRepo {
	return &AirportTransferRepo{col: db.Collection("airport_transfers")}
}

func (r *AirportTransferRepo) Create(ctx context.Context, tenantID primitive.ObjectID, t *models.AirportTransfer) error {
	t.ID = primitive.NewObjectID()
	t.TenantID = tenantID
	t.Status = models.TransferPending
	t.CreatedAt = time.Now()
	t.UpdatedAt = time.Now()
	_, err := r.col.InsertOne(ctx, t)
	return err
}

func (r *AirportTransferRepo) FindAll(ctx context.Context, tenantID primitive.ObjectID, filter bson.M, page, limit int) ([]*models.AirportTransfer, int64, error) {
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

	var results []*models.AirportTransfer
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *AirportTransferRepo) FindByID(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (*models.AirportTransfer, error) {
	var t models.AirportTransfer
	err := r.col.FindOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}).Decode(&t)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *AirportTransferRepo) UpdateStatus(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, status models.TransferStatus) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": bson.M{
		"status":     status,
		"updated_at": time.Now(),
	}})
	return err
}
